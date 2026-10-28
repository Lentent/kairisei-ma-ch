package admin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
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
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func bossTestArtwork(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 64, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 64; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCustomBossAppearanceSaveRestartAndProjection(t *testing.T) {
	base, path := customBossFixture(t)
	base.TeamBattleSolo = json.RawMessage(strings.ReplaceAll(string(base.TeamBattleSolo), `"14":1`, `"14":0`))
	enemyPath := filepath.Join(filepath.Dir(path), "cn602-battle-master/enemy.csv")
	raw, err := os.ReadFile(enemyPath)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := customBossCSVRows(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	updates := map[string][]string{}
	for id, row := range rows {
		row[4] = "10002013"
		updates[strconv.Itoa(id)] = row
	}
	text, err := rewriteResourceCSV(string(raw), updates)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(enemyPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	router := chi.NewRouter()
	router.Post("/copies", a.createCustomBoss)
	router.Put("/copies", a.saveCustomBoss)
	router.Post("/export", a.exportCustomBossAppearance)
	var result struct {
		Boss    customBoss `json:"boss"`
		Current bool       `json:"appearance_current"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 200), &result); err != nil {
		t.Fatal(err)
	}
	b := result.Boss
	if !result.Current || b.Targets[0].StaticImageID == 0 {
		t.Fatal("2D template did not expose editable artwork")
	}
	b.Targets[0].Name = "机械核心"
	b.Name = "机械试炼"
	b.Targets[0].Artwork = bossTestArtwork(t)
	b.Targets[2].Name = "第二波核心"
	b.Targets[2].Artwork = bossTestArtwork(t)
	b.CoverArtwork = bossTestArtwork(t)
	b.ArtworkLayout = "separate"
	input := map[string]any{"expected_revision": 1, "boss_id": b.BossID, "name": b.Name, "difficulty": b.Difficulty, "bp_use": b.BPUse, "bp_use_half": b.BPUseHalf, "continue": b.Continue, "enabled": b.Enabled, "targets": b.Targets, "cover_artwork": b.CoverArtwork, "artwork_layout": b.ArtworkLayout}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 200), &result); err != nil {
		t.Fatal(err)
	}
	if result.Current {
		t.Fatal("saving a design claimed that client resources were applied")
	}
	testfixture.CallContentAdmin(t, router, "POST", "/export", map[string]any{"expected_revision": 1, "boss_id": b.BossID}, 409)
	if ops.ContentConfiguration().State.TeamBattleReplays[1].Battles[0].ClientEnemyPartyID != 0 {
		t.Fatal("unpublished client identity became active")
	}
	for _, invalid := range []string{"", "bad,name", "bad\nname", "<b>"} {
		bad := cloneBossTargets(b.Targets)
		bad[0].Name = invalid
		input["targets"] = bad
		input["expected_revision"] = 2
		testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 400)
	}
	bad := cloneBossTargets(b.Targets)
	bad[0].Artwork = []byte("fake png")
	input["targets"] = bad
	testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 400)
	receipt := map[int]customBossAppearance{b.BossID: {SourceBossID: 11, Fingerprint: customBossAppearanceHash(b), Parties: []int{customBossPartyID(b.BossID, 0), customBossPartyID(b.BossID, 1)}, CoverPictureID: customBossCoverID(b.BossID)}}
	raw, _ = json.Marshal(receipt)
	if err = os.WriteFile(filepath.Join(filepath.Dir(path), "cn602-custom-boss-appearances.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	saved := restarted.content.customBosses[0]
	if !bytes.Equal(saved.Targets[0].Artwork, b.Targets[0].Artwork) || saved.Targets[0].Name != "机械核心" || !restarted.content.customBossAppearanceCurrent(saved) {
		t.Fatal("design or applied receipt lost on restart")
	}
	if !bytes.Equal(saved.CoverArtwork, b.CoverArtwork) || saved.ArtworkLayout != "separate" {
		t.Fatal("cover and layout were lost on restart")
	}
	config := restarted.ContentConfiguration().State
	assertSelection := func(raw json.RawMessage) {
		t.Helper()
		var top map[string][]map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatal(err)
		}
		if len(top["9"]) != 0 || len(top["12"]) != 1 {
			t.Fatal("2D copy did not use enemy-art selection catalog")
		}
		g := top["12"][0]
		var name string
		var picture, search int
		var bosses []map[string]json.RawMessage
		_ = json.Unmarshal(g["4"], &name)
		_ = json.Unmarshal(g["7"], &picture)
		_ = json.Unmarshal(g["2"], &search)
		_ = json.Unmarshal(g["10"], &bosses)
		var bossPicture int
		_ = json.Unmarshal(bosses[0]["9"], &bossPicture)
		if name != b.Name || picture != customBossCoverID(b.BossID) || bossPicture != picture || search != b.GroupID {
			t.Fatal("selection name, cover or independent search identity is stale")
		}
	}
	assertSelection(config.TeamBattleSolo)
	account := game.ApplyContentState(gamestate.State{TeamBattleSolo: base.TeamBattleSolo}, restarted.ContentConfiguration())
	assertSelection(account.TeamBattleSolo)
	for wave, w := range config.TeamBattleReplays[1].Battles {
		if w.EnemyPartyID != 101 || w.ClientEnemyPartyID != customBossPartyID(b.BossID, wave) {
			t.Fatal("client appearance changed server combat party")
		}
	}
	if config.TeamBattleReplays[0].Battles[0].ClientEnemyPartyID != 0 || config.TeamBattleReplays[1].EnemyOverrides[0].Stats.EnemyID != 1 || restarted.content.bosses[11].Targets[0].Name != "部位1" {
		t.Fatal("copy modified template or combat identity")
	}
	if saved.Targets[2].Name != "第二波核心" {
		t.Fatal("wave names were shared")
	}
	saved.Targets[0].Stats.HP++
	if !restarted.content.customBossAppearanceCurrent(saved) {
		t.Fatal("stat changes incorrectly require new art")
	}
	saved.Targets[0].Name = "改名待应用"
	if restarted.content.customBossAppearanceCurrent(saved) {
		t.Fatal("unapplied rename marked current")
	}
	a.operations = restarted
	input["targets"] = b.Targets
	input["cover_artwork"] = ""
	input["artwork_layout"] = "template"
	testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 200)
	if len(restarted.content.customBosses[0].CoverArtwork) != 0 || restarted.content.customBosses[0].ArtworkLayout != "template" {
		t.Fatal("cover reset/layout switch did not persist")
	}
}

func TestCustomBossArtworkFitsTextureCanvas(t *testing.T) {
	im, err := decodeCustomBossArtwork(bossTestArtwork(t))
	if err != nil {
		t.Fatal(err)
	}
	fit := fitCustomTextureArtwork(im, "chr50_98000000", 128, 128, "texture")
	if fit.NRGBAAt(20, 64).A != 0 || fit.NRGBAAt(32, 64).A != 255 || fit.NRGBAAt(95, 64).A != 255 || fit.NRGBAAt(96, 64).A != 0 {
		t.Fatal("2D enemy PNG used card display proportions")
	}
}

func TestCustomBossAppearanceRuntimeExport(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("requires full runtime resource set")
	}
	path := filepath.Join(root, "_local/control/server/cn602-battle-runtime-master.json")
	master, err := masterdata.LoadBattleRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{TeamBattleSolo: json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)}
	if err = masterdata.ApplyBattleRuntimeMaster(&base, master); err != nil {
		t.Fatal(err)
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	sourceEnemyCSV, err := os.ReadFile(filepath.Join(filepath.Dir(path), "cn602-battle-master/enemy.csv"))
	if err != nil {
		t.Fatal(err)
	}
	sourceEnemyRows, err := customBossCSVRows(string(sourceEnemyCSV))
	if err != nil {
		t.Fatal(err)
	}
	sourceID := 0
	for id := range ops.content.bosses {
		s, e := ops.content.customBossSource(id)
		firstCount, allStatic, sizes := 0, true, ""
		for _, t := range s.Targets {
			if t.BattleIndex == 0 {
				firstCount++
				allStatic = allStatic && t.StaticImageID > 0
				if t.Stats == nil {
					allStatic = false
					continue
				}
				if row := sourceEnemyRows[t.Stats.EnemyID]; len(row) > 24 {
					sizes += row[24]
				}
			}
		}
		if e == nil && firstCount == 3 && allStatic && sizes == "LPP" && (sourceID == 0 || id < sourceID) {
			sourceID = id
		}
	}
	if sourceID == 0 {
		t.Fatal("no static 2D template")
	}
	a := &API{operations: ops, collectionResourceRoot: root}
	router := chi.NewRouter()
	router.Post("/copies", a.createCustomBoss)
	var created struct {
		Boss customBoss `json:"boss"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": sourceID}, 200), &created); err != nil {
		t.Fatal(err)
	}
	b := created.Boss
	// Repeat the same template as a second wave: its parts must get independent identities.
	w := customBossWaves(b)[0]
	b.Replay.Battles = []gamestate.TeamBattleReplayBattle{w, w}
	first := []dropTarget{}
	for _, target := range b.Targets {
		if target.BattleIndex == 0 {
			first = append(first, target)
		}
	}
	b.Targets = first
	b.Targets = append(cloneBossTargets(b.Targets), cloneBossTargets(b.Targets)...)
	for i := len(b.Targets) / 2; i < len(b.Targets); i++ {
		b.Targets[i].BattleIndex = 1
	}
	b.Targets[0].Name = "自制核心"
	b.Targets[0].Artwork = bossTestArtwork(t)
	b.Targets[len(b.Targets)/2].Name = "二波核心"
	b.Targets[len(b.Targets)/2].Artwork = bossTestArtwork(t)
	patch, err := a.buildCustomBossAppearancePatch(b)
	if err != nil {
		t.Fatal(err)
	}
	readZIP := func(raw []byte) map[string][]byte {
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil {
			t.Fatal(e)
		}
		out := map[string][]byte{}
		for _, f := range z.File {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			out[f.Name], e = io.ReadAll(r)
			r.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	files := readZIP(patch)
	readTexts := func(raw []byte) map[string]string {
		bundle, e := readResourceBundle(raw)
		if e != nil {
			t.Fatal(e)
		}
		out := map[string]string{}
		updates := map[string]func(string) (string, error){}
		for _, name := range []string{"enemy.csv", "enemy_party.csv", "enemy_lvup.csv", "enemy_target.csv", "enemy_uipos.csv"} {
			updates[name] = func(s string) (string, error) { out[name] = s; return s, nil }
		}
		if _, e = replaceResourceTexts(bundle.nodes[0].data, updates); e != nil {
			t.Fatal(e)
		}
		return out
	}
	containerPath := "resources/patch/main_c/container.dat"
	original, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(containerPath)))
	if err != nil {
		t.Fatal(err)
	}
	before, after := readTexts(original), readTexts(files["resource-set/"+containerPath])
	for name, text := range before {
		old, e := customBossCSVRows(text, name == "enemy_target.csv")
		if e != nil {
			t.Fatal(e)
		}
		next, e := customBossCSVRows(after[name], name == "enemy_target.csv")
		if e != nil {
			t.Fatal(e)
		}
		for id, row := range old {
			if !reflect.DeepEqual(row, next[id]) {
				t.Fatalf("original %s row %d changed", name, id)
			}
		}
	}
	enemies, _ := customBossCSVRows(after["enemy.csv"])
	parties, _ := customBossCSVRows(after["enemy_party.csv"])
	targets, _ := customBossCSVRows(after["enemy_target.csv"], true)
	separatedPositions := false
	for _, line := range strings.Split(after["enemy_uipos.csv"], "\n") {
		if strings.HasPrefix(line, "SSS,") {
			r := strings.Split(line, ",")
			separatedPositions = len(r) > 21 && r[1] != r[11] && r[11] != r[21] && r[1] != r[21]
		}
	}
	if !separatedPositions {
		t.Fatal("separate 2D layout has no distinct client coordinates")
	}
	for wave := 0; wave < 2; wave++ {
		id := customBossEnemyID(b.BossID, wave, 0)
		pict := customBossImageID(b.BossID, wave, 0)
		if enemies[id][4] != strconv.Itoa(pict) || parties[customBossPartyID(b.BossID, wave)][2] != strconv.Itoa(id) {
			t.Fatal("client party/image identity was not cloned")
		}
		name := fmtBossTexturePath(b.BossID, wave)
		bundle, e := readResourceBundle(files["resource-set/"+name])
		if e != nil {
			t.Fatal(e)
		}
		found := false
		_, e = rewriteResourceObjects(bundle.nodes[0].data, func(class int, object []byte) ([]byte, error) {
			if class == 28 && resourceObjectName(object) == "chr50_"+strconv.Itoa(pict) {
				found = true
			}
			return object, nil
		})
		if e != nil || !found {
			t.Fatal("new texture not loadable", e)
		}
		for slot := 0; slot < 3; slot++ {
			id := customBossEnemyID(b.BossID, wave, slot)
			if enemies[id][24] != "S" || parties[customBossPartyID(b.BossID, wave)][4+slot*3] != strconv.Itoa(slot) {
				t.Fatal("independent 2D sprites still use overlapping template layout/parent anchors")
			}
			for j := 1; j < 5; j++ {
				if targets[id][j] != "0" {
					t.Fatal("separated target does not anchor to its own image")
				}
			}
		}
	}
	coverPath := fmt.Sprintf("resource-set/resources/patch/main_c/image/custom_boss_%d_cover.dat", b.BossID)
	cover, e := readResourceBundle(files[coverPath])
	if e != nil {
		t.Fatal(e)
	}
	coverFound := false
	_, e = rewriteResourceObjects(cover.nodes[0].data, func(class int, object []byte) ([]byte, error) {
		if class == 28 && resourceObjectName(object) == fmt.Sprintf("chr60_%08d", customBossCoverID(b.BossID)) {
			coverFound = true
		}
		return object, nil
	})
	if e != nil || !coverFound {
		t.Fatal("selection cover lacks the client chr60 resource", e)
	}
	if enemies[customBossEnemyID(b.BossID, 0, 0)][6] != "自制核心" || enemies[customBossEnemyID(b.BossID, 1, 0)][6] != "二波核心" {
		t.Fatal("per-wave names not exported")
	}
	staging := t.TempDir()
	for name, raw := range files {
		if !strings.HasPrefix(name, "resource-set/") {
			continue
		}
		target := filepath.Join(staging, strings.TrimPrefix(name, "resource-set/"))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var assets map[string]any
	if err = json.Unmarshal(files["resource-set/asset-map.json"], &assets); err != nil {
		t.Fatal(err)
	}
	// Re-export from the applied set. Only the original texture is read as a template.
	coverTemplate, err := customBossCoverTemplate(b, assets["catalog_assets"].([]any))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range assets["catalog_assets"].([]any) {
		entry := v.(map[string]any)
		if entry["name"] == "chr50_"+strconv.Itoa(b.Targets[0].StaticImageID) || entry["bundle"] == coverTemplate["bundle"] {
			name := "resources/patch/" + entry["bundle"].(string)
			raw, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
			if e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(staging, filepath.FromSlash(name))
			if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(target, raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	b.Targets[0].Name = "再次改名"
	b.ArtworkLayout = "template"
	a2 := &API{collectionResourceRoot: staging}
	second, err := a2.buildCustomBossAppearancePatch(b)
	if err != nil {
		t.Fatal(err)
	}
	secondFiles := readZIP(second)
	secondEnemies, _ := customBossCSVRows(readTexts(secondFiles["resource-set/"+containerPath])["enemy.csv"])
	if secondEnemies[customBossEnemyID(b.BossID, 0, 0)][6] != "再次改名" {
		t.Fatal("applied design could not be updated")
	}
	secondTexts := readTexts(secondFiles["resource-set/"+containerPath])
	secondTargets, _ := customBossCSVRows(secondTexts["enemy_target.csv"], true)
	originalTargets, _ := customBossCSVRows(before["enemy_target.csv"], true)
	for _, target := range b.Targets {
		id := customBossEnemyID(b.BossID, target.BattleIndex, target.EnemyIndex)
		if expected := originalTargets[target.Stats.EnemyID]; len(expected) > 0 && !reflect.DeepEqual(expected[1:], secondTargets[id][1:]) {
			t.Fatal("template layout lost its original aim/effect coordinates")
		}
	}
	if output := os.Getenv("CUSTOM_BOSS_TEST_ZIP"); output != "" {
		if err = os.WriteFile(output, patch, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("exported static 2D template %d, %d parts across two waves, %d ZIP bytes", sourceID, len(b.Targets), len(patch))
}

func fmtBossTexturePath(boss, wave int) string {
	return "resources/patch/main_c/image/custom_boss_" + strconv.Itoa(boss) + "_" + strconv.Itoa(wave) + "_0.dat"
}
