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

	"kairisei.local/server/internal/cpk"
	"kairisei.local/server/internal/testfixture"
)

func TestCDNLoginKeepsAPIsAndVersionServiceLocal(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	versionNamespace := resourceVersionNamespace([]byte("catalog"), []byte("cpk list"), []byte("cpk state"))
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
		cnBootstrapLogin(accounts, "10.0.2.2", 26020, config, versionNamespace, "images")(response,
			httptest.NewRequest(http.MethodPost, "/loginSDK.php", strings.NewReader(`{"uuid":"00000000-0000-0000-0000-000000000463","clver":"`+cnMinimumClientVersion+`"}`)))
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		root := "http://10.0.2.2:26020"
		if result["api_url"] != root+"/" || result["version_url"] != root+"/local/version/"+versionNamespace+"/" {
			t.Fatalf("CDN changed non-payload routes: %v", result)
		}
		if base == "" {
			base = root + "/local/resources/"
		}
		if result["res_patch_url"] != base+"patch/" || result["res_cpk_url"] != base+"cpk/" || result["res_img_url"] != base+"image/images/" {
			t.Fatalf("unexpected resource URLs: %v", result)
		}
	}
	for _, base := range []string{"file:///tmp/assets", "https://user:secret@cdn.test", "https://cdn.test/?token=secret"} {
		if _, err := (CDNConfig{BaseURL: base}).normalized(); err == nil {
			t.Fatalf("invalid public CDN configuration accepted: %s", base)
		}
	}
}

func TestCPKSphereMoviesUseBattleCategory(t *testing.T) {
	files := []cpk.File{
		{Name: "mov_16000011.cpk", Size: 1048576, Version: 1},
		{Name: "mov_16000400.cpk", Size: 1048576, Version: 1},
		{Name: "mov_op.cpk", Size: 1048576, Version: 1},
		{Name: "mov_prologue.cpk", Size: 1048576, Version: 1},
		{Name: "mov_unknown.cpk", Size: 1048576, Version: 1},
	}
	list := string(cpk.FileList(files))
	for _, row := range []string{
		"mov_16000011.cpk,SPHR_MOVIE,16000011,1.000000,ALL,1048576\n",
		"mov_16000400.cpk,SPHR_MOVIE,16000400,1.000000,ALL,1048576\n",
		"mov_op.cpk,MOVIE,1,1.000000,ALL,1048576\n",
		"mov_prologue.cpk,MOVIE,0,1.000000,ALL,1048576\n",
		"mov_unknown.cpk,MOVIE,0,1.000000,ALL,1048576\n",
	} {
		if !strings.Contains(list, row) {
			t.Fatalf("missing native movie category: %q in %s", row, list)
		}
	}
	oldList := strings.ReplaceAll(list, ",SPHR_MOVIE,", ",MOVIE,")
	state := cpk.PatchState(files)
	if resourceVersionNamespace(nil, []byte(oldList), state) == resourceVersionNamespace(nil, []byte(list), state) {
		t.Fatal("CPK category correction must refresh the client manifest URL without changing movie bytes")
	}
}

func TestCDNExportUsesOverlayCRCAndPhysicalAliasOwner(t *testing.T) {
	root := t.TempDir()
	images, err := loadCardImages(writeTestCardImages(t, root))
	if err != nil {
		t.Fatal(err)
	}
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
	write("cpk/versions.json", []byte(`{"schema_version":1,"versions":[{"name":"cv_navi_5.cpk","version":3},{"name":"cv_tb_1007.cpk","version":2}]}`))
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
	write("aliases.json", []byte(`{"schema_version":2,"client_profile":"cn602-bootstrap","aliases":[{"alias_cpk_name":"cv_navi_94.cpk","source_cpk_name":"cv_navi_5.cpk"}]}`))
	files := []map[string]any{}
	for _, name := range []string{"patch/" + cn602MenuBundle, "cpk/cv_navi_5.cpk", "cpk/cv_tb_1007.cpk", "cpk/versions.json", "image/manifest.json", "image/chr51/chr51_10000010.png"} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, map[string]any{"path": name, "bytes": len(content), "sha256": fmt.Sprintf("%x", sha256.Sum256(content))})
	}
	manifest, _ := json.Marshal(map[string]any{"schema_version": 1, "client_profile": "cn602-bootstrap", "path_base": "RESOURCE_SET_ROOT", "files": files,
		"entrypoints": map[string]string{"cn-patch-root": "patch", "cn-cpk-root": "cpk", "cn-cpk-aliases": "aliases.json", "cn-asset-map": filepath.Base(assetPath), "cn-image-root": "image"}})
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
	if err := json.Unmarshal(content, &result); err != nil || len(result.Files) != 5 {
		t.Fatalf("export: %v %s", err, content)
	}
	aliases, err := cpk.LoadAliases(filepath.Join(root, "cpk"), filepath.Join(root, "aliases.json"))
	if err != nil {
		t.Fatal(err)
	}
	cpkFiles, err := cpk.Load(filepath.Join(root, "cpk"), aliases)
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
	router.Get("/cpk/*", cnBootstrapCPKResource(cpkFiles))
	router.Get("/image/*", images.serveHTTP)
	router.Head("/image/*", images.serveHTTP)
	for _, object := range result.Files {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+object.Key, nil))
		if response.Code != 200 || fmt.Sprintf("%x", sha256.Sum256(response.Body.Bytes())) != object.SHA256 {
			t.Fatalf("export/HTTP differ for %s: %d", object.Key, response.Code)
		}
	}
	imageURL := "/image/" + images.Namespace + "/chr51/chr51_10000010.png"
	head := httptest.NewRecorder()
	router.ServeHTTP(head, httptest.NewRequest(http.MethodHead, imageURL, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("enlarged image HEAD failed: %d %v", head.Code, head.Header())
	}
	ranged := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, imageURL, nil)
	request.Header.Set("Range", "bytes=0-7")
	router.ServeHTTP(ranged, request)
	if ranged.Code != http.StatusPartialContent || ranged.Body.String() != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("enlarged image range failed: %d", ranged.Code)
	}
	for _, url := range []string{"/image/stale/chr51/chr51_10000010.png", "/image/" + images.Namespace + "/manifest.json"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("unregistered enlarged image served: %s", url)
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

	// A data-only revision must update HTTP, aliases, CDN keys and login cache
	// identity together, without changing the catalog's official version.
	beforeNamespace := resourceVersionNamespace(catalog, cpk.FileList(cpkFiles), cpk.PatchState(cpkFiles))
	policy := []byte(`{"schema_version":1,"versions":[{"name":"cv_navi_5.cpk","version":4}]}`)
	write("cpk/versions.json", policy)
	if _, err := BuildCDNManifest(filepath.Join(root, "resource-set.json")); err == nil {
		t.Fatal("unregistered CPK version metadata accepted")
	}
	for i := range files {
		if files[i]["path"] == "cpk/versions.json" {
			files[i] = map[string]any{"path": "cpk/versions.json", "bytes": len(policy), "sha256": fmt.Sprintf("%x", sha256.Sum256(policy))}
		}
	}
	var resourceSet map[string]any
	if err := json.Unmarshal(manifest, &resourceSet); err != nil {
		t.Fatal(err)
	}
	resourceSet["files"] = files
	manifest, _ = json.Marshal(resourceSet)
	write("resource-set.json", manifest)
	updated, err := BuildCDNManifest(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	cpkFiles, err = cpk.Load(filepath.Join(root, "cpk"), aliases)
	if err != nil {
		t.Fatal(err)
	}
	updatedRouter := chi.NewRouter()
	updatedRouter.Get("/patch/Android/patch/*", cnBootstrapVersionedPatchFile([]string{filepath.Join(root, "patch")}, patch))
	updatedRouter.Get("/cpk/*", cnBootstrapCPKResource(cpkFiles))
	updatedRouter.Get("/image/*", images.serveHTTP)
	for _, object := range updated.Files {
		response := httptest.NewRecorder()
		updatedRouter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+object.Key, nil))
		if response.Code != http.StatusOK || fmt.Sprintf("%x", sha256.Sum256(response.Body.Bytes())) != object.SHA256 {
			t.Fatalf("updated HTTP/CDN differ for %s", object.Key)
		}
	}
	for _, file := range cpkFiles {
		if (file.Name == "cv_navi_5.cpk" || file.Name == "cv_navi_94.cpk") && file.Version != 4 {
			t.Fatalf("source revision did not reach alias: %+v", file)
		}
	}
	list, state := cpk.FileList(cpkFiles), cpk.PatchState(cpkFiles)
	afterNamespace := resourceVersionNamespace(catalog, list, state)
	if afterNamespace == beforeNamespace || afterNamespace != resourceVersionNamespace(catalog, list, state) {
		t.Fatal("resource namespace must be stable and reflect voice revisions")
	}
	registerResourceVersions(updatedRouter, []string{filepath.Join(root, "patch")}, catalog, list, state)
	for _, namespace := range []string{"default", "cached-login", beforeNamespace, afterNamespace} {
		response := httptest.NewRecorder()
		updatedRouter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/local/version/"+namespace+"/CPK/cpk_patch.txt", nil))
		if response.Code != http.StatusOK || response.Body.String() != string(state) {
			t.Fatalf("login namespace cannot resume downloads: %s", namespace)
		}
	}
	write("cpk/versions.json", []byte(`{"schema_version":1,"versions":[{"name":"cv_navi_5.cpk","version":2}]}`))

	if _, err := BuildCDNManifest(filepath.Join(root, "resource-set.json")); err == nil {
		t.Fatal("modified CPK version metadata accepted")
	}
}
