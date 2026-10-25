package admin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

func stampImageName(id int) string { return fmt.Sprintf("chat_stamp_10_%05d", id) }
func stampImageDirectory(id int) string {
	return "05_image_assets/chat/stamp/10/" + fmt.Sprintf("%05d", id)[:3]
}
func stampImagePath(id int) string {
	return "assets/resources/" + stampImageDirectory(id) + "/" + stampImageName(id) + ".png"
}
func resourceAlignedString(b *bytes.Buffer, s string) {
	_ = binary.Write(b, binary.LittleEndian, uint32(len(s)))
	b.WriteString(s)
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}
func resourceObjectName(raw []byte) string {
	c := resourceCursor{b: raw, order: binary.LittleEndian}
	return string(c.take(int(c.u32())))
}

// Build a small standalone bundle with only the template texture and its
// AssetBundle. Original object IDs and type trees remain intact; no other
// official chat textures or aliases are copied into the new namespace.
func buildStampArtworkBundle(raw []byte, scrambled bool, id int, art image.Image) (result []byte, cab string, err error) {
	defer func() {
		if v := recover(); v != nil {
			result = nil
			err = fmt.Errorf("表情图片资源格式不支持：%v", v)
		}
	}()
	if scrambled {
		raw = bytes.Clone(raw)
		key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
		for i := range raw {
			raw[i] -= key[i%8]
		}
	}
	b, err := readResourceBundle(raw)
	if err != nil {
		return nil, "", err
	}
	if len(b.nodes) != 1 || len(b.nodes[0].name) != 36 {
		return nil, "", errors.New("表情模板须为单文件UnityFS资源")
	}
	const templateID = 30001
	var pointer []byte
	_, err = rewriteResourceObjects(b.nodes[0].data, func(class int, object []byte) ([]byte, error) {
		if class != 142 {
			return object, nil
		}
		c := resourceCursor{b: object, order: binary.LittleEndian}
		c.take(int(c.u32()))
		c.align(4)
		n := int(c.u32())
		if n > 100000 {
			return nil, errors.New("表情预加载表过大")
		}
		c.take(n * 12)
		n = int(c.u32())
		if n > 100000 {
			return nil, errors.New("表情目录过大")
		}
		for i := 0; i < n; i++ {
			name := string(c.take(int(c.u32())))
			c.align(4)
			c.take(8)
			ref := c.take(12)
			if name == stampImagePath(templateID) {
				if pointer != nil {
					return nil, errors.New("表情模板路径重复")
				}
				pointer = bytes.Clone(ref)
			}
		}
		return object, nil
	})
	if err != nil {
		return nil, "", err
	}
	if len(pointer) != 12 || binary.LittleEndian.Uint32(pointer) != 0 {
		return nil, "", errors.New("表情模板缺少本地贴图引用")
	}
	textures, containers := 0, 0
	asset, err := rewriteResourceObjects(b.nodes[0].data, func(class int, object []byte) ([]byte, error) {
		if class == 28 {
			textures++
			return rewriteCustomTexture(object, stampImageName(id), art)
		}
		containers++
		var out bytes.Buffer
		resourceAlignedString(&out, "")
		_ = binary.Write(&out, binary.LittleEndian, uint32(1))
		out.Write(pointer)
		_ = binary.Write(&out, binary.LittleEndian, uint32(1))
		resourceAlignedString(&out, stampImagePath(id))
		_ = binary.Write(&out, binary.LittleEndian, uint32(0))
		_ = binary.Write(&out, binary.LittleEndian, uint32(1))
		out.Write(pointer)
		out.Write(make([]byte, 20))
		_ = binary.Write(&out, binary.LittleEndian, uint32(1))
		resourceAlignedString(&out, fmt.Sprintf("image/custom_stamp_%d.unity3d", id))
		out.Write(make([]byte, 8)) // no dependencies; not a streamed scene
		return out.Bytes(), nil
	}, func(class int, object []byte) bool {
		return class == 142 || class == 28 && resourceObjectName(object) == stampImageName(templateID)
	})
	if err != nil {
		return nil, "", err
	}
	if textures != 1 || containers != 1 {
		return nil, "", errors.New("表情模板贴图或容器数量不正确")
	}
	old := b.nodes[0].name
	cab = "cab-" + resourceHash(asset)[:32]
	b.nodes[0].name = cab
	b.nodes[0].data = bytes.ReplaceAll(asset, []byte(old), []byte(cab))
	return b.save(), cab, nil
}
