package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"strings"
)

func decodeCustomArtwork(raw []byte) (image.Image, error) {
	if len(raw) > 4<<20 {
		return nil, errors.New("单张卡面须小于4MB")
	}
	c, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || format != "png" && format != "jpeg" || c.Width < 64 || c.Height < 64 || c.Width > 4096 || c.Height > 4096 || c.Width*c.Height > 8*1024*1024 {
		return nil, errors.New("卡面须为PNG/JPEG，边长64–4096且总像素不超过800万")
	}
	im, _, e := image.Decode(bytes.NewReader(raw))
	return im, e
}

// Fit without stretching; transparent margins preserve the template's canvas.
func fitCustomArtwork(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	w2, h2 := w, h
	if w*b.Dy() > h*b.Dx() {
		w2 = h * b.Dx() / b.Dy()
	} else {
		h2 = w * b.Dy() / b.Dx()
	}
	w2 = max(w2, 1)
	h2 = max(h2, 1)
	x0, y0 := (w-w2)/2, (h-h2)/2
	for y := 0; y < h2; y++ {
		for x := 0; x < w2; x++ {
			p := color.NRGBAModel.Convert(src.At(b.Min.X+x*b.Dx()/w2, b.Min.Y+y*b.Dy()/h2)).(color.NRGBA)
			dst.SetNRGBA(x0+x, y0+y, p)
		}
	}
	return dst
}

// CN 5.3 Texture2D: aligned name, five ints, two bools, image/dimension,
// GLTextureSettings, lightmap/color space, inline byte array, StreamingInfo.
// Other layouts fail before a ZIP is returned. RGBA32 needs no GPU codec.
func rewriteCustomTexture(raw []byte, newName string, art image.Image) ([]byte, error) {
	c := resourceCursor{b: raw, order: binary.LittleEndian}
	n := int(c.u32())
	c.take(n)
	c.align(4)
	head := c.p
	w, h := int(c.u32()), int(c.u32())
	c.u32()
	c.u32()
	c.u32()
	c.take(2)
	c.align(4)
	if c.u32() != 1 || c.u32() != 2 || w < 1 || h < 1 || w > 2048 || h > 2048 {
		return nil, errors.New("卡面模板Texture2D布局不支持")
	}
	c.take(24)
	dataPos := c.p
	length := int(c.u32())
	c.take(length)
	c.align(4)
	if c.u32() != 0 || c.u32() != 0 {
		return nil, errors.New("卡面模板使用外置纹理，暂不支持")
	}
	pathLen := int(c.u32())
	if pathLen != 0 {
		return nil, errors.New("卡面模板流路径不为空")
	}
	c.align(4)
	if c.p != len(raw) {
		return nil, errors.New("卡面模板纹理尾部不支持")
	}
	pixels := fitCustomArtwork(art, w, h)
	rgba := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		copy(rgba[y*w*4:(y+1)*w*4], pixels.Pix[(h-1-y)*pixels.Stride:(h-y)*pixels.Stride])
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(newName)))
	out.WriteString(newName)
	for out.Len()%4 != 0 {
		out.WriteByte(0)
	}
	fields := bytes.Clone(raw[head:dataPos])
	binary.LittleEndian.PutUint32(fields[8:], uint32(len(rgba)))
	binary.LittleEndian.PutUint32(fields[12:], 4)
	binary.LittleEndian.PutUint32(fields[16:], 1)
	out.Write(fields)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(rgba)))
	out.Write(rgba)
	out.Write(make([]byte, 12))
	return out.Bytes(), nil
}

// Card image paths are bucketed by the first two and next three digits of
// PictID. Renaming the texture alone leaves the client looking in another
// directory, even though the separate enlarged PNG can still be displayed.
func customArtworkResourcePath(value string, oldPict, newPict int) string {
	oldID, newID := fmt.Sprintf("%08d", oldPict), fmt.Sprintf("%08d", newPict)
	parts := strings.Split(value, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == oldID[:2] && parts[i+1] == oldID[2:5] {
			parts[i], parts[i+1] = newID[:2], newID[2:5]
		}
	}
	return strings.ReplaceAll(strings.Join(parts, "/"), oldID, newID)
}

func rewriteCustomArtworkContainer(object []byte, paths map[string]string, oldPict, newPict int) ([]byte, error) {
	for oldPath, newPath := range paths {
		// These fixed-width ID/bucket changes preserve serialized string lengths.
		if len(oldPath) != len(newPath) {
			return nil, errors.New("卡面资源路径长度不一致")
		}
		object = bytes.ReplaceAll(object, []byte(oldPath), []byte(newPath))
	}
	oldID, newID := fmt.Sprintf("%08d", oldPict), fmt.Sprintf("%08d", newPict)
	return bytes.ReplaceAll(object, []byte(oldID), []byte(newID)), nil
}

func buildCustomArtworkBundle(raw []byte, scrambled bool, oldPict, newPict int, art image.Image, paths map[string]string) (result []byte, cab string, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("卡面资源格式不支持：%v", v)
		}
	}()
	if scrambled {
		raw = bytes.Clone(raw)
		key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
		for i := range raw {
			raw[i] -= key[i%8]
		}
	}
	b, e := readResourceBundle(raw)
	if e != nil {
		return nil, "", e
	}
	if !strings.HasPrefix(b.engine, "5.3.") || len(b.nodes) != 1 {
		return nil, "", errors.New("卡面打包目前支持CN Unity 5.3单文件资源")
	}
	oldID, newID := fmt.Sprintf("%08d", oldPict), fmt.Sprintf("%08d", newPict)
	oldCab := b.nodes[0].name
	if len(oldCab) != 36 || !strings.HasPrefix(strings.ToLower(oldCab), "cab-") {
		return nil, "", errors.New("卡面CAB名称布局不支持")
	}
	found := 0
	asset, e := rewriteResourceObjects(b.nodes[0].data, func(class int, object []byte) ([]byte, error) {
		if class == 28 {
			c := resourceCursor{b: object, order: binary.LittleEndian}
			n := int(c.u32())
			name := string(c.take(n))
			if !strings.HasSuffix(name, "_"+oldID) {
				return object, nil
			}
			found++
			return rewriteCustomTexture(object, strings.TrimSuffix(name, oldID)+newID, art)
		}
		if class == 142 {
			return rewriteCustomArtworkContainer(object, paths, oldPict, newPict)
		}
		return object, nil
	})
	if e != nil {
		return nil, "", e
	}
	if found == 0 {
		return nil, "", errors.New("卡面模板未找到对应Texture2D")
	}
	// Include the new pixels so a subsequent artwork edit gets a distinct CAB.
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("%d/%s", newPict, oldCab)), asset...))
	cab = fmt.Sprintf("cab-%x", sum[:16])
	b.nodes[0].data = bytes.ReplaceAll(asset, []byte(oldCab), []byte(cab))
	b.nodes[0].name = cab
	return b.save(), cab, nil
}

func encodeCustomArtworkPNG(src image.Image, w, h int) ([]byte, error) {
	var out bytes.Buffer
	e := png.Encode(&out, fitCustomArtwork(src, w, h))
	return out.Bytes(), e
}
