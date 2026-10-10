package admin

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

func TestCustomCSVPreservesNativeQuotedFields(t *testing.T) {
	row := []string{"11", `"技能,名称"`, `"保留引号"`, "ATTACK"}
	out, err := rewriteCustomCSV("", map[string][][]string{"11": {row}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := customCSVRows([]byte(out))
	if err != nil || !reflect.DeepEqual(parsed, [][]string{row}) {
		t.Fatalf("client fields changed: %q, %v", out, err)
	}
	for _, text := range []string{"新增,描述", "新增\n描述", "新增\r描述", "新增\x00描述", `新增"描述`} {
		if _, err := rewriteCustomCSV("", map[string][][]string{"11": {{"11", text, "ATTACK"}}}); err == nil {
			t.Fatalf("accepted a field that corrupts client columns: %q", text)
		}
	}
}

func TestCustomEffectsKeepTemplateDirectionAfterEditing(t *testing.T) {
	s := customUnitSources()
	for i, role := range s.Roles {
		copy(role[1:8], []string{fmt.Sprintf("script2d_%d", i), "cutin", "playlist", "hit", "TARGET", "charge", "movie"})
	}
	// Continuation rows carry effects, while direction is owned by the first.
	continuation := append([]string(nil), s.Roles[0]...)
	for i := 1; i < 8; i++ {
		continuation[i] = ""
	}
	s.Roles = append(s.Roles, continuation)
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	c.Roles[0], c.Roles[2] = c.Roles[2], c.Roles[0]
	c.RoleSources[0], c.RoleSources[2] = c.RoleSources[2], c.RoleSources[0]
	c.Roles[0][20] = "777"
	for _, edit := range []string{"reorder", "remove", "foreign"} {
		t.Run(edit, func(t *testing.T) {
			edited := c
			edited.Roles = cloneCustomRows(c.Roles)
			if edit == "remove" {
				edited.Roles = edited.Roles[:2]
			}
			if edit == "foreign" {
				copy(edited.Roles[0][1:8], []string{"foreign2d", "foreigncutin", "foreignplaylist", "foreignhit", "SELF", "foreigncharge", "foreignmovie"})
			}
			_, _, roles, _, err := materializeCustomCard(edited, s)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(roles[0][1:8], s.Roles[0][1:8]) || !reflect.DeepEqual(roles[1][1:8], s.Roles[1][1:8]) || roles[0][20] != "777" {
				t.Fatalf("lost template direction or edited effect: %v", roles)
			}
			if edited.Roles[0][1] == "script2d_0" {
				t.Fatal("export mutated the draft")
			}
		})
	}
}

func TestCustomCardsRespectClientGroupCapacity(t *testing.T) {
	s := customUnitSources()
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	for i := 1; i < customClientSkillGroupLimit; i++ {
		c.Roles = append(c.Roles, append([]string(nil), c.Roles[0]...))
		c.RoleSources = append(c.RoleSources, customRoleSource{101, 0})
	}
	if err := a.validateCustomCards(&customCardDraft{[]customCard{c}}, s); err != nil {
		t.Fatalf("five effects per group must be accepted: %v", err)
	}
	c.Roles = append(c.Roles, append([]string(nil), c.Roles[0]...))
	c.RoleSources = append(c.RoleSources, customRoleSource{101, 0})
	if err := a.validateCustomCards(&customCardDraft{[]customCard{c}}, s); err == nil {
		t.Fatal("six effects overflow the client's fixed role array")
	}
	if _, _, _, _, err := materializeCustomCard(c, s); err == nil {
		t.Fatal("export accepted an overflowing group")
	}
	c.Roles = c.Roles[:2]
	for len(c.Skills) < 6 {
		c.Skills = append(c.Skills, append([]string(nil), c.Skills[0]...))
	}
	if err := validateCustomClientSkillCapacity(c); err != nil {
		t.Fatalf("five branches plus another skill must be accepted: %v", err)
	}
	c.Skills = append(c.Skills, append([]string(nil), c.Skills[0]...))
	if err := validateCustomClientSkillCapacity(c); err == nil {
		t.Fatal("six branches overflow the client's fixed extend array")
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
	bad = c
	bad.CardArtworkMode = "stretch"
	if a.validateCustomCards(&customCardDraft{[]customCard{bad}}, s) == nil {
		t.Fatal("accepted an invalid portrait composition mode")
	}
	bad = c
	bad.CardArtwork = []byte("invalid portrait image")
	if a.validateCustomCards(&customCardDraft{[]customCard{bad}}, s) == nil {
		t.Fatal("accepted a corrupt independent portrait")
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

func TestCustomArtworkContainerUpdatesOnlySelectedAssetPaths(t *testing.T) {
	const oldPict = 10000222
	oldPath := "assets/resources/05_image_assets/chr10/10/000/chr10_10000222.pvr"
	neighborPath := "assets/resources/05_image_assets/chr10/10/000/chr10_10000223.pvr"
	for _, tc := range []struct {
		id    int
		shard string
	}{{98000001, "98/000"}, {98012345, "98/012"}} {
		t.Run(strconv.Itoa(tc.id), func(t *testing.T) {
			newID := strconv.Itoa(tc.id)
			newPath := "assets/resources/05_image_assets/chr10/" + tc.shard + "/chr10_" + newID + ".pvr"
			if got := customArtworkResourcePath(oldPath, oldPict, tc.id); got != newPath {
				t.Fatalf("resource path=%q, want %q", got, newPath)
			}
			if got := customArtworkResourcePath("05_image_assets/chr10/10/000", oldPict, tc.id); got != "05_image_assets/chr10/"+tc.shard {
				t.Fatalf("resource directory=%q", got)
			}
			// Use length-prefixed, aligned strings as in an AssetBundle object.
			// A neighboring card in the same shard must retain its own path.
			var object bytes.Buffer
			for _, value := range []string{oldPath, neighborPath, "chr10_10000222"} {
				_ = binary.Write(&object, binary.LittleEndian, uint32(len(value)))
				object.WriteString(value)
				for object.Len()%4 != 0 {
					object.WriteByte(0)
				}
			}
			original := bytes.Clone(object.Bytes())
			next, err := rewriteCustomArtworkContainer(object.Bytes(), map[string]string{oldPath: newPath}, oldPict, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if len(next) != len(original) || !bytes.Equal(object.Bytes(), original) ||
				!bytes.Contains(next, []byte(newPath)) || !bytes.Contains(next, []byte(neighborPath)) ||
				!bytes.Contains(next, []byte("chr10_"+newID)) || bytes.Contains(next, []byte(oldPath)) {
				t.Fatalf("selected path, neighbor, texture name or object layout changed incorrectly: %q", next)
			}
		})
	}
}

type customArtworkTestAsset struct {
	Name          string `json:"name"`
	Directory     string `json:"directory"`
	ContainerPath string `json:"container_path"`
	Bundle        string `json:"bundle"`
}

func assertCustomArtworkExport(t *testing.T, c customCard, s customCardSources, source, generated []byte, files map[string][]byte, prefix string) map[string]customArtworkTestAsset {
	t.Helper()
	var original, exported struct {
		Assets []customArtworkTestAsset `json:"catalog_assets"`
	}
	if err := json.Unmarshal(source, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(generated, &exported); err != nil {
		t.Fatal(err)
	}
	oldPict := customRowInt(s.Cards[c.TemplateID], 36)
	oldID, newID := fmt.Sprintf("%08d", oldPict), fmt.Sprintf("%08d", c.ID)
	oldShard := fmt.Sprintf("/%02d/%03d", oldPict/1000000, oldPict/1000%1000)
	newShard := fmt.Sprintf("/%02d/%03d", c.ID/1000000, c.ID/1000%1000)
	sources := map[string]customArtworkTestAsset{}
	var art, icon, cardArt image.Image
	var err error
	if len(c.Artwork) > 0 {
		art, err = decodeCustomArtwork(c.Artwork)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(c.IconArtwork) > 0 {
		icon, err = decodeCustomArtwork(c.IconArtwork)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(c.CardArtwork) > 0 {
		cardArt, err = decodeCustomArtwork(c.CardArtwork)
		if err != nil {
			t.Fatal(err)
		}
	}
	if icon == nil && art == nil {
		icon = cardArt
	}
	for _, originalEntry := range original.Assets {
		if !strings.HasSuffix(originalEntry.Name, "_"+oldID) {
			continue
		}
		name := strings.ReplaceAll(originalEntry.Name, oldID, newID)
		sources[name] = originalEntry
		wantDirectory := strings.ReplaceAll(strings.ReplaceAll(originalEntry.Directory, oldShard, newShard), oldID, newID)
		wantContainer := strings.ReplaceAll(strings.ReplaceAll(originalEntry.ContainerPath, oldShard, newShard), oldID, newID)
		var entry customArtworkTestAsset
		count := 0
		for _, candidate := range exported.Assets {
			if candidate.Name != name {
				continue
			}
			count++
			entry = candidate
			if candidate.Directory != wantDirectory || candidate.ContainerPath != wantContainer {
				t.Fatalf("asset %s still uses template shard: directory=%q container=%q, want %q / %q", name, candidate.Directory, candidate.ContainerPath, wantDirectory, wantContainer)
			}
		}
		if count != 1 {
			t.Fatalf("asset %s has %d catalog entries, want one", name, count)
		}
		raw, exists := files[prefix+"resources/patch/"+entry.Bundle]
		if !exists {
			t.Fatalf("asset %s references an absent generated bundle %s", name, entry.Bundle)
		}
		bundle, err := readResourceBundle(raw)
		if err != nil || len(bundle.nodes) != 1 {
			t.Fatalf("read artwork bundle %s: %v", entry.Bundle, err)
		}
		foundContainer, foundTexture := false, false
		_, err = rewriteResourceObjects(bundle.nodes[0].data, func(class int, object []byte) ([]byte, error) {
			if class == 142 {
				foundContainer = foundContainer || bytes.Contains(object, []byte(wantContainer))
			}
			if class != 28 {
				return object, nil
			}
			cursor := resourceCursor{b: object, order: binary.LittleEndian}
			textureName := string(cursor.take(int(cursor.u32())))
			if textureName != name {
				return object, nil
			}
			foundTexture = true
			selected := art
			if strings.HasPrefix(name, "chr20_") && icon != nil {
				selected = icon
			}
			if strings.HasPrefix(name, "chr10_") && cardArt != nil {
				selected = cardArt
			}
			if selected == nil {
				root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
				originalTexture := readCustomArtworkTestTexture(t, root, originalEntry)
				copy(originalTexture[4:4+len(name)], name)
				if !bytes.Equal(object, originalTexture) {
					t.Fatalf("icon-only upload changed template illustration %s", name)
				}
				return object, nil
			}
			cursor.align(4)
			width, height := int(cursor.u32()), int(cursor.u32())
			cursor.u32()
			format, mipCount := cursor.u32(), cursor.u32()
			cursor.take(2)
			cursor.align(4)
			if cursor.u32() != 1 || cursor.u32() != 2 || format != 4 || mipCount != 1 {
				t.Fatalf("asset %s does not contain an inline RGBA32 texture", name)
			}
			cursor.take(24)
			pixels := cursor.take(int(cursor.u32()))
			fitted := fitCustomTextureArtwork(selected, name, width, height, c.CardArtworkMode)
			if len(pixels) != width*height*4 {
				t.Fatalf("asset %s has an invalid pixel payload", name)
			}
			for y := 0; y < height; y++ {
				if !bytes.Equal(pixels[y*width*4:(y+1)*width*4], fitted.Pix[(height-1-y)*fitted.Stride:(height-y)*fitted.Stride]) {
					t.Fatalf("asset %s did not receive the uploaded artwork", name)
				}
			}
			return object, nil
		})
		if err != nil || !foundContainer || !foundTexture {
			t.Fatalf("asset %s cannot resolve its bundle Container and Texture2D: container=%v texture=%v err=%v", name, foundContainer, foundTexture, err)
		}
	}
	if len(sources) == 0 {
		t.Fatal("template has no artwork assets to verify")
	}
	return sources
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
	attackDialogue, supportDialogue := "自制攻击台词！", "自制支援台词！"
	c.AttackDialogue, c.SupportDialogue = &attackDialogue, &supportDialogue
	c.Cost = 3
	c.Maximum.HP += 100
	c.Roles[0][20] = "999"
	// Add a second supported effect from an existing source to the normal branch.
	c.Roles = append(c.Roles, append([]string(nil), c.Roles[0]...))
	c.RoleSources = append(c.RoleSources, customRoleSource{10000010, 0})
	c.CutinTemplateID = 10000020
	c.ActionSources = []customCardActionSource{{FunctionID: customRowInt(c.Roles[0], 0), CardID: 10000020, SourceFunctionID: 11100101}}
	im := image.NewNRGBA(image.Rect(0, 0, 128, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 128; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	var art bytes.Buffer
	_ = png.Encode(&art, im)
	c.Artwork = art.Bytes()
	c.IconArtwork = art.Bytes()
	a.catalogByKey["6:10000010"] = AdminCatalogEntry{Name: "模板", ResourceState: "ready", ImageURL: "/assets/card/10000010.webp"}
	a.catalogByKey["6:10000020"] = AdminCatalogEntry{Name: "动作来源", ResourceState: "ready"}
	// Real resource sets can already contain published custom IDs. Preserve
	// those IDs in this synthetic draft, and exercise artwork re-export for
	// them too. The draft and ZIP remain disposable; no fixture is applied.
	testCards := []customCard{c}
	appliedIDs := make([]int, 0, len(s.Applied))
	for id := range s.Applied {
		appliedIDs = append(appliedIDs, id)
	}
	sort.Ints(appliedIDs)
	for _, id := range appliedIDs {
		templateID := s.Applied[id]
		applied, err := s.template(templateID)
		if err != nil {
			t.Fatal(err)
		}
		applied.ID, applied.Artwork = id, art.Bytes()
		applied.Attribute = "LIGHT"
		testCards = append(testCards, applied)
		a.catalogByKey[adminCatalogKey(6, templateID)] = AdminCatalogEntry{Name: applied.Name, ResourceState: "ready"}
	}
	router := chi.NewRouter()
	router.Post("/draft", a.saveCustomCards)
	router.Post("/export", a.exportCustomCards)
	body := map[string]any{"expected_revision": 0, "config": customCardDraft{testCards}}
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
	if e != nil || len(d.Cards) != len(testCards) || !bytes.Equal(d.Cards[0].Artwork, c.Artwork) || !bytes.Equal(d.Cards[0].IconArtwork, c.IconArtwork) {
		t.Fatalf("restart: %v", e)
	}
	if d.Cards[0].CutinTemplateID != c.CutinTemplateID || !reflect.DeepEqual(d.Cards[0].ActionSources, c.ActionSources) {
		t.Fatal("presentation selections lost after storage restart")
	}
	if !reflect.DeepEqual(d.Cards[0].AttackDialogue, c.AttackDialogue) || !reflect.DeepEqual(d.Cards[0].SupportDialogue, c.SupportDialogue) {
		t.Fatal("dialogue overrides lost after storage restart")
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
	cardRows, e := customCSVRows(files["resource-set/"+customCardCSVPath])
	if e != nil {
		t.Fatal(e)
	}
	generatedCards := map[int][]string{}
	for _, row := range cardRows {
		generatedCards[customRowInt(row, 0)] = row
	}
	if row := generatedCards[c.ID]; row[79] != attackDialogue || row[80] != supportDialogue || row[89] != s.Cards[c.TemplateID][89] || row[90] != s.Cards[c.TemplateID][90] {
		t.Fatal("exported dialogue text or inherited voice IDs mismatch")
	}
	sourceAssets, e := os.ReadFile(filepath.Join(root, "asset-map.json"))
	if e != nil {
		t.Fatal(e)
	}
	var sources map[string]customArtworkTestAsset
	for _, exportedCard := range testCards {
		row := generatedCards[exportedCard.ID]
		if len(row) == 0 || customRowInt(row, 36) != exportedCard.ID {
			t.Fatalf("uploaded artwork does not own card %d PictID", exportedCard.ID)
		}
		if exportedCard.CutinTemplateID != 0 && row[35] != s.Cards[exportedCard.CutinTemplateID][35] {
			t.Fatal("selected cut-in style missing from client card table")
		}
		cardSources := assertCustomArtworkExport(t, exportedCard, s, sourceAssets, files["resource-set/asset-map.json"], files, "resource-set/")
		if exportedCard.ID == c.ID {
			sources = cardSources
		}
	}
	// Simulate a previously exported map with the new texture names but the
	// old template directories. Re-export must repair, rather than duplicate,
	// those entries; the client builds its asset catalog directly from this map.
	var staleAssets, imageManifest map[string]any
	if e = json.Unmarshal(files["resource-set/asset-map.json"], &staleAssets); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(files["resource-set/resource-set.json"], &imageManifest); e != nil {
		t.Fatal(e)
	}
	oldID := fmt.Sprintf("%08d", customRowInt(s.Cards[c.TemplateID], 36))
	for _, v := range staleAssets["catalog_assets"].([]any) {
		entry := v.(map[string]any)
		if original, ok := sources[entry["name"].(string)]; ok {
			entry["directory"] = original.Directory
			entry["container_path"] = strings.ReplaceAll(original.ContainerPath, oldID, strconv.Itoa(c.ID))
		}
	}
	repairedFiles := map[string][]byte{}
	if e = buildCustomCardImages(c, generatedCards[c.ID], s, staleAssets, imageManifest, repairedFiles, map[string][]byte{}, root); e != nil {
		t.Fatal(e)
	}
	repairedAssets, e := json.Marshal(staleAssets)
	if e != nil {
		t.Fatal(e)
	}
	assertCustomArtworkExport(t, c, s, sourceAssets, repairedAssets, repairedFiles, "")
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
	generatedRoleRows, err := customCSVRows(files["resource-set/"+customRoleCSVPath])
	if err != nil {
		t.Fatal(err)
	}
	for _, exportedCard := range testCards {
		base, err := s.template(exportedCard.TemplateID)
		if err != nil {
			t.Fatal(err)
		}
		ids := customSkillIDs(exportedCard)
		seen := map[int]bool{}
		for _, originalRole := range base.Roles {
			fn := customRowInt(originalRole, 0)
			if seen[fn] {
				continue
			}
			seen[fn] = true
			wantDirection := originalRole[1:8]
			for _, source := range exportedCard.ActionSources {
				if source.FunctionID == fn {
					for _, sourceRole := range s.Roles {
						if customRowInt(sourceRole, 0) == source.SourceFunctionID {
							wantDirection = sourceRole[1:8]
							break
						}
					}
				}
			}
			found := false
			for _, exportedRole := range generatedRoleRows {
				if customRowInt(exportedRole, 0) == ids[fn] {
					found = true
					if !reflect.DeepEqual(exportedRole[1:8], wantDirection) {
						t.Fatalf("card %d lost selected direction for function %d", exportedCard.ID, fn)
					}
					break
				}
			}
			if !found {
				t.Fatalf("card %d missing function %d", exportedCard.ID, fn)
			}
		}
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
