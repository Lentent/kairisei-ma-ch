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
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
)

func TestCustomSkillRowsPreserveBranchesAndOtherLines(t *testing.T) {
	text := "#header\r\n11,old1\r\n7,\"historical\"bad,unchanged\n11,old2\r\n"
	out, e := rewriteCustomCSV(text, map[string][][]string{"11": {{"11", "first"}, {"11", "second"}}, "12": {{"12", "new"}}})
	if e != nil || strings.Count(out, "11,") != 2 || !strings.Contains(out, "7,\"historical\"bad,unchanged\n") || !strings.HasSuffix(out, "12,new\r\n") {
		t.Fatalf("rows: %s %v", out, e)
	}
}

func customUnitSources() customCardSources {
	row := make([]string, 62)
	row[0], row[4], row[5], row[9], row[26], row[27] = "101", "冠名", "模板", "3", "11", "12"
	skills := [][]string{}
	roles := [][]string{}
	for _, id := range []int{11, 12} {
		skill := make([]string, 51)
		skill[0], skill[1], skill[3], skill[11], skill[12], skill[14], skill[49] = strconv.Itoa(id), "技能", "造成伤害", "FIRE", "MERCENARY", "3", strconv.Itoa(id)
		skills = append(skills, skill)
		role := make([]string, 33)
		role[0], role[8], role[9], role[20] = strconv.Itoa(id), "ATTACK_AA", "SELECT", "100"
		roles = append(roles, role)
	}
	rules := make([]string, 10)
	rules[0] = "VALUE"
	return customCardSources{Master: masterdata.CardRuntimeMaster{CardTemplates: []gamestate.Card{{CardID: 101, Name: "模板", Level: 1, LevelMax: 50, Fame: 1, FameMax: 100, LoveMax: 10000, ParameterInitial: gamestate.CardParameter{HP: 100}, ParameterMaximum: gamestate.CardParameter{HP: 500}}}, DeckRankPolicy: gamestate.DeckRankPolicy{Cards: map[int]gamestate.CardRankRule{101: {ArthurType: 1}}}, CardProgressionPolicy: gamestate.CardProgressionPolicy{ConfigVersion: 1}}, Cards: map[int][]string{101: row}, Skills: skills, Roles: roles, Rules: map[string][]string{"ATTACK_AA": rules}, Applied: map[int]int{}}
}

func TestCustomCardsValidateCompositionAndIndependentIDs(t *testing.T) {
	s := customUnitSources()
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	c, e := s.template(101)
	if e != nil {
		t.Fatal(e)
	}
	c.ID = customCardFirstID
	c.Cost = 3
	c.Roles = append(c.Roles, append([]string(nil), c.Roles[0]...))
	c.RoleSources = append(c.RoleSources, customRoleSource{101, 0})
	c.Roles[2][20] = "999"
	d := customCardDraft{[]customCard{c}}
	if e = a.validateCustomCards(&d, s); e != nil {
		t.Fatal(e)
	}
	original := cloneCustomRows(s.Skills)
	row, skills, roles, card, e := materializeCustomCard(c, s)
	if e != nil {
		t.Fatal(e)
	}
	if row[26] == "11" || skills[0][49] == "11" || roles[0][0] == "11" || card.CardID != c.ID || roles[2][20] != "999" || !reflect.DeepEqual(original, s.Skills) {
		t.Fatal("shared or missing references")
	}
	c2 := c
	c2.ID++
	if customSkillIDs(c)[11] == customSkillIDs(c2)[11] {
		t.Fatal("custom cards share skills")
	}
	bad := c
	bad.Roles = cloneCustomRows(c.Roles)
	bad.Roles[0][8] = "UNKNOWN"
	if a.validateCustomCards(&customCardDraft{[]customCard{bad}}, s) == nil {
		t.Fatal("accepted arbitrary effect")
	}
	bad = c
	bad.Skills = cloneCustomRows(c.Skills)
	bad.Skills[0][49] = "123"
	if a.validateCustomCards(&customCardDraft{[]customCard{bad}}, s) == nil {
		t.Fatal("accepted altered function reference")
	}
	bad = c
	bad.Initial.HP = 1000
	if a.validateCustomCards(&customCardDraft{[]customCard{bad}}, s) == nil {
		t.Fatal("accepted initial above maximum")
	}
	s.Cards[c.ID] = row
	if a.validateCustomCards(&customCardDraft{[]customCard{c}}, s) == nil {
		t.Fatal("overwrote foreign card")
	}
	s.Applied[c.ID] = 101
	if a.validateCustomCards(&customCardDraft{[]customCard{}}, s) == nil {
		t.Fatal("removed published card")
	}
}

func TestCustomArtworkRGBAAndInvalidInput(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var pngBytes bytes.Buffer
	if e := png.Encode(&pngBytes, im); e != nil {
		t.Fatal(e)
	}
	decoded, e := decodeCustomArtwork(pngBytes.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	var raw bytes.Buffer
	_ = binary.Write(&raw, binary.LittleEndian, uint32(14))
	raw.WriteString("chr10_10000010")
	raw.Write(make([]byte, 2))
	for _, v := range []uint32{64, 64, 16, 33, 1} {
		_ = binary.Write(&raw, binary.LittleEndian, v)
	}
	raw.Write([]byte{1, 1, 0, 0})
	for _, v := range []uint32{1, 2, 1, 1, 0, 0, 0, 0, 16} {
		_ = binary.Write(&raw, binary.LittleEndian, v)
	}
	raw.Write(make([]byte, 16+12))
	result, e := rewriteCustomTexture(raw.Bytes(), "chr10_98000001", decoded)
	if e != nil {
		t.Fatal(e)
	}
	if binary.LittleEndian.Uint32(result[32:]) != 4 || binary.LittleEndian.Uint32(result[28:]) != 64*64*4 || !bytes.Contains(result, []byte("chr10_98000001")) {
		t.Fatal("invalid RGBA texture header")
	}
	if _, e = decodeCustomArtwork([]byte("not an image")); e == nil {
		t.Fatal("accepted corrupt artwork")
	}
}

func TestCustomEnlargedImagesAccumulateAndReplace(t *testing.T) {
	root := t.TempDir()
	path := "images/manifest.json"
	base := []byte(`{"schema_version":1,"files":[]}`)
	if e := os.MkdirAll(filepath.Join(root, "images"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, path), base, 0600); e != nil {
		t.Fatal(e)
	}
	manifest := map[string]any{"entrypoints": map[string]any{"cn-image-root": "images"}}
	files, original := map[string][]byte{}, map[string][]byte{}
	im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for _, id := range []int{98000001, 98000002, 98000001} {
		if e := addCustomEnlargedImage(id, im, manifest, files, original, root); e != nil {
			t.Fatal(e)
		}
	}
	var result struct {
		Files []struct {
			Path   string
			Bytes  int
			SHA256 string
		}
	}
	if e := json.Unmarshal(files[path], &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Files) != 2 || !bytes.Equal(original[path], base) {
		t.Fatal("lost a previously generated image or original manifest")
	}
	for _, row := range result.Files {
		data := files["images/"+row.Path]
		if len(data) != row.Bytes || resourceHash(data) != row.SHA256 {
			t.Fatal("incorrect enlarged image metadata")
		}
	}
}

func TestCustomCardsCompleteResourceExport(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires complete game resources")
	}
	a, accounts, _ := customGachaTestAPI(t)
	a.collectionResourceRoot = root
	s, e := a.customCardSources()
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.template(10000010)
	if e != nil {
		t.Fatal(e)
	}
	c.ID = customCardNextID(s, customCardDraft{})
	c.Name = "自制卡验收"
	c.Cost = 3
	c.Maximum.HP += 100
	c.Roles[0][20] = "999"
	// Add a second supported effect from an existing source to the normal branch.
	c.Roles = append(c.Roles, append([]string(nil), c.Roles[0]...))
	c.RoleSources = append(c.RoleSources, customRoleSource{10000010, 0})
	im := image.NewNRGBA(image.Rect(0, 0, 128, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 128; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	var art bytes.Buffer
	_ = png.Encode(&art, im)
	c.Artwork = art.Bytes()
	a.catalogByKey["6:10000010"] = AdminCatalogEntry{Name: "模板", ResourceState: "ready", ImageURL: "/assets/card/10000010.webp"}
	router := chi.NewRouter()
	router.Post("/draft", a.saveCustomCards)
	router.Post("/export", a.exportCustomCards)
	body := map[string]any{"expected_revision": 0, "config": customCardDraft{[]customCard{c}}}
	w := customGachaRequest(t, router, "/draft", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var saved struct {
		Revision int `json:"revision"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &saved)
	if w := customGachaRequest(t, router, "/draft", body); w.Code != 409 {
		t.Fatalf("stale revision %d", w.Code)
	}
	ops, e := NewOperations(accounts.Database(), nil)
	if e != nil {
		t.Fatal(e)
	}
	a.operations = ops
	d, rev, e := a.customCardsDraft(s)
	if e != nil || len(d.Cards) != 1 || !bytes.Equal(d.Cards[0].Artwork, c.Artwork) {
		t.Fatalf("restart: %v", e)
	}
	w = customGachaRequest(t, router, "/export", map[string]any{"expected_revision": rev})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	z, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	files := map[string][]byte{}
	for _, f := range z.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		files[f.Name] = b
		if strings.HasPrefix(f.Name, "resource-set/") {
			path := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(f.Name, "resource-set/")))
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, b, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	master, e := masterdata.LoadCardRuntimeMaster(filepath.Join(dir, customCardMasterPath))
	if e != nil {
		t.Fatal(e)
	}
	if master.CardProgressionConfigVersion != s.Master.CardProgressionConfigVersion+1 || len(master.CardTemplates) != len(s.Master.CardTemplates)+1 {
		t.Fatal("progression/catalog")
	}
	for _, f := range []string{customCardCSVPath, customSkillCSVPath, customRoleCSVPath} {
		if !bytes.Contains(files["resource-set/"+f], []byte(c.Name)) && f == customCardCSVPath {
			t.Fatal("missing card")
		}
	}
	// Overlay generated combat CSVs on a disposable copy of the original tables.
	battleDir := filepath.Join(dir, "_local/control/server/cn602-battle-master")
	entries, e := os.ReadDir(filepath.Join(root, "_local/control/server/cn602-battle-master"))
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range entries {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".csv") {
			continue
		}
		path := filepath.Join(battleDir, f.Name())
		if _, e = os.Stat(path); e == nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join(root, "_local/control/server/cn602-battle-master", f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	combat, e := multiplayer.LoadCombatCatalog(filepath.Join(dir, customCardCSVPath), battleDir)
	if e != nil {
		t.Fatal(e)
	}
	_, effects, e := combat.CardSkill(c.ID, 2)
	if e != nil || len(effects) != len(c.Roles)-1 {
		t.Fatalf("combat effects: %d %v", len(effects), e)
	}
	if combat.Cards[c.TemplateID].NormalSkillID == combat.Cards[c.ID].NormalSkillID {
		t.Fatal("template skill overwritten")
	}
	// The client and server must have identical rows for the new card/functions.
	client, e := readResourceBundle(files["resource-set/resources/patch/main_c/container.dat"])
	if e != nil {
		t.Fatal(e)
	}
	ids := customSkillIDs(c)
	paths := map[string]string{"card.csv": customCardCSVPath, "skill_player.csv": customSkillCSVPath, "skill_role_player.csv": customRoleCSVPath}
	updates := map[string]func(string) (string, error){}
	for name, path := range paths {
		updates[name] = func(text string) (string, error) {
			filter := func(raw []byte) [][]string {
				rows, e := customCSVRows(raw)
				if e != nil {
					t.Fatal(e)
				}
				out := [][]string{}
				for _, row := range rows {
					id := customRowInt(row, 0)
					match := id == c.ID
					for _, skillID := range ids {
						match = match || id == skillID
					}
					if match {
						out = append(out, row)
					}
				}
				return out
			}
			if !reflect.DeepEqual(filter([]byte(text)), filter(files["resource-set/"+path])) {
				t.Fatalf("client/server mismatch in %s", name)
			}
			return text, nil
		}
	}
	if _, e = replaceResourceTexts(client.nodes[0].data, updates); e != nil {
		t.Fatal(e)
	}
	if e = combat.ValidateRoleParameterContracts(); e != nil {
		t.Fatal(e)
	}
	if e = combat.ValidatePlayerFunctionCoverage(); e != nil {
		t.Fatal(e)
	}
	var manifest map[string]any
	_ = json.Unmarshal(files["resource-set/resource-set.json"], &manifest)
	for _, v := range manifest["files"].([]any) {
		r := v.(map[string]any)
		if b, ok := files["resource-set/"+r["path"].(string)]; ok {
			if r["sha256"] != resourceHash(b) || int(r["bytes"].(float64)) != len(b) {
				t.Fatal("invalid inventory digest")
			}
		}
	}
	if output := os.Getenv("CN602_CUSTOM_CARD_SAMPLE_ZIP"); output != "" {
		if e = os.WriteFile(output, w.Body.Bytes(), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
