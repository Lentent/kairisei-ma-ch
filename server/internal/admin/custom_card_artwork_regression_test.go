package admin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCustomArtworkTestTexture(t *testing.T, root string, entry customArtworkTestAsset) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "resources/patch", filepath.FromSlash(entry.Bundle)))
	if err != nil {
		t.Fatal(err)
	}
	assetsRaw, err := os.ReadFile(filepath.Join(root, "asset-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	var assets map[string]any
	if err := json.Unmarshal(assetsRaw, &assets); err != nil {
		t.Fatal(err)
	}
	for _, v := range assets["bundles"].([]any) {
		info := v.(map[string]any)
		if info["bundle"] == entry.Bundle && info["scrambled"].(bool) {
			key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
			for i := range raw {
				raw[i] -= key[i%8]
			}
		}
	}
	bundle, err := readResourceBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	var texture []byte
	_, err = rewriteResourceObjects(bundle.nodes[0].data, func(class int, object []byte) ([]byte, error) {
		if class == 28 {
			cursor := resourceCursor{b: object, order: binary.LittleEndian}
			if string(cursor.take(int(cursor.u32()))) == entry.Name {
				texture = bytes.Clone(object)
			}
		}
		return object, nil
	})
	if err != nil || texture == nil {
		t.Fatalf("template texture missing: %s (%v)", entry.Name, err)
	}
	return texture
}

func TestCustomCardIndependentIconAndIllustration(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires complete game resources")
	}
	a := &API{collectionResourceRoot: root}
	s, err := a.customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.template(10108001)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = 98012346
	makeArt := func(pixel color.NRGBA) []byte {
		im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				im.SetNRGBA(x, y, pixel)
			}
		}
		var out bytes.Buffer
		if err := png.Encode(&out, im); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	c.IconArtwork = makeArt(color.NRGBA{G: 255, A: 255})
	illustration := makeArt(color.NRGBA{R: 255, A: 255})
	assetsRaw, err := os.ReadFile(filepath.Join(root, "asset-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, iconOnly := range []bool{false, true} {
		name := "different_images"
		if iconOnly {
			name = "icon_only_preserves_template"
		}
		t.Run(name, func(t *testing.T) {
			card := c
			if !iconOnly {
				card.Artwork = illustration
			}
			row, _, _, _, err := materializeCustomCard(card, s)
			if err != nil {
				t.Fatal(err)
			}
			if customRowInt(row, 36) != card.ID {
				t.Fatal("icon-only upload retained template PictID")
			}
			var assets, manifest map[string]any
			if err := json.Unmarshal(assetsRaw, &assets); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			if err := buildCustomCardImages(card, row, s, assets, manifest, files, map[string][]byte{}, root); err != nil {
				t.Fatal(err)
			}
			generated, err := json.Marshal(assets)
			if err != nil {
				t.Fatal(err)
			}
			assertCustomArtworkExport(t, card, s, assetsRaw, generated, files, "")
			preview, err := png.Decode(bytes.NewReader(files["_local/control/server/cn602-admin-assets/card/98012346.png"]))
			if err != nil {
				t.Fatal(err)
			}
			if color.NRGBAModel.Convert(preview.At(80, 80)).(color.NRGBA).G != 255 {
				t.Fatal("admin preview did not use icon")
			}
			var expected []byte
			if iconOnly {
				expected, err = os.ReadFile(filepath.Join(root, "resources/image/chr51/chr51_10000222.png"))
			} else {
				expected = illustration
			}
			if err != nil {
				t.Fatal(err)
			}
			im, _, err := image.Decode(bytes.NewReader(expected))
			if err != nil {
				t.Fatal(err)
			}
			want, err := encodeCustomArtworkPNG(im, 1024, 1024)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(files["resources/image/chr51/chr51_98012346.png"], want) {
				t.Fatal("enlarged illustration used the icon")
			}
		})
	}
}

func TestNewCustomCardArtworkResourceClosure(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires complete game resources")
	}
	a := &API{collectionResourceRoot: root}
	s, err := a.customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.template(10108001)
	if err != nil {
		t.Fatal(err)
	}
	// Use a new card outside the first bucket to exercise directory generation.
	c.ID = 98012345
	art := image.NewNRGBA(image.Rect(0, 0, 64, 96))
	art.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, art); err != nil {
		t.Fatal(err)
	}
	c.Artwork = encoded.Bytes()
	row, _, _, _, err := materializeCustomCard(c, s)
	if err != nil {
		t.Fatal(err)
	}
	sourceAssets, err := os.ReadFile(filepath.Join(root, "asset-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	rawManifest, err := os.ReadFile(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		change    func(map[string]any)
		wantError bool
	}{
		{"missing_optional_container_path", func(assets map[string]any) {
			for _, v := range assets["catalog_assets"].([]any) {
				entry := v.(map[string]any)
				if strings.HasSuffix(entry["name"].(string), "_10000222") {
					delete(entry, "container_path")
				}
			}
		}, false},
		{"missing_main_card_image", func(assets map[string]any) { removeCustomArtworkTestEntry(assets, "chr10_10000222") }, true},
		{"missing_card_icon", func(assets map[string]any) { removeCustomArtworkTestEntry(assets, "chr20_10000222") }, true},
		{"invalid_embedded_container_reference", func(assets map[string]any) {
			for _, v := range assets["catalog_assets"].([]any) {
				entry := v.(map[string]any)
				if entry["name"] == "chr10_10000222" {
					entry["container_path"] = strings.TrimSuffix(entry["container_path"].(string), ".pvr") + ".bad"
				}
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var assets, manifest map[string]any
			if err := json.Unmarshal(sourceAssets, &assets); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rawManifest, &manifest); err != nil {
				t.Fatal(err)
			}
			tc.change(assets)
			files := map[string][]byte{}
			err := buildCustomCardImages(c, row, s, assets, manifest, files, map[string][]byte{}, root)
			if tc.wantError {
				if err == nil {
					t.Fatal("accepted resources that would leave ordinary card artwork unavailable")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			generated, err := json.Marshal(assets)
			if err != nil {
				t.Fatal(err)
			}
			assertCustomArtworkExport(t, c, s, sourceAssets, generated, files, "")
		})
	}
}

func removeCustomArtworkTestEntry(assets map[string]any, name string) {
	rows := assets["catalog_assets"].([]any)
	kept := rows[:0]
	for _, row := range rows {
		if row.(map[string]any)["name"] != name {
			kept = append(kept, row)
		}
	}
	assets["catalog_assets"] = kept
}
