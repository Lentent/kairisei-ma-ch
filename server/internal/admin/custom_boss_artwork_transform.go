package admin

import (
	"errors"
	"image"
	"math"
)

// Offsets are percentages of the output texture; positive X/Y move right/down.
// Scale is centered on the canvas. The original upload is always preserved.
type customBossArtworkTransform struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Scale float64 `json:"scale"`
}

// Client UI coordinates relative to the sprite's target anchor.
type customBossTargetPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func validateCustomBossTargetPosition(p *customBossTargetPosition, static bool) error {
	if p == nil {
		return nil
	}
	if !static {
		return errors.New("只有普通2D部位支持调整光标位置")
	}
	if math.IsNaN(p.X) || math.IsInf(p.X, 0) || math.Abs(p.X) > 2000 || math.IsNaN(p.Y) || math.IsInf(p.Y, 0) || math.Abs(p.Y) > 2000 {
		return errors.New("光标坐标须为-2000至2000")
	}
	return nil
}

func validateCustomBossArtworkTransform(t *customBossArtworkTransform, uploaded bool) error {
	if t == nil {
		return nil
	}
	if !uploaded {
		return errors.New("请先为普通2D部位上传PNG，再调整位置和缩放")
	}
	if math.IsNaN(t.X) || math.IsInf(t.X, 0) || math.Abs(t.X) > 100 || math.IsNaN(t.Y) || math.IsInf(t.Y, 0) || math.Abs(t.Y) > 100 || math.IsNaN(t.Scale) || math.IsInf(t.Scale, 0) || t.Scale < 10 || t.Scale > 300 {
		return errors.New("部位横向、纵向偏移须为-100至100%，缩放须为10至300%")
	}
	return nil
}

func transformCustomBossArtwork(src *image.NRGBA, t *customBossArtworkTransform) *image.NRGBA {
	if t == nil || t.X == 0 && t.Y == 0 && t.Scale == 100 {
		return src
	}
	b := src.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	scale := t.Scale / 100
	x0, y0 := w*(1-scale)/2+w*t.X/100, h*(1-scale)/2+h*t.Y/100
	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		sy := int(math.Floor((float64(y-b.Min.Y) + 0.5 - y0) / scale))
		if sy < 0 || sy >= b.Dy() {
			continue
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			sx := int(math.Floor((float64(x-b.Min.X) + 0.5 - x0) / scale))
			if sx >= 0 && sx < b.Dx() {
				dst.SetNRGBA(x, y, src.NRGBAAt(b.Min.X+sx, b.Min.Y+sy))
			}
		}
	}
	return dst
}
