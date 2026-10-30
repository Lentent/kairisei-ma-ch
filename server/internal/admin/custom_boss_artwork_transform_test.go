package admin

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestCustomBossArtworkTransform(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	src.SetNRGBA(40, 40, color.NRGBA{R: 255, A: 255})
	original := bytes.Clone(src.Pix)
	if transformCustomBossArtwork(src, nil) != src || transformCustomBossArtwork(src, &customBossArtworkTransform{Scale: 100}) != src {
		t.Fatal("default changed image")
	}
	moved := transformCustomBossArtwork(src, &customBossArtworkTransform{X: 10, Y: -20, Scale: 100})
	if moved.NRGBAAt(50, 20).R != 255 || moved.NRGBAAt(40, 40).A != 0 {
		t.Fatal("offset direction or distance incorrect")
	}
	scaled := transformCustomBossArtwork(src, &customBossArtworkTransform{Scale: 200})
	if scaled.NRGBAAt(30, 30).R != 255 || scaled.NRGBAAt(31, 31).R != 255 {
		t.Fatal("scale must be centered")
	}
	clipped := transformCustomBossArtwork(src, &customBossArtworkTransform{X: 100, Scale: 100})
	if !bytes.Equal(clipped.Pix, make([]byte, len(clipped.Pix))) {
		t.Fatal("out-of-canvas pixels were not clipped")
	}
	if !bytes.Equal(src.Pix, original) {
		t.Fatal("source pixels were modified")
	}
}

func TestCustomBossArtworkTransformValidationAndHash(t *testing.T) {
	for _, v := range []customBossArtworkTransform{{Scale: 0}, {Scale: 301}, {X: 101, Scale: 100}, {Y: -101, Scale: 100}, {X: math.Inf(1), Scale: 100}, {Y: math.NaN(), Scale: 100}} {
		if validateCustomBossArtworkTransform(&v, true) == nil {
			t.Fatalf("accepted invalid transform: %+v", v)
		}
	}
	v := &customBossArtworkTransform{X: -10, Y: 20, Scale: 80}
	if validateCustomBossArtworkTransform(v, false) == nil {
		t.Fatal("accepted transform without upload")
	}
	if err := validateCustomBossArtworkTransform(v, true); err != nil {
		t.Fatal(err)
	}
	b := customBoss{Targets: []dropTarget{{Artwork: []byte("source")}}}
	before := customBossAppearanceHash(b)
	b.Targets[0].ArtworkTransform = v
	if before == customBossAppearanceHash(b) {
		t.Fatal("transform did not invalidate published appearance")
	}
}

func TestCustomBossTargetPosition(t *testing.T) {
	for _, p := range []customBossTargetPosition{{X: 2001}, {Y: -2001}, {X: math.NaN()}, {Y: math.Inf(1)}} {
		if validateCustomBossTargetPosition(&p, true) == nil {
			t.Fatalf("accepted invalid position: %+v", p)
		}
	}
	p := &customBossTargetPosition{X: 120.5, Y: -35}
	if validateCustomBossTargetPosition(p, false) == nil {
		t.Fatal("accepted non-static target")
	}
	if err := validateCustomBossTargetPosition(p, true); err != nil {
		t.Fatal(err)
	}
	b := customBoss{Targets: []dropTarget{{}}}
	before := customBossAppearanceHash(b)
	b.Targets[0].TargetPosition = p
	if before == customBossAppearanceHash(b) {
		t.Fatal("target change did not invalidate appearance")
	}
	clone := cloneBossTargets(b.Targets)
	clone[0].TargetPosition.X = 0
	if p.X != 120.5 {
		t.Fatal("draft target coordinates alias saved state")
	}
}
