package cnbootstrap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCDNLoginKeepsAPIsAndVersionServiceLocal(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	for _, base := range []string{"", "https://cdn.example.test/game/v1/"} {
		configPath := filepath.Join(t.TempDir(), "cdn.json")
		content, _ := json.Marshal(CDNConfig{BaseURL: base})
		if err := os.WriteFile(configPath, content, 0600); err != nil {
			t.Fatal(err)
		}
		config, err := LoadCDNConfig(configPath)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		cnBootstrapLogin(accounts, "10.0.2.2", 26020, config)(response,
			httptest.NewRequest(http.MethodPost, "/loginSDK.php", strings.NewReader(`{"uuid":"00000000-0000-0000-0000-000000000463","clver":"6.0.4"}`)))
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		root := "http://10.0.2.2:26020"
		if result["api_url"] != root+"/" || result["version_url"] != root+"/local/version/"+cn602VersionNamespace+"/" || result["res_img_url"] != root+"/local/resources/image/" {
			t.Fatalf("CDN changed non-payload routes: %v", result)
		}
		if base == "" {
			base = root + "/local/resources/"
		}
		if result["res_patch_url"] != base+"patch/" || result["res_cpk_url"] != base+"cpk/" {
			t.Fatalf("unexpected resource URLs: %v", result)
		}
	}
	for _, base := range []string{"file:///tmp/assets", "https://user:secret@cdn.test", "https://cdn.test/?token=secret"} {
		if _, err := (CDNConfig{BaseURL: base}).normalized(); err == nil {
			t.Fatalf("invalid public CDN configuration accepted: %s", base)
		}
	}
}

func TestCDNExportUsesOverlayCRCAndPhysicalAliasOwner(t *testing.T) {
	root := t.TempDir()
	write := func(name string, content []byte) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	version := []byte("<version>,790\n<bundle_ver>,main_c/scenes/scene_Menu.dat,0,93936638\n")
	for i := range version {
		version[i] += cn602ScrambleKey[i%len(cn602ScrambleKey)]
	}
	write("patch/version.dat", version)
	write("patch/"+cn602MenuBundle, []byte("menu"))
	write("cpk/cv_navi_5.cpk", []byte("voice bytes"))
	write("cpk/cv_tb_1007.cpk", []byte("complete boss voice bytes"))
	assetPath := writeTestAssetMap(t, root, version)
	content, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	var assets map[string]any
	if err := json.Unmarshal(content, &assets); err != nil {
		t.Fatal(err)
	}
	assets["bundles"].([]any)[0].(map[string]any)["delivery_crc32"] = "f00dcafe"
	content, _ = json.Marshal(assets)
	write(filepath.Base(assetPath), content)
	write("aliases.json", []byte(`{"schema_version":1,"client_profile":"cn602-bootstrap","aliases":[{"alias_cpk_name":"cv_navi_94.cpk","source_cpk_name":"cv_navi_5.cpk"}]}`))
	files := []map[string]any{}
	for _, name := range []string{"patch/" + cn602MenuBundle, "cpk/cv_navi_5.cpk", "cpk/cv_tb_1007.cpk"} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, map[string]any{"path": name, "bytes": len(content), "sha256": fmt.Sprintf("%x", sha256.Sum256(content))})
	}
	manifest, _ := json.Marshal(map[string]any{"schema_version": 1, "client_profile": "cn602-bootstrap", "path_base": "RESOURCE_SET_ROOT", "files": files,
		"entrypoints": map[string]string{"cn-patch-root": "patch", "cn-cpk-root": "cpk", "cn-cpk-aliases": "aliases.json", "cn-asset-map": filepath.Base(assetPath)}})
	write("resource-set.json", manifest)
	if err := ExportCDNManifest(filepath.Join(root, "resource-set.json"), filepath.Join(root, "cdn-output.json")); err == nil {
		t.Fatal("export wrote inside the read-only resource set")
	}
	output := filepath.Join(t.TempDir(), "cdn.json")
	if err := ExportCDNManifest(filepath.Join(root, "resource-set.json"), output); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Files []cdnObject `json:"files"`
	}
	if err := json.Unmarshal(content, &result); err != nil || len(result.Files) != 4 {
		t.Fatalf("export: %v %s", err, content)
	}
	aliases, err := loadCPKAliases(filepath.Join(root, "cpk"), filepath.Join(root, "aliases.json"))
	if err != nil {
		t.Fatal(err)
	}
	cpk, err := loadCNCPKDelivery(filepath.Join(root, "cpk"), aliases)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := buildCN602Catalog([]string{filepath.Join(root, "patch")}, assetPath)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := cnPatchDeliveryFromCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Get("/patch/Android/patch/*", cnBootstrapVersionedPatchFile([]string{filepath.Join(root, "patch")}, patch))
	router.Get("/cpk/*", cnBootstrapCPKResource(cpk))
	for _, object := range result.Files {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+object.Key, nil))
		if response.Code != 200 || fmt.Sprintf("%x", sha256.Sum256(response.Body.Bytes())) != object.SHA256 {
			t.Fatalf("export/HTTP differ for %s: %d", object.Key, response.Code)
		}
	}
	if !strings.Contains(string(content), cn602MenuBundle+".vF00DCAFE") || !strings.Contains(string(content), "cv_navi_94.cpk.v3") || !strings.Contains(string(content), "cv_tb_1007.cpk.v2") {
		t.Fatalf("overlay CRC or alias source version lost: %s", content)
	}
	old := httptest.NewRecorder()
	router.ServeHTTP(old, httptest.NewRequest(http.MethodGet, "/patch/Android/patch/"+cn602MenuBundle+".v93936638", nil))
	if old.Code != http.StatusNotFound {
		t.Fatalf("stale version served current bytes: %d", old.Code)
	}
	if err := ExportCDNManifest(filepath.Join(root, "resource-set.json"), output); err == nil {
		t.Fatal("export overwrote an existing manifest")
	}
}
