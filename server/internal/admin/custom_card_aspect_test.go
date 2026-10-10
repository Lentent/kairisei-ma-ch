package admin

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
)

// Check the serialized pixels in client display space, independently of the
// fitting implementation. Square uploads must still look square on each quad.
func TestCustomArtworkClientDisplayAspect(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		mode                             string
		srcW, srcH, texW, texH           int
		displayW, displayH, wantW, wantH int
	}{
		{"chr10_98000003", "contain", 64, 64, 512, 512, 400, 560, 512, 365},
		{"chr10_98000003", "contain", 80, 160, 512, 512, 400, 560, 358, 512},
		{"chr10_98000003", "contain", 160, 80, 512, 512, 400, 560, 512, 182},
		{"chr10_98000003", "contain", 64, 64, 512, 256, 400, 560, 512, 182},
		{"chr50_98000003", "", 64, 64, 512, 512, 613, 490, 409, 512},
		{"chr20_98000003", "", 64, 64, 256, 256, 256, 256, 256, 256},
		{"chr60_98000003", "", 64, 64, 1024, 1024, 1024, 1024, 1024, 1024},
	} {
		t.Run(tc.name+"/"+image.Rect(0, 0, tc.srcW, tc.srcH).String()+"/"+image.Rect(0, 0, tc.texW, tc.texH).String(), func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(7, 9, 7+tc.srcW, 9+tc.srcH))
			for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
				for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
					src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
				}
			}
			var raw bytes.Buffer
			_ = binary.Write(&raw, binary.LittleEndian, uint32(len(tc.name)))
			raw.WriteString(tc.name)
			for raw.Len()%4 != 0 {
				raw.WriteByte(0)
			}
			for _, v := range []uint32{uint32(tc.texW), uint32(tc.texH), 16, 33, 1} {
				_ = binary.Write(&raw, binary.LittleEndian, v)
			}
			raw.Write([]byte{1, 1, 0, 0})
			for _, v := range []uint32{1, 2, 1, 1, 0, 0, 0, 0, 16} {
				_ = binary.Write(&raw, binary.LittleEndian, v)
			}
			raw.Write(make([]byte, 16+12))
			encoded, err := rewriteCustomTexture(raw.Bytes(), tc.name, src, tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			c := resourceCursor{b: encoded, order: binary.LittleEndian}
			c.take(int(c.u32()))
			c.align(4)
			c.take(56)
			pixels := c.take(int(c.u32()))
			if len(pixels) != tc.texW*tc.texH*4 {
				t.Fatal("invalid texture payload")
			}
			bounds := image.Rectangle{}
			for y := 0; y < tc.texH; y++ {
				for x := 0; x < tc.texW; x++ {
					if pixels[(y*tc.texW+x)*4+3] != 0 {
						bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
					}
				}
			}
			if bounds.Dx() != tc.wantW || bounds.Dy() != tc.wantH {
				t.Fatalf("content bounds %v, want %dx%d", bounds, tc.wantW, tc.wantH)
			}
			displayedAspect := float64(bounds.Dx()*tc.displayW*tc.texH) / float64(bounds.Dy()*tc.displayH*tc.texW)
			if math.Abs(displayedAspect-float64(tc.srcW)/float64(tc.srcH)) > 0.01 {
				t.Fatalf("client stretches artwork: displayed aspect %f", displayedAspect)
			}
		})
	}
}

func TestCustomArtworkPortraitCoverFillsFrameWithoutStretching(t *testing.T) {
	// Opaque backdrop makes newly inserted letterboxing visible. A square red
	// marker detects distortion even when the source itself is being cropped.
	for _, dimensions := range []image.Point{{128, 128}, {256, 128}, {128, 256}, {100, 140}} {
		t.Run(dimensions.String(), func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(7, 9, 7+dimensions.X, 9+dimensions.Y))
			b := src.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					pixel := color.NRGBA{G: 255, A: 255}
					if x >= b.Min.X+b.Dx()/2-10 && x < b.Min.X+b.Dx()/2+10 && y >= b.Min.Y+b.Dy()/2-10 && y < b.Min.Y+b.Dy()/2+10 {
						pixel = color.NRGBA{R: 255, A: 255}
					}
					src.SetNRGBA(x, y, pixel)
				}
			}
			for _, mode := range []string{"", "cover"} {
				pixels := fitCustomTextureArtwork(src, "chr10_98000003", 512, 512, mode)
				marker := image.Rectangle{}
				for y := 0; y < 512; y++ {
					for x := 0; x < 512; x++ {
						pixel := pixels.NRGBAAt(x, y)
						if pixel.A != 255 {
							t.Fatal("introduced transparent gaps in the portrait frame")
						}
						if pixel.R == 255 {
							marker = marker.Union(image.Rect(x, y, x+1, y+1))
						}
					}
				}
				aspect := float64(marker.Dx()*400) / float64(marker.Dy()*560)
				if math.Abs(aspect-1) > 0.03 {
					t.Fatalf("square marker stretched in the client: %v, aspect %.3f", marker, aspect)
				}
			}
		})
	}
}
