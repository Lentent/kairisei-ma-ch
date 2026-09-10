package cnbootstrap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestApplyCNAvatarShopAssetClosureRequiresIconAndModel(t *testing.T) {
	t.Parallel()
	state := release.State{AvatarPartDefinitions: []release.AvatarPartDefinition{
		{PartID: 50, IconPictID: 50},
		{PartID: 602, IconPictID: 602},
		{PartID: 790, IconPictID: 790},
	}}
	available := map[string]struct{}{
		"avatar_parts/icon/avatar_00050_icon.dat":   {},
		"avatar_parts/icon/avatar_00602_icon.dat":   {},
		"avatar_parts/parts/avatar_00602_parts.dat": {},
		"avatar_parts/parts/avatar_00790_parts.dat": {},
	}
	if err := applyCNAvatarShopAssetClosure(&state, available); err != nil {
		t.Fatal(err)
	}
	if want := []int{602}; !reflect.DeepEqual(state.AvatarShopPartIDs, want) {
		t.Fatalf("asset-closed shop IDs = %v, want %v", state.AvatarShopPartIDs, want)
	}
}

func TestLoadCNAssetMapDeliveryCRC32(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	version := []byte("test version")
	digest := sha256.Sum256(version)
	manifest := map[string]any{
		"schema_version": 2,
		"client_profile": "cn602-bootstrap",
		"source": map[string]any{
			"catalog_version":                    cn602CatalogVersion,
			"version_dat_sha256":                 fmt.Sprintf("%x", digest),
			"parsed_unity_bundle_count":          1,
			"non_unity_bundle_count":             0,
			"bundle_dependency_edge_count":       0,
			"unresolved_bundle_dependency_count": 0,
			"scrambled_bundle_count":             1,
			"plain_bundle_count":                 0,
			"surviving_official_catalog_overlay": testSurvivingOfficialCatalogOverlay(),
		},
		"bundles": []map[string]any{{
			"bundle":         cn602MenuBundle,
			"cab_name":       "cab-test",
			"scrambled":      true,
			"delivery_crc32": "143c9c4d",
			"dependencies":   []string{},
		}},
		"catalog_assets": []map[string]any{{
			"directory": "scenes",
			"name":      "Menu",
			"base_dir":  "Assets/Plugins/rhyme/EeL/Resources/",
			"extension": ".prefab",
			"bundle":    cn602MenuBundle,
		}},
	}
	contents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "asset-map.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	assets, metadata, err := loadCNAssetMap(
		path,
		fmt.Sprintf("%x", digest),
		map[string]struct{}{cn602MenuBundle: {}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := metadata[cn602MenuBundle].deliveryCRC32; got != "143C9C4D" {
		t.Fatalf("delivery CRC = %q, want 143C9C4D", got)
	}
	if len(assets) != 1 || assets[0].baseDir != "Assets/Plugins/rhyme/EeL/Resources/" {
		t.Fatalf("catalog base directory = %#v, want preserved nonstandard base directory", assets)
	}
	delete(manifest["source"].(map[string]any), "surviving_official_catalog_overlay")
	contents, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	missingOverlayPath := filepath.Join(root, "asset-map-missing-overlay.json")
	if err := os.WriteFile(missingOverlayPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCNAssetMap(
		missingOverlayPath,
		fmt.Sprintf("%x", digest),
		map[string]struct{}{cn602MenuBundle: {}},
	); err == nil {
		t.Fatal("asset map without verified surviving catalog overlay was accepted")
	}
}
