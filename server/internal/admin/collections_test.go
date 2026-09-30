package admin

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

func TestResourceLZ4RoundTripAndMalformed(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("small"), bytes.Repeat([]byte("reward-material-honor"), 100000)} {
		decoded, e := resourceLZ4(packResourceLZ4(data), len(data))
		if e != nil || !bytes.Equal(decoded, data) {
			t.Fatalf("LZ4: %v", e)
		}
	}
	for _, b := range [][]byte{{0, 0, 0}, {0xff}, {0x10}, {0, 0, 1}} {
		if _, e := resourceLZ4(b, 12); e == nil {
			t.Fatalf("accepted malformed %x", b)
		}
	}
	bundle := resourceBundle{player: "5.x.x", engine: "5.3.5p1", nodes: []resourceNode{{"CAB-test", 0, bytes.Repeat([]byte("data"), 5000)}}}
	check, e := readResourceBundle(bundle.save())
	if e != nil || len(check.nodes) != 1 || !bytes.Equal(check.nodes[0].data, bundle.nodes[0].data) {
		t.Fatalf("bundle: %v", e)
	}
	if _, e = readResourceBundle([]byte("not a bundle")); e == nil {
		t.Fatal("accepted invalid bundle")
	}
}
func TestReplaceResourceTextPreservesOtherObject(t *testing.T) {
	var metadata, raw bytes.Buffer
	metadata.WriteString("5.3.5p1\x00")
	_ = binary.Write(&metadata, binary.LittleEndian, uint32(13))
	metadata.WriteByte(0)
	_ = binary.Write(&metadata, binary.LittleEndian, uint32(2))
	for _, class := range []uint32{49, 142} {
		_ = binary.Write(&metadata, binary.LittleEndian, class)
		metadata.Write(make([]byte, 16))
	}
	_ = binary.Write(&metadata, binary.LittleEndian, uint32(2))
	text := func(s string) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.LittleEndian, uint32(8))
		b.WriteString("item.csv")
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(s)))
		b.WriteString(s)
		for b.Len()%4 != 0 {
			b.WriteByte(0)
		}
		return b.Bytes()
	}
	objects := [][]byte{text("8887,old\n"), []byte("unchanged-reference-bytes")}
	tables := []int{}
	for i, b := range objects {
		for (20+metadata.Len())%4 != 0 {
			metadata.WriteByte(0)
		}
		_ = binary.Write(&metadata, binary.LittleEndian, uint64(i+1))
		tables = append(tables, 20+metadata.Len())
		for raw.Len()%8 != 0 {
			raw.WriteByte(0)
		}
		_ = binary.Write(&metadata, binary.LittleEndian, uint32(raw.Len()))
		_ = binary.Write(&metadata, binary.LittleEndian, uint32(len(b)))
		_ = binary.Write(&metadata, binary.LittleEndian, uint32([]int{49, 142}[i]))
		_ = binary.Write(&metadata, binary.LittleEndian, uint16([]int{49, 142}[i]))
		metadata.Write([]byte{0, 0, 0})
		raw.Write(b)
	}
	offset := 20 + metadata.Len()
	asset := make([]byte, 20)
	binary.BigEndian.PutUint32(asset, uint32(metadata.Len()))
	binary.BigEndian.PutUint32(asset[8:], 15)
	binary.BigEndian.PutUint32(asset[12:], uint32(offset))
	asset = append(asset, metadata.Bytes()...)
	asset = append(asset, raw.Bytes()...)
	binary.BigEndian.PutUint32(asset[4:], uint32(len(asset)))
	next, e := replaceResourceTexts(asset, map[string]func(string) (string, error){"item.csv": func(s string) (string, error) { return s + "9999,new gift box with longer text\n", nil }})
	if e != nil {
		t.Fatal(e)
	}
	table := tables[1]
	start := int(binary.LittleEndian.Uint32(next[table:]))
	size := int(binary.LittleEndian.Uint32(next[table+4:]))
	if !bytes.Equal(next[offset+start:offset+start+size], objects[1]) {
		t.Fatal("other object modified")
	}
	if _, e = replaceResourceTexts(asset, map[string]func(string) (string, error){"missing.csv": func(s string) (string, error) { return s, nil }}); e == nil {
		t.Fatal("accepted missing CSV")
	}
}
func TestCollectionDraftAndResourceExport(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("requires full runtime resource set")
	}
	a, accounts, _ := customGachaTestAPI(t)
	a.collectionResourceRoot = root
	masters := filepath.Join(root, "_local/control/server")
	items, e := masterdata.LoadItemRuntimeMaster(filepath.Join(masters, "cn602-item-runtime-master.json"))
	if e != nil {
		t.Fatal(e)
	}
	cards, e := masterdata.LoadCardRuntimeMaster(filepath.Join(masters, "cn602-card-runtime-master.json"))
	if e != nil {
		t.Fatal(e)
	}
	a.catalog, a.catalogByKey, e = BuildAdminCatalog(cards, items)
	if e != nil {
		t.Fatal(e)
	}
	a.catalogByKey["14:16"] = AdminCatalogEntry{Name: "妮妙", ResourceState: "ready"}
	d, revision, e := a.collectionDraft()
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Boxes) == 0 {
		t.Fatal("missing initial box")
	}
	b := d.Boxes[0]
	newItemID, newHonorID, newFunction := 90000001, 26093003, 70000001
	for _, item := range items.Items {
		newItemID = max(newItemID, item.ItemID+1)
		newFunction = max(newFunction, item.FunctionValue+1)
	}
	for _, h := range d.Honors {
		newHonorID = max(newHonorID, h.ID+1)
	}
	b.Item.ItemID = newItemID
	b.Item.Name = "测试新礼盒"
	b.Item.FunctionValue = newFunction
	b.Profile.ItemID = b.Item.ItemID
	b.Profile.FunctionValue = b.Item.FunctionValue
	b.Profile.RewardPool = []gamestate.WeightedReward{{Reward: gamestate.Reward{Type: 18, RewardTypeID: newHonorID, Num: 1, CardSkillLevels: []int16{}}, Weight: 1}}
	d.Honors = append(d.Honors, editableHonor{ID: newHonorID, Name: "测试新称号", SlotMask: 8})
	d.Boxes = append(d.Boxes, b)
	router := chi.NewRouter()
	router.Post("/draft", a.saveCollections)
	router.Post("/export", a.exportCollections)
	body := map[string]any{"expected_revision": revision, "config": d}
	w := customGachaRequest(t, router, "/draft", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Revision int `json:"revision"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if w = customGachaRequest(t, router, "/draft", body); w.Code != 409 {
		t.Fatalf("stale revision: %d", w.Code)
	}
	restarted, e := NewOperations(accounts.Database(), nil)
	if e != nil {
		t.Fatal(e)
	}
	a.operations = restarted
	saved, nextRevision, e := a.collectionDraft()
	if e != nil || len(saved.Boxes) != len(d.Boxes) || nextRevision != response.Revision {
		t.Fatalf("draft restart: %v", e)
	}
	invalid := saved
	invalid.Boxes = append([]editableBox(nil), saved.Boxes...)
	invalid.Boxes[len(invalid.Boxes)-1].Profile.RewardPool = []gamestate.WeightedReward{{Reward: gamestate.Reward{Type: 18, RewardTypeID: newHonorID, Num: 1, CardSkillLevels: []int16{}}, Weight: 0}}
	w = customGachaRequest(t, router, "/draft", map[string]any{"expected_revision": nextRevision, "config": invalid})
	if w.Code != 400 {
		t.Fatalf("invalid weight: %d %s", w.Code, w.Body.String())
	}
	w = customGachaRequest(t, router, "/export", map[string]any{"expected_revision": nextRevision})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	z, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	staged := t.TempDir()
	for _, file := range z.File {
		if strings.HasSuffix(file.Name, "cn602-honor-runtime-master.json") {
			r, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(staged, "honors.json"), content, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasSuffix(file.Name, "cn602-item-runtime-master.json") {
			r, e := file.Open()
			if e != nil {
				t.Fatal(e)
			}
			content, e := io.ReadAll(r)
			r.Close()
			if e != nil || !bytes.Contains(content, []byte("测试新礼盒")) {
				t.Fatal("missing item")
			}
			masterPath := filepath.Join(staged, "items.json")
			if e = os.WriteFile(masterPath, content, 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = masterdata.LoadItemRuntimeMaster(masterPath); e != nil {
				t.Fatalf("generated runtime master: %v", e)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing runtime item master")
	}
	// Reproduce the production boundary: generated masters must also be
	// accepted by account construction, not only by the JSON master loader.
	state, err := accountstore.LoadSaveState(filepath.Join(root, "server/config/cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	generatedItems, err := masterdata.LoadItemRuntimeMaster(filepath.Join(staged, "items.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyCardRuntimeMaster(&state, cards); err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyItemRuntimeMaster(&state, generatedItems); err != nil {
		t.Fatal(err)
	}
	avatars, err := masterdata.LoadAvatarRuntimeMaster(filepath.Join(masters, "cn602-avatar-runtime-master.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyAvatarRuntimeMaster(&state, avatars); err != nil {
		t.Fatal(err)
	}
	honors, err := masterdata.LoadHonorRuntimeMaster(filepath.Join(staged, "honors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyHonorRuntimeMaster(&state, honors); err != nil {
		t.Fatal(err)
	}
	login, err := masterdata.LoadLoginBonusRuntimeMaster(filepath.Join(root, "server/config/cn602-login-bonus-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyLoginBonusRuntimeMaster(&state, login); err != nil {
		t.Fatal(err)
	}
	progression, err := masterdata.LoadPlayerProgressionRuntimeMaster(filepath.Join(root, "server/config/cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyPlayerProgressionRuntimeMaster(&state, progression); err != nil {
		t.Fatal(err)
	}
	state.Items = append(state.Items, gamestate.Item{ItemID: newItemID, Num: 1})
	account, err := game.New(state)
	if err != nil {
		t.Fatalf("generated master account initialization: %v", err)
	}
	if _, err = account.PlayItemGacha(newItemID, 1); err != nil {
		t.Fatalf("generated box opening: %v", err)
	}
	if target := os.Getenv("CN602_COLLECTION_EXPORT"); target != "" {
		if e = os.WriteFile(target, w.Body.Bytes(), 0600); e != nil {
			t.Fatal(e)
		}
	}
	request := httptest.NewRequest("POST", "http://localhost/export", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	a.exportCollections(recorder, request)
	if recorder.Code != 403 {
		t.Fatal("missing mutation authorization accepted")
	}
}

func TestResourceCSVPreservesOfficialQuoting(t *testing.T) {
	original := "#header\r\n6015,old with unescaped \"quotes\"\r\n9999,old box\r\n"
	next, e := rewriteResourceCSV(original, map[string][]string{"9999": {"9999", "updated box"}, "90000001": {"90000001", "new box"}})
	if e != nil || !strings.Contains(next, "6015,old with unescaped \"quotes\"\r\n") || !strings.Contains(next, "90000001,new box\r\n") {
		t.Fatalf("old row changed: %q %v", next, e)
	}
}
