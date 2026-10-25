package admin

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/cpk"
	"kairisei.local/server/internal/criaudio"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func stampTestDraft(t *testing.T) customStampDraft {
	t.Helper()
	var buf bytes.Buffer
	im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	im.SetNRGBA(20, 20, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	audio := customTestVoiceWAV()
	for i := 44; i < len(audio); i += 2 {
		binary.LittleEndian.PutUint16(audio[i:], 12000)
	}
	return customStampDraft{Stamps: []customStamp{
		{ID: 980001, Name: "文字测试", Kind: "text", Category: 2, Message: "来叠连吧！", Volume: 100},
		{ID: 980002, Name: "图片测试", Kind: "image", Category: 1, Message: "", Artwork: buf.Bytes(), Volume: 100},
		{ID: 980003, Name: "声音测试", Kind: "sound", Category: 1, Message: "加油！", Artwork: buf.Bytes(), Audio: audio, Volume: 50},
	}}
}
func stampTestAPI(t *testing.T) *API {
	t.Helper()
	root := t.TempDir()
	path := testfixture.WriteTestStampMaster(t, root)
	master, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, filepath.FromSlash(customStampMasterPath))
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, master, 0600); err != nil {
		t.Fatal(err)
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &API{operations: ops, collectionResourceRoot: root}
}
func TestCustomStampDraftValidationAndRevision(t *testing.T) {
	a := stampTestAPI(t)
	r := chi.NewRouter()
	r.Get("/stamps", a.customStamps)
	r.Put("/stamps", a.saveCustomStamps)
	r.Post("/export", a.exportCustomStamps)
	d := stampTestDraft(t)
	testfixture.CallContentAdmin(t, r, http.MethodPut, "/stamps", map[string]any{"expected_revision": 0, "config": d}, 200)
	var saved struct {
		Revision int              `json:"revision"`
		Config   customStampDraft `json:"config"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, r, http.MethodGet, "/stamps", nil, 200), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || len(saved.Config.Stamps) != 3 || !bytes.Equal(saved.Config.Stamps[2].Audio, d.Stamps[2].Audio) {
		t.Fatal("draft media did not persist")
	}
	testfixture.CallContentAdmin(t, r, http.MethodPut, "/stamps", map[string]any{"expected_revision": 0, "config": d}, 409)
	testfixture.CallContentAdmin(t, r, http.MethodPost, "/export", map[string]any{"expected_revision": 0}, 409)
	d.Stamps[0].Message = "bad,CSV"
	testfixture.CallContentAdmin(t, r, http.MethodPut, "/stamps", map[string]any{"expected_revision": 1, "config": d}, 400)
	d = stampTestDraft(t)
	d.Stamps[1].Artwork = []byte("not an image")
	if a.validateCustomStamps(&d) == nil {
		t.Fatal("invalid image accepted")
	}
	d = stampTestDraft(t)
	d.Stamps[2].Audio = []byte("not audio")
	if a.validateCustomStamps(&d) == nil {
		t.Fatal("invalid audio accepted")
	}
	d = stampTestDraft(t)
	d.Stamps[2].Volume = 101
	if a.validateCustomStamps(&d) == nil {
		t.Fatal("invalid volume accepted")
	}
	// Only a resource ownership receipt permits editing an occupied ID.
	root := a.collectionResourceRoot
	var m map[string]any
	if err := readResourceJSON(root, customStampMasterPath, &m); err != nil {
		t.Fatal(err)
	}
	m["stamps"] = append(m["stamps"].([]any), map[string]any{"stamp_id": 980001, "category": 2, "label": "published", "display_text": "published", "default_owned": false})
	write := func() {
		raw, _ := resourceJSON(m)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(customStampMasterPath)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	d = stampTestDraft(t)
	if a.validateCustomStamps(&d) == nil {
		t.Fatal("unowned resource ID overwritten")
	}
	m["source"].(map[string]any)["admin_custom_stamps"] = []int{980001}
	write()
	if err := a.validateCustomStamps(&d); err != nil {
		t.Fatal(err)
	}
	d.Stamps = d.Stamps[1:]
	if a.validateCustomStamps(&d) == nil {
		t.Fatal("published ID removed")
	}
}
func TestCustomStampRuntimeExport(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("requires full runtime resource set")
	}
	a := &API{collectionResourceRoot: root}
	d := stampTestDraft(t)
	if err := a.validateCustomStamps(&d); err != nil {
		t.Fatal(err)
	}
	patch, err := a.buildCustomStampPatch(d)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(patch), int64(len(patch)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		files[f.Name], e = io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	staging := t.TempDir()
	for name, body := range files {
		if !strings.HasPrefix(name, "resource-set/") {
			continue
		}
		target := filepath.Join(staging, strings.TrimPrefix(name, "resource-set/"))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := masterdata.LoadStampRuntimeMaster(filepath.Join(staging, filepath.FromSlash(customStampMasterPath)))
	if err != nil {
		t.Fatal(err)
	}
	var state gamestate.State
	if _, err = masterdata.ApplyStampRuntimeMaster(&state, loaded); err != nil {
		t.Fatal(err)
	}
	for _, id := range state.Stamps.StampIDs {
		if id >= customStampFirstID {
			t.Fatal("custom stamp automatically granted")
		}
	}
	for _, def := range state.CollectionRewards {
		if def.Type == 16 && def.ID == 980002 && (def.Name != "图片测试" || def.PictID != 980002) {
			t.Fatal("image reward catalog metadata")
		}
	}
	for _, id := range []int{980002, 980003} {
		name := "resource-set/resources/patch/main_c/image/custom_stamp_" + strings.TrimPrefix(stampImageName(id), "chat_stamp_10_") + ".dat"
		b, e := readResourceBundle(files[name])
		if e != nil {
			t.Fatal(e)
		}
		classes := map[int]int{}
		_, e = rewriteResourceObjects(b.nodes[0].data, func(class int, raw []byte) ([]byte, error) {
			classes[class]++
			if class == 142 && !bytes.Contains(raw, []byte(stampImagePath(id))) {
				t.Fatal("texture container path missing")
			}
			if class == 28 && resourceObjectName(raw) != stampImageName(id) {
				t.Fatal("wrong texture name")
			}
			return raw, nil
		})
		if e != nil || classes[28] != 1 || classes[142] != 1 || len(classes) != 2 {
			t.Fatal("standalone texture closure", classes, e)
		}
	}
	if len(files["resource-set/resources/cpk/"+customStampVoiceCPK]) == 0 {
		t.Fatal("sound CPK missing")
	}
	if _, err = criaudio.ReadCPKEntry(files["resource-set/resources/cpk/"+customStampVoiceCPK], "902000003.acb"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"resources/patch/main_c/image/image_chat.dat", "resources/cpk/CueSheet_Card_0.cpk"} {
		raw, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if e != nil {
			t.Fatal(e)
		}
		if err = os.WriteFile(filepath.Join(staging, filepath.FromSlash(name)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Re-export from the applied resource set, editing a voice and converting
	// an image to text while retaining both public identities.
	a2 := &API{collectionResourceRoot: staging}
	d.Stamps[1].Kind = "text"
	d.Stamps[1].Artwork = nil
	d.Stamps[1].Message = "改为文字"
	d.Stamps[2].Volume = 25
	if err = a2.validateCustomStamps(&d); err != nil {
		t.Fatal(err)
	}
	second, err := a2.buildCustomStampPatch(d)
	if err != nil {
		t.Fatal(err)
	}
	z2, err := zip.NewReader(bytes.NewReader(second), int64(len(second)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z2.File {
		if f.Name != "resource-set/resources/cpk/versions.json" && f.Name != "resource-set/"+customStampMasterPath {
			continue
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		raw, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if err = os.WriteFile(filepath.Join(staging, strings.TrimPrefix(f.Name, "resource-set/")), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	delivery, err := cpk.Load(filepath.Join(staging, "resources/cpk"), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range delivery {
		if file.Name == customStampVoiceCPK {
			found = true
			if file.Version != 2 {
				t.Fatal("edited sound failed to increment version")
			}
		}
	}
	if !found {
		t.Fatal("custom stamp CPK missing delivery")
	}
	if !strings.Contains(string(cpk.FileList(delivery)), customStampVoiceCPK+",CARD_VOICE,981,") {
		t.Fatal("stamp CPK not preloaded by native client")
	}
	if output := os.Getenv("CUSTOM_STAMP_TEST_ZIP"); output != "" {
		if err = os.WriteFile(output, patch, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
