package cnbootstrap

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func newTestHandler(t *testing.T, root string, logPath string) http.Handler {
	t.Helper()
	savePath := writeTestSave(t, root)
	// These transport fixtures model an established QA account. Seed SQLite
	// explicitly; JSON is configuration, no longer an account-import path.
	storage, err := newCNSaveDatabase(savePath, savePath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.loadOrImport(); err != nil {
		t.Fatal(err)
	}
	fixture, err := loadCNSaveState(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.persist(fixture); err != nil {
		t.Fatal(err)
	}
	cpkRoot := filepath.Join(root, "CPK")
	if err := os.Mkdir(cpkRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpkRoot, "alpha.cpk"), []byte("cpk!"), 0o600); err != nil {
		t.Fatal(err)
	}
	patchRoot := filepath.Join(root, "patch")
	menuPath := filepath.Join(patchRoot, "main_c", "scenes", "scene_Menu.dat")
	if err := os.MkdirAll(filepath.Dir(menuPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(menuPath, []byte("menu"), 0o600); err != nil {
		t.Fatal(err)
	}
	version := []byte("<version>,790\n<bundle_ver>,main_c/scenes/scene_Menu.dat,0,93936638\n")
	for index := range version {
		version[index] += cn602ScrambleKey[index%len(cn602ScrambleKey)]
	}
	if err := os.WriteFile(filepath.Join(patchRoot, "version.dat"), version, 0o600); err != nil {
		t.Fatal(err)
	}
	assetMapPath := writeTestAssetMap(t, root, version)
	if err := os.WriteFile(filepath.Join(root, cn602HomeEventBannerFile), []byte("event-png"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range cn602GachaBannerFiles {
		if err := os.WriteFile(filepath.Join(root, fileName), []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cardMasterPath := writeTestCardMaster(t, root)
	handler, err := New(
		logPath,
		savePath,
		savePath,
		assetMapPath,
		cardMasterPath,
		writeTestExploreMaster(t, root),
		writeTestStoryMaster(t, root),
		"",
		writeTestNaviMaster(t, root),
		writeTestItemMaster(t, root),
		writeTestAvatarMaster(t, root),
		assetMapPath,
		assetMapPath,
		assetMapPath,
		writeTestStampMaster(t, root),
		writeTestHonorMaster(t, root),
		filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"),
		filepath.Join("..", "..", "config", "cn602-login-bonus-runtime.json"),
		"10.0.2.2",
		18081,
		cpkRoot,
		writeTestCPKAliases(t, root),
		[]string{patchRoot},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"http://local/loginSDK.php",
		strings.NewReader(`{"uuid":"00000000-0000-0000-0000-000000000001","clver":"6.0.3"}`),
	)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("initialize test session: status=%d body=%q", loginResponse.Code, loginResponse.Body.String())
	}
	var loginPayload struct {
		SessionKey string `json:"sess_key"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginPayload); err != nil || loginPayload.SessionKey == "" {
		t.Fatalf("decode initialized test session: payload=%+v error=%v", loginPayload, err)
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatalf("reset request capture after test login: %v", err)
	}
	return testSessionHandler{Handler: handler, sessionKey: loginPayload.SessionKey}
}

type testSessionHandler struct {
	http.Handler
	sessionKey string
}

func (handler testSessionHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		legacy := []byte(cn602LocalSession)
		if bytes.HasPrefix(body, legacy) {
			body = append([]byte(handler.sessionKey), body[len(legacy):]...)
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	handler.Handler.ServeHTTP(writer, request)
}

func writeTestAssetMap(t *testing.T, root string, version []byte, catalogAssets ...map[string]any) string {
	t.Helper()
	if len(catalogAssets) == 0 {
		catalogAssets = []map[string]any{
			testAsset("scenes", "Menu", ".prefab", cn602MenuBundle),
		}
	}
	bundleNames := make([]string, 0)
	seenBundles := make(map[string]struct{})
	for _, asset := range catalogAssets {
		bundleName := asset["bundle"].(string)
		if _, exists := seenBundles[bundleName]; exists {
			continue
		}
		seenBundles[bundleName] = struct{}{}
		bundleNames = append(bundleNames, bundleName)
	}
	bundles := make([]map[string]any, len(bundleNames))
	for index, bundleName := range bundleNames {
		bundles[index] = map[string]any{
			"bundle":       bundleName,
			"cab_name":     fmt.Sprintf("cab-test-%d", index),
			"scrambled":    true,
			"dependencies": []string{},
		}
	}
	digest := sha256.Sum256(version)
	manifest := map[string]any{
		"schema_version": 2,
		"client_profile": "cn602-bootstrap",
		"source": map[string]any{
			"catalog_version":                    cn602CatalogVersion,
			"version_dat_sha256":                 fmt.Sprintf("%x", digest),
			"parsed_unity_bundle_count":          len(bundles),
			"non_unity_bundle_count":             0,
			"bundle_dependency_edge_count":       0,
			"unresolved_bundle_dependency_count": 0,
			"scrambled_bundle_count":             len(bundles),
			"plain_bundle_count":                 0,
			"surviving_official_catalog_overlay": testSurvivingOfficialCatalogOverlay(),
		},
		"bundles":        bundles,
		"catalog_assets": catalogAssets,
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	assetMapPath := filepath.Join(root, "cn602-asset-map.json")
	if err := os.WriteFile(assetMapPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return assetMapPath
}

func testSurvivingOfficialCatalogOverlay() map[string]any {
	return map[string]any{
		"path":                                "_local/extracted/cn-official-cdn/ma43-update-2018-final/catalog.dat",
		"bytes":                               cn602SurvivingCatalogBytes,
		"sha256":                              cn602SurvivingCatalogSHA256,
		"catalog_version":                     cn602SurvivingCatalogVersion,
		"scope":                               []string{"eelbinary"},
		"candidate_rows_in_available_bundles": cn602SurvivingCatalogLogicalRows,
		"physically_verified_rows":            cn602SurvivingCatalogLogicalRows,
		"physically_absent_rows":              0,
		"added_rows":                          cn602SurvivingCatalogLogicalRows,
		"verification":                        "same logical row, bundle owner and physical container path",
	}
}

func testAsset(directory string, name string, extension string, bundle string) map[string]any {
	return map[string]any{
		"directory": directory,
		"name":      name,
		"extension": extension,
		"bundle":    bundle,
	}
}

func writeTestCardMaster(t *testing.T, root string) string {
	t.Helper()
	cardTemplate := func(cardID int, name string) map[string]any {
		return map[string]any{
			"unique_id": 0, "card_id": cardID, "name": name,
			"same_card_id": cardID, "same_support_card_id": cardID,
			"rarity_rank": 1, "skill_level_max": 1,
			"level": 1, "level_max": 60, "experience_table_id": 1,
			"experience": 0, "now_level_experience": 0, "love": 0,
			"love_max": 10000, "premium_rarity": false,
			"skill_levels": []int{1}, "hp": 103, "attack": 12,
			"magic": 2, "mind": 1, "next_level_experience": 10,
			"parameter_initial":            map[string]any{"hp": 100, "attack": 10, "magic": 0, "mind": 0},
			"parameter_maximum":            map[string]any{"hp": 200, "attack": 20, "magic": 0, "mind": 0},
			"parameter_love_maximum_bonus": map[string]any{"hp": 0, "attack": 0, "magic": 0, "mind": 0},
			"add_experience":               10, "base_add_price": 100, "sell_gold": 500, "fame": 1,
			"fame_max": 100, "development_type": 1, "decompose_radix": 20,
			"develop_radix": 0, "acquisition_text": "Test source",
		}
	}
	stackTemplate := func(cardID int) map[string]any {
		return map[string]any{
			"cardid": cardID, "num": 0, "hp": 5, "atkp": 5,
			"intp": 5, "mndp": 5, "add_exp": 10, "base_add_price": 500,
		}
	}
	buddyStackTemplate := func(cardID int, materialType int, addExperience int) map[string]any {
		return map[string]any{
			"cardid": cardID, "num": 0, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0,
			"add_exp": addExperience, "base_add_price": 100, "material_type": materialType,
		}
	}
	sphereDefinition := func(sphereID int) map[string]any {
		return map[string]any{
			"sphrid": sphereID, "same_sphrid": sphereID, "name": "Test sphere",
			"text": "", "type": "NORMAL", "rarity": "NORMAL", "evo_count": 0,
			"max_level": 1, "exp_table_id": 0, "equip_allowed": []bool{true, true, true, true},
			"skill_id": sphereID, "passive_skill_id": 0, "call_skill_id": 0,
			"pict_id": sphereID, "count": 1, "play_condition": "TURN_AFTER",
			"play_condition_param": 1, "way_to_use_text": "", "sell_gold": 100,
			"evolution_id": 0, "material_add_experience": 10, "fusion_base_add_price": 10,
		}
	}
	sphereSeed := func(uniqueID int64, sphereID int) map[string]any {
		return map[string]any{
			"unique_id": uniqueID, "sphere_id": sphereID, "level": 1, "experience": 0,
			"next_level_experience": 0, "now_level_experience": 0,
			"add_experience": 10, "base_add_price": 10, "is_lock": 0, "create_time": 0,
		}
	}
	buddyDefinition := map[string]any{
		"buddyid": 1000010, "same_buddyid": 1000010, "name": "Test buddy", "rarity": "LEGEND",
		"evolution_count": 0, "max_level": 1, "pict_id": 10000010, "experience_table_id": 401,
		"sell_gold": 100000, "evolution_id": 0, "overlimit_item_id": 6191, "overlimit_item_num": 1,
		"material_add_experience": 100, "fusion_base_add_price": 500,
	}
	buddySeed := map[string]any{
		"unique_id": 1, "buddy_id": 1000010, "level": 1, "experience": 0,
		"next_level_experience": 0, "now_level_experience": 0,
		"add_experience": 100, "base_add_price": 500, "is_lock": 0, "create_time": 0,
	}
	cardTemplates := []map[string]any{
		cardTemplate(10000010, "Test source"),
		cardTemplate(10000011, "Test target"),
		cardTemplate(10000018, "Test friend gacha two"),
		cardTemplate(10000022, "Test friend gacha three"),
		cardTemplate(10000026, "Test friend gacha four"),
		cardTemplate(10000030, "Test friend gacha five"),
		cardTemplate(10000034, "Test friend gacha six"),
		cardTemplate(10000038, "Test friend gacha seven"),
		cardTemplate(10000050, "Test friend gacha eight"),
		cardTemplate(10000054, "Test friend gacha nine"),
		cardTemplate(10000061, "Test friend gacha ten"),
		cardTemplate(10000123, "Test friend gacha eleven"),
		cardTemplate(10000304, "Test friend gacha twelve"),
		cardTemplate(10105002, "Test gacha one"),
		cardTemplate(10105009, "Test gacha two"),
		cardTemplate(10105012, "Test gacha three"),
		cardTemplate(10105016, "Test gacha four"),
	}
	for index := 1; index <= 49; index++ {
		cardTemplates = append(cardTemplates, cardTemplate(11000000+index, fmt.Sprintf("Test trade card %d", index)))
	}
	seenCardIDs := make(map[int]struct{}, len(cardTemplates))
	for _, template := range cardTemplates {
		seenCardIDs[template["card_id"].(int)] = struct{}{}
	}
	var seed struct {
		Cards []struct {
			CardID int    `json:"card_id"`
			Name   string `json:"name"`
		} `json:"cards"`
		ContainerCards []struct {
			CardID int    `json:"card_id"`
			Name   string `json:"name"`
		} `json:"container_cards"`
		Gachas []struct {
			CardIDs    []int                    `json:"cardids"`
			RewardPool []release.WeightedReward `json:"reward_pool"`
		} `json:"gachas"`
	}
	seedContent, err := os.ReadFile(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(seedContent, &seed); err != nil {
		t.Fatal(err)
	}
	appendSeedTemplate := func(cardID int, name string) {
		if _, exists := seenCardIDs[cardID]; exists {
			return
		}
		seenCardIDs[cardID] = struct{}{}
		cardTemplates = append(cardTemplates, cardTemplate(cardID, name))
	}
	for _, card := range seed.Cards {
		appendSeedTemplate(card.CardID, card.Name)
	}
	for _, card := range seed.ContainerCards {
		appendSeedTemplate(card.CardID, card.Name)
	}
	for _, gacha := range seed.Gachas {
		for _, cardID := range gacha.CardIDs {
			appendSeedTemplate(cardID, fmt.Sprintf("Test gacha card %d", cardID))
		}
		for _, entry := range gacha.RewardPool {
			if entry.Reward.Type == 6 {
				appendSeedTemplate(entry.Reward.RewardTypeID, "Test mixed gacha card")
			}
		}
	}
	master := map[string]any{
		"schema_version":                  15,
		"card_collection_pages":           [][]int{{10000010, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		"client_profile":                  "cn602-bootstrap",
		"card_progression_config_version": 4,
		"card_progression_policy": map[string]any{
			"config_version": 4, "fusion_gold_per_material_per_base_level": 100,
			"maximum_card_material_count": 100,
			"fusion_success_types": []map[string]any{
				{"success_type": 0, "weight": 90, "experience_permille": 1000},
				{"success_type": 1, "weight": 9, "experience_permille": 1500},
				{"success_type": 2, "weight": 1, "experience_permille": 2500},
			},
			"fame_normal":  map[string]any{"hp": 300, "attack": 200, "magic": 200, "mind": 100},
			"fame_premium": map[string]any{"hp": 600, "attack": 400, "magic": 400, "mind": 200},
			"source_state": map[string]any{
				"experience_tables": "CONFIRMED: test", "parameter_formula": "CONFIRMED: test",
				"fusion_material_experience": "INFERRED: test", "fusion_gold": "INFERRED: test",
				"fusion_gold_consumption": "CONFIRMED: test", "sell_gold_consumption": "CONFIRMED: test",
				"ordinary_card_material_experience": "PLACEHOLDER: test",
				"material_selection":                "CONFIRMED: test", "skill_level_maximum": "CONFIRMED: test",
				"fame_inheritance": "CONFIRMED: test", "success_multipliers": "INFERRED: test",
				"success_weights": "PLACEHOLDER: test",
			},
		},
		"card_experience_tables": map[string]any{"1": func() []int {
			values := make([]int, 59)
			for index := range values {
				values[index] = 10
			}
			return values
		}()},
		"sphere_config_version": 3,
		"sphere_progression_policy": map[string]any{
			"config_version": 3, "material_level_bonus_permille_per_level": 100,
			"source_state": map[string]any{
				"experience_tables": "CONFIRMED: test", "material_base_experience": "INFERRED: test",
				"material_level_bonus": "INFERRED: test", "fusion_gold": "PLACEHOLDER: test",
				"evolution_material_selection": "CONFIRMED: test", "evolution_level_requirement": "CONFIRMED: test",
				"evolution_progression_preservation": "INFERRED: test", "account_seed": "PLACEHOLDER: test",
			},
		},
		"buddy_config_version": 2,
		"buddy_progression_policy": map[string]any{
			"config_version": 2, "maximum_material_count": 100,
			"source_state": map[string]any{
				"experience_tables": "CONFIRMED: test", "material_experience_consumption": "CONFIRMED: test",
				"fusion_gold_consumption": "CONFIRMED: test", "ordinary_buddy_material_experience": "PLACEHOLDER: test",
				"ancient_melody_experience": "INFERRED: test", "success_multipliers": "INFERRED: test",
				"success_weights": "PLACEHOLDER: test", "evolution_material_selection": "CONFIRMED: test",
				"evolution_level_requirement": "CONFIRMED: test", "evolution_progression_preservation": "INFERRED: test",
				"account_seed": "PLACEHOLDER: test",
			},
		},
		"support_deck_config_version": 1,
		"support_deck_set_card_num":   3,
		"source":                      map[string]any{"test": true},
		"card_templates":              cardTemplates,
		"card_category_profiles": []map[string]any{{
			"categoryid": 60200001, "name": "Test card series", "order": 2,
			"view_type": 0, "evidence": "PLACEHOLDER: test",
		}},
		"card_group_profiles": []map[string]any{
			{"groupid": 20020001, "categoryid": 60200001, "name": "Test 1", "order": 10, "deck_limit_bossid": 0, "member_cardids": []int{10000010}, "name_evidence": "INFERRED: test"},
			{"groupid": 30020001, "categoryid": 60200001, "name": "Test 2", "order": 11, "deck_limit_bossid": 0, "member_cardids": []int{10000010}, "name_evidence": "INFERRED: test"},
			{"groupid": 30020002, "categoryid": 60200001, "name": "Test 3", "order": 12, "deck_limit_bossid": 0, "member_cardids": []int{10000010}, "name_evidence": "INFERRED: test"},
			{"groupid": 30020003, "categoryid": 60200001, "name": "Test 4", "order": 13, "deck_limit_bossid": 0, "member_cardids": []int{10000010}, "name_evidence": "INFERRED: test"},
			{"groupid": 30020005, "categoryid": 60200001, "name": "Test 5", "order": 14, "deck_limit_bossid": 0, "member_cardids": []int{10000010}, "name_evidence": "INFERRED: test"},
			{"groupid": 90020001, "categoryid": 60200001, "name": "Test 6", "order": 15, "deck_limit_bossid": 0, "member_cardids": []int{10000010}, "name_evidence": "INFERRED: test"},
		},
		"card_group_policy": map[string]any{
			"config_version": 2, "client_contract": "CONFIRMED: test",
			"group_membership": "CONFIRMED: test", "display_names": "INFERRED: test",
			"category_projection": "PLACEHOLDER: test", "deck_limit_bossid": "CONFIRMED: test",
			"claims_original_service_topology": false,
		},
		"stack_card_templates": []map[string]any{
			stackTemplate(20000006),
			stackTemplate(20000011),
			stackTemplate(20000016),
			buddyStackTemplate(20005001, 10, 10),
			buddyStackTemplate(20005002, 9, 7200),
		},
		"evolution_transitions": []map[string]any{{
			"from_cardid":    10000010,
			"to_cardid":      10000011,
			"evolution_type": 0,
			"gold":           100,
			"materials": []map[string]any{{
				"cardid": 20000006,
				"num":    1,
			}},
		}},
		"card_development_policy": map[string]any{
			"time_every_fame_seconds": 3600,
			"coin_every_hour":         1,
			"help_path":               "/disabled/web",
			"evidence":                "PLACEHOLDER",
		},
		"skipped_incomplete_evolution_source_cardids": []int{},
		"sphere_definitions": []map[string]any{
			sphereDefinition(15000010), sphereDefinition(15000020), sphereDefinition(15000030),
		},
		"sphere_experience_tables": map[string]any{"1": []int{1}},
		"sphere_evolution_prices": map[string]any{
			"NORMAL": []int{1, 2, 3, 4}, "RARE": []int{1, 2, 3, 4}, "MILLIONRARE": []int{1, 2, 3, 4},
		},
		"sphere_seed_templates": []map[string]any{
			sphereSeed(603000001, 15000010), sphereSeed(603000002, 15000020), sphereSeed(603000003, 15000030),
		},
		"sphere_seed_decks": map[string]any{
			"1": []int64{603000001, 603000002, 603000003},
			"2": []int64{603000001, 603000002, 603000003},
			"3": []int64{603000001, 603000002, 603000003},
			"4": []int64{603000001, 603000002, 603000003},
		},
		"buddy_definitions":       []map[string]any{buddyDefinition},
		"buddy_experience_tables": map[string]any{"401": []int{1}},
		"buddy_evolution_prices": map[string]any{
			"NORMAL": []int{1, 2, 3, 4, 5}, "RARE": []int{1, 2, 3, 4, 5},
			"MILLIONRARE": []int{1, 2, 3, 4, 5}, "EXRARE": []int{1, 2, 3, 4, 5},
			"LEGEND": []int{1, 2, 3, 4, 5},
		},
		"buddy_seed_templates": []map[string]any{buddySeed},
		"buddy_seed_decks": map[string]any{
			"1": []int64{1, 0, 0, 0, 0}, "2": []int64{1, 0, 0, 0, 0},
			"3": []int64{1, 0, 0, 0, 0}, "4": []int64{1, 0, 0, 0, 0},
		},
		"buddy_seed_stack_cards": []map[string]any{
			{"cardid": 20005001, "num": 1, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0, "add_exp": 10, "base_add_price": 100, "material_type": 10},
			{"cardid": 20005002, "num": 100, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0, "add_exp": 7200, "base_add_price": 100, "material_type": 9},
		},
		"support_deck_slot_unlock_rules": []map[string]any{
			{"slot_index": 1, "level": 70, "card_collection_num": 300, "gold": 500000},
			{"slot_index": 2, "level": 100, "card_collection_num": 400, "gold": 1000000},
			{"slot_index": 3, "level": 130, "card_collection_num": 500, "gold": 1000000},
		},
	}
	// Bootstrap fixtures resolve the newly configured reward identities using
	// their existing minimal definitions; production closure is checked separately.
	for _, spec := range []struct {
		key, idKey string
		kind       int
	}{
		{"buddy_definitions", "buddyid", 19}, {"sphere_definitions", "sphrid", 15}, {"stack_card_templates", "cardid", 13},
	} {
		rows := master[spec.key].([]map[string]any)
		seen := map[int]bool{}
		for _, row := range rows {
			seen[row[spec.idKey].(int)] = true
		}
		for _, gacha := range seed.Gachas {
			for _, entry := range gacha.RewardPool {
				id := entry.Reward.RewardTypeID
				if entry.Reward.Type != spec.kind || seen[id] {
					continue
				}
				seen[id] = true
				copy := map[string]any{}
				for k, v := range rows[0] {
					copy[k] = v
				}
				copy[spec.idKey] = id
				if spec.kind == 19 {
					copy["same_buddyid"] = id
				}
				if spec.kind == 15 {
					copy["same_sphrid"] = id
				}
				rows = append(rows, copy)
			}
		}
		master[spec.key] = rows
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	cardMasterPath := filepath.Join(root, "cn602-card-runtime-master.json")
	if err := os.WriteFile(cardMasterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return cardMasterPath
}

func writeTestExploreMaster(t *testing.T, root string) string {
	t.Helper()
	reward := map[string]any{
		"type": 4, "num": 10, "reward_typeid": 0,
		"card_lv": 0, "card_fame": 0, "card_love": 0,
		"card_skill_lv": []int{},
	}
	master := map[string]any{
		"schema_version": 1,
		"client_profile": "cn602-bootstrap",
		"source":         map[string]any{"test": true},
		"maps": []map[string]any{{
			"id": 1000, "map_type": "GRASS", "map_ids": []int{1000, 1000}, "bgm_id": 10020,
		}},
		"stage": map[string]any{
			"explore_stageid": 1000, "stage_name": "Test Explore",
			"stage_mapid": 1000, "stage_type": 0, "state_flag": 0,
			"limit_sec": 60,
			"floors":    []map[string]any{{"explore_floorid": 1000, "floor_name": "Test Floor"}},
		},
		"floor_rarity": 0,
		"events": []map[string]any{{
			"treasureboxes": []map[string]any{{
				"reward": []map[string]any{reward}, "bonus": []any{}, "talk_cardid": 10000010, "is_rare": 0,
			}},
			"symbols": []any{},
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-explore-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestStoryMaster(t *testing.T, root string) string {
	t.Helper()
	story := func(id int) map[string]any {
		return map[string]any{
			"story_mainid": id, "state_flag": 0, "title": "第1话",
			"unlock_text": "", "reward_type_flag": 0, "script_segment_count": 1,
		}
	}
	part := func(id int, name string, storyID int) []map[string]any {
		return []map[string]any{{
			"story_main_partid": id,
			"name":              name,
			"sections": []map[string]any{{
				"story_main_sectionid": id * 100,
				"section_title":        "序章",
				"stories":              []map[string]any{story(storyID)},
			}},
		}}
	}
	master := map[string]any{
		"schema_version": 5, "client_profile": "cn602-bootstrap",
		"source":       map[string]any{"kind": "test"},
		"normal_parts": part(1, "第一部", 10001),
		"cn_parts":     part(1, "国服专属剧情", 100001),
		"sub_characters": []map[string]any{{
			"story_main_charaid": 1, "name": "角色剧情", "pictid": 0,
			"sections": []map[string]any{{
				"story_sub_sectionid": 10000010, "story_sub_section_type": 0,
				"section_title": "Test Card", "pictid": 10000010,
				"stories": []map[string]any{{
					"story_subid": 20010110, "state_flag": 0, "title": "Test Sub Story",
					"unlock_text": "获得对应卡牌后解锁", "script_segment_count": 1,
					"feature_reward": map[string]any{
						"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
						"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
					},
				}},
			}},
		}},
		"sub_character_container_contract": map[string]any{
			"evidence": "PLACEHOLDER", "reason": "NO_OFFICIAL_OUTER_GROUPING_FIELDS",
			"official_row_count": 1, "observed_row_widths": []int{3},
			"meaningful_column_indexes": []int{0, 1}, "nonempty_additional_value_count": 0,
			"managed_consumed_fields":   []string{"name", "pictid", "sections"},
			"managed_unused_fields":     []string{"story_main_charaid"},
			"published_container_count": 1,
			"published_container": map[string]any{
				"story_main_charaid": 1, "name": "角色剧情", "pictid": 0,
			},
		},
		"sub_section_mappings": []map[string]any{{
			"story_sub_sectionid": 10000010, "card_name": "Test Card",
			"script_chapter": "Test Card", "story_ids": []int{20010110},
			"evidence": "CONFIRMED",
		}},
		"events": []map[string]any{{
			"story_eventid": 100964, "name": "Test Event", "pictid": 10000010,
			"stories": []map[string]any{{
				"sub": map[string]any{
					"story_subid": 10096410, "state_flag": 8, "title": "Test Event Story",
					"unlock_text": "", "script_segment_count": 1,
					"feature_reward": map[string]any{
						"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
						"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
					},
				},
				"materials": []any{},
			}},
		}},
		"event_mappings": []map[string]any{{
			"story_eventid": 100964, "script_chapter_labels": []string{"Test Event"},
			"story_ids": []int{10096410}, "pictid": 10000010,
			"pict_candidates":   []int{10000010},
			"topology_evidence": "CONFIRMED_STORY_ID_PREFIX",
			"pictid_evidence":   "INFERRED_FIRST_SCRIPT_CARD_PICT",
		}},
		"reward_policy": map[string]any{
			"config_version": 1, "state": "PASS",
			"main_first_clear": map[string]any{
				"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
				"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
			},
			"sub_first_clear": map[string]any{
				"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
				"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
			},
			"event_first_clear": map[string]any{
				"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
				"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
			},
			"source_state": map[string]any{
				"client_contract": "CONFIRMED", "reward_schedule": "PLACEHOLDER",
			},
		},
		"story_battles": map[string]any{
			"official_ids":     []int{60004110},
			"referenced_ids":   []int{60004110},
			"unreferenced_ids": []int{},
			"references": []map[string]any{{
				"story_kind": "sub", "story_id": 10096410,
				"story_battle_ids": []int{60004110},
			}},
		},
		"event_page_profile": map[string]any{
			"eventid": 0, "bg_pictid": 0, "home_button_pictid": 0,
			"end_time": 2147483647, "info_url": "",
			"update_info": "本地活动中心", "itemid": []int{},
			"buttons": []any{}, "is_new_solo": 0, "is_new_multi": 0,
			"evidence": "PLACEHOLDER_GENERIC_OFFICIAL_EVENTPAGE_ASSETS",
		},
		"popup_profile": map[string]any{
			"popupid": 602000001, "popup_type": 7, "priority": 100,
			"is_spview": 0, "banner_url": "", "open_url": "",
			"title_text": "本地服务公告", "body_text": "本地服务已连接。",
			"button_text": "确定", "destination": "", "is_system": 1,
			"item_lineup": []any{}, "gacha": []any{}, "mission": []any{},
			"item_icon": []any{}, "evidence": "PLACEHOLDER_LOCAL_INFORMATION",
		},
		"normal_story_count": 1, "cn_story_count": 1,
		"sub_story_count": 1, "event_story_count": 1,
		"normal_missing_talk_ids": []int{}, "cn_missing_talk_ids": []int{},
		"normal_orphan_script_ids": []int{}, "cn_orphan_script_ids": []int{},
		"sub_orphan_script_ids": []int{}, "sub_missing_talk_ids": []int{},
		"excluded_test_script_ids": []int{}, "script_label_fallback_ids": []int{},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-story-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestNaviMaster(t *testing.T, root string) string {
	t.Helper()
	navigators := make([]map[string]any, 0, 49)
	for naviID := 0; naviID < 49; naviID++ {
		voiceID := naviID
		if naviID == 41 {
			voiceID = 18
		}
		folder := fmt.Sprintf("navi_%d", naviID)
		navigators = append(navigators, map[string]any{
			"navi_id":             naviID,
			"name":                fmt.Sprintf("Test Navi %d", naviID),
			"pict_id":             50000000 + naviID*1000,
			"item_pict_id":        30000 + naviID,
			"live2d_folder":       folder,
			"live2d_bundle":       "live2d/live2d_" + folder + ".dat",
			"live2d_source_state": "PRESENT_IN_CN_OFFICIAL_ASSET_MAP",
			"voice_id":            voiceID,
			"client_published":    true,
		})
	}
	master := map[string]any{
		"schema_version": 3,
		"client_profile": "cn602-bootstrap",
		"source":         map[string]any{"test": true},
		"publication_policy": map[string]any{
			"evidence":               "CONFIRMED_CN_OFFICIAL_ASSET_MAP",
			"live2d_bundle_required": true,
			"voice_source_required":  false,
			"voice_fallback":         "OPTIONAL_SILENT_FALLBACK",
		},
		"local_purchase": map[string]any{
			"evidence": "PLACEHOLDER_LOCAL_PROFILE",
			"currency": "CRYSTAL",
			"price":    50,
		},
		"summary": map[string]any{
			"official_navigator_rows":     len(navigators),
			"client_published":            len(navigators),
			"excluded_missing_live2d":     0,
			"excluded_missing_live2d_ids": []int{},
		},
		"navigators": navigators,
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-navi-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestCPKAliases(t *testing.T, root string) string {
	t.Helper()
	content := []byte(`{"schema_version":1,"client_profile":"cn602-bootstrap","generated_utc":"test","evidence":"test","source_inventory":"test","aliases":[]}`)
	manifestPath := filepath.Join(root, "cn602-cpk-aliases.json")
	if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath
}

func writeTestItemMaster(t *testing.T, root string) string {
	t.Helper()
	item := func(itemID int, name string, pictID int, itemType string) map[string]any {
		return map[string]any{
			"item_id": itemID, "name": name, "pict_id": pictID,
			"item_type": itemType, "function": "", "function_value": 0,
			"love_up_price": 0,
			"max_owned":     9999, "daily_limited": 0, "description": name,
		}
	}
	userBuffProfiles := make([]map[string]any, 527)
	for index := range userBuffProfiles {
		userBuffProfiles[index] = map[string]any{
			"user_buff_id": index + 1, "item_id": 1000, "required_num": 1,
			"not_enough_error_text": "test", "duration_seconds": 1800,
		}
	}
	tradeShopLineups := make([]map[string]any, 49)
	for index := range tradeShopLineups {
		tradeShopLineups[index] = map[string]any{
			"lineupid": 60290002 + index, "lineup_name": fmt.Sprintf("Test trade card %d", index+1),
			"stock_num": 1, "is_lineup_new": 0, "is_lineup_old": 0, "pictid": 0,
			"prices": []map[string]any{{"type": 4, "id": 4000, "num": 1000, "point_card_condition": []map[string]any{}}},
			"rewards": []map[string]any{{
				"type": 6, "num": 1, "reward_typeid": 11000001 + index,
				"card_lv": 1, "card_fame": 1, "card_love": 0, "card_skill_lv": []int{1},
			}},
			"evidence": "INFERRED_OFFICIAL_CN_BOSS_COIN_EXCHANGE_ACQUISITION",
		}
	}
	exchangeItemIDs := []int{
		6046, 6049, 6060, 6099,
		8829, 8830, 8831, 8832, 8833, 8834, 8835,
		8919, 8920, 8921, 8922, 8923, 8991,
	}
	exchangeProfiles := make([]map[string]any, len(exchangeItemIDs))
	crystalGachaItem := item(9010, "水晶扭蛋币", 90010, "GACHA_TICKET")
	crystalGachaItem["description"] = "可在水晶扭蛋币商店中兑换获得"
	testItems := []map[string]any{
		item(1000, "Test BP Heal", 10080, "BP_HEAL"),
		item(1200, "Test AP Heal", 10010, "AP_HEAL"),
		item(2000, "Test Gacha Ticket", 10060, "GACHA_TICKET"),
		item(2001, "Test First Gacha Ticket", 10060, "GACHA_TICKET"),
		item(2002, "Test Attribute Gacha Ticket", 10062, "GACHA_TICKET"),
		item(3999, "骑士徽章", 19001, "EVENT_POINT"),
		item(4000, "大硬币", 20010, "EVENT_POINT"),
		item(6201, "Test Medal", 16201, "GACHA_TICKET"),
		item(7025, "Test Fragment", 17025, "EVENT_POINT"),
		item(6202, "Test Rare Gacha Ticket", 16202, "GACHA_TICKET"),
		item(6203, "Test Unowned Gacha Ticket", 16203, "GACHA_TICKET"),
		crystalGachaItem,
	}
	for index, itemID := range exchangeItemIDs {
		testItems = append(testItems, item(itemID, fmt.Sprintf("Test exchange %d", itemID), 60000+index, "EVENT_POINT"))
		exchangeProfiles[index] = map[string]any{
			"item_id": itemID, "is_appear_event": 0, "eventid": 60280000 + itemID,
			"need_num": 1,
			"reward": map[string]any{
				"type": 6, "num": 1, "reward_typeid": 11000001 + index,
				"card_lv": 1, "card_fame": 1, "card_love": 0, "card_skill_lv": []int{1},
			},
			"evidence": "INFERRED_OFFICIAL_CN_ITEM_NAME_EXACT_CARD_FAMILY_LOCAL_POLICY",
		}
	}
	master := map[string]any{
		"schema_version": 9,
		"client_profile": "cn602-bootstrap",
		"source": map[string]any{
			"test": true,
			"user_buff": map[string]any{
				"container_path":  "assets/resources/container/user/user_buff.csv.txt",
				"container_bytes": 1, "container_sha256": strings.Repeat("0", 64),
				"textasset_bytes": 1, "textasset_sha256": strings.Repeat("0", 64),
			},
		},
		"user_buff_profiles":          userBuffProfiles,
		"items":                       testItems,
		"item_gacha_profiles":         []map[string]any{},
		"item_gacha_source_gap_ids":   []int{},
		"item_exchange_profiles":      exchangeProfiles,
		"item_exchange_source_status": "PUBLISHED_LOCAL_POLICY_FROM_OFFICIAL_CN_ITEM_AND_CARD_IDENTITIES",
		"item_exchange_policy": map[string]any{
			"config_version": 1, "need_num": 1, "event_id_offset": 60280000,
			"selection": "exact_card_name_else_unique_same_card_family_substring",
			"source_state": map[string]any{
				"client_contract": "CONFIRMED", "item_identity": "CONFIRMED",
				"card_family_identity": "INFERRED", "explicit_fame": "CONFIRMED",
				"default_fame": "PLACEHOLDER", "need_num": "PLACEHOLDER", "event_id": "PLACEHOLDER",
			},
			"official_service_values_claimed": false,
		},
		"item_lack_tip_profiles": []map[string]any{
			{"idx": 1, "title": "徽章不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 2, "title": "水晶不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 3, "title": "金币不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 4, "title": "强化素材不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 5, "title": "体力不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 6, "title": "钥匙不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 7, "title": "扭蛋券不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
			{"idx": 8, "title": "水晶扭蛋币不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}, "evidence": "LOCAL_POLICY_OVER_CONFIRMED_CN_ITEM_LACK_ENUM"},
		},
		"item_lack_tip_source_status": "PUBLISHED_LOCAL_TEXT_POLICY_FROM_MANAGED_ENUM_AND_OFFICIAL_CN_IDENTITIES",
		"item_lack_tip_policy": map[string]any{
			"config_version": 2, "reachable_indices": []int{2, 8},
			"navigation_publication": "NON_NAVIGATING_LOCAL_CLIENT_CONTRACT_ROW",
			"source_state": map[string]any{
				"client_contract": "CONFIRMED", "enum_identity": "CONFIRMED",
				"item_9010_identity": "CONFIRMED", "display_text": "PLACEHOLDER",
			},
			"official_service_text_claimed": false,
		},
		"event_shop_profiles":      []map[string]any{},
		"event_shop_source_status": "ABSENT_FROM_CN_OFFICIAL_CONTAINER_AND_PACKAGED_EVENT_SHOP_WEB_FILES",
		"event_shop_source_audit": map[string]any{
			"container_sha256":               strings.Repeat("0", 64),
			"textasset_count":                135,
			"candidate_paths":                []string{},
			"packaged_web_files":             []string{"assets/a.js", "assets/b.js", "assets/c.html"},
			"packaged_event_shop_candidates": []string{},
		},
		"trade_shop_profiles": []map[string]any{{
			"trade_shopid": 60290001, "name": "BOSS币交换所", "text": "Test trade shop",
			"shop_type": 1, "tab_type": 0, "end_time": 2147483647, "is_new": 0, "pictid": 0,
			"lineups": tradeShopLineups, "evidence": "LOCAL_POLICY_OVER_OFFICIAL_CN_ITEM_AND_CARD_IDENTITIES",
		}},
		"trade_shop_policy": map[string]any{
			"config_version": 1, "currency_item_id": 4000, "price_per_card": 1000,
			"stock_per_card": 1, "card_acquisition_text": "BOSS币交换所",
			"selection": "lowest_rarity_then_card_id_per_official_same_card_family",
			"source_state": map[string]any{
				"client_contract": "CONFIRMED", "currency_identity": "CONFIRMED",
				"card_identity": "INFERRED", "price": "PLACEHOLDER", "stock": "PLACEHOLDER",
			},
			"official_service_values_claimed": false,
		},
		"trade_shop_source_status":  "PUBLISHED_LOCAL_POLICY_FROM_OFFICIAL_CN_IDENTITIES",
		"skipped_inactive_item_ids": []int{},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-item-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestAvatarMaster(t *testing.T, root string) string {
	t.Helper()
	part := func(partID int, arthurMask int, slotMask int) map[string]any {
		return map[string]any{
			"part_id": partID, "name": fmt.Sprintf("Test Avatar part %d", partID),
			"arthur_mask": arthurMask, "slot_mask": slotMask, "icon_pict_id": partID,
			"is_new_list": 0, "series_id": 0,
		}
	}
	master := map[string]any{
		"schema_version": 1, "client_profile": "cn602-bootstrap",
		"source": map[string]any{"test": true}, "local_account_config_version": 1,
		"initial_owned_part_ids": []int{1, 2, 3, 4, 11, 12, 13, 14},
		"default_decks": map[string]any{
			"1": []int{0, 1, 1, 0, 0, 0, 0}, "2": []int{11, 2, 2, 0, 11, 0, 0},
			"3": []int{0, 3, 3, 3, 0, 0, 14}, "4": []int{12, 4, 4, 0, 12, 0, 13},
		},
		"shop_policy": map[string]any{
			"pay_type": 1, "pay_typeid": 0, "price": 1000,
			"appear_end": 0, "new_appear_end": 0,
			"evidence": "PLACEHOLDER_LOCAL_GOLD_PRICE",
		},
		"part_definitions": []map[string]any{
			part(1, 2, 6), part(2, 4, 6), part(3, 8, 14), part(4, 16, 6),
			part(11, 4, 17), part(12, 16, 17), part(13, 16, 64), part(14, 8, 64),
		},
		"series": []map[string]any{{"series_id": 0, "name": "Test"}},
		"series_completions": []map[string]any{{
			"completion_id": 1, "name": "Test completion", "part_ids": []int{1},
			"rewards": []map[string]any{{"type": "SPHR", "num": 1, "reward_id": 15000010}},
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-avatar-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestStampMaster(t *testing.T, root string) string {
	t.Helper()
	master := map[string]any{
		"schema_version": 1,
		"client_profile": "cn602-bootstrap",
		"source":         map[string]any{"test": true},
		"category_order": []int{1, 7, 2, 3, 4, 5, 6},
		"stamps": []map[string]any{{
			"stamp_id": 100001, "category": 1, "label": "Test Stamp",
			"display_text": "Test Stamp", "default_owned": true,
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-stamp-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestHonorMaster(t *testing.T, root string) string {
	t.Helper()
	master := map[string]any{
		"schema_version": 1,
		"client_profile": "cn602-bootstrap",
		"source":         map[string]any{"test": true},
		"honors": []map[string]any{{
			"honor_id": 10100001, "name": "Test Honor", "slot_mask": 5,
			"same_card_id": 0, "get_type": 0, "default_owned": true,
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-honor-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func writeTestSave(t *testing.T, root string) string {
	t.Helper()
	templatePath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(root, "cn602-save.json")
	if err := os.WriteFile(savePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return savePath
}

func TestCaptureUnknownCNRequest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(
		http.MethodPost,
		"http://local/unimplemented.php?probe=1",
		strings.NewReader(`{"uid":"local"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d", response.Code)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("capture log is empty")
	}
	var entry capture
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Path != "/unimplemented.php" || entry.Query != "probe=1" ||
		entry.Body != `{"uid":"local"}` {
		t.Fatalf("unexpected capture: %+v", entry)
	}
	if scanner.Scan() {
		t.Fatal("capture log has unexpected extra entry")
	}
}

func TestCN602ServerList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(http.MethodGet, "http://local/local/server/default.list?time=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	want := "ALL,0,本地服务器,10.0.2.2,18081,0,,6.0.3,,,\nALL,0,请更新客户端,10.0.2.2,18081,0,,ALL," + cnClientReleaseURL + "," + cnClientUpdateTips() + ",\n"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}
	if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
}

func TestCN602BootstrapNoUpdateAndLogAcknowledgement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	for _, path := range []string{"/xxxx/g0/apk.config", "/xxxx/g0/dex.config"} {
		request := httptest.NewRequest(http.MethodGet, "http://local"+path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "{}" {
			t.Fatalf("%s: status=%d body=%q", path, response.Code, response.Body.String())
		}
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"http://local//log.php",
		strings.NewReader(`{"log_cat":"Activation"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"log_cat\":\"Activation\"}\n" {
		t.Fatalf("log acknowledgement: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestCN602ModuleSwitchBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(
		http.MethodPost,
		"http://local//mods_switch.php",
		strings.NewReader(`{"clver":"6.0.2","market":2}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload struct {
		ResponseCode int   `json:"res_code"`
		ModuleState  int64 `json:"mods_state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ResponseCode != 0 || payload.ModuleState != cn602LocalModuleSwitchState {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.ModuleState&(int64(1)<<31) == 0 {
		t.Fatalf("Card development module switch is closed: %d", payload.ModuleState)
	}
	if payload.ModuleState&(int64(1)<<29) == 0 {
		t.Fatalf("Battle failure advice module switch is closed: %d", payload.ModuleState)
	}
	if payload.ModuleState&(int64(1)<<34) == 0 {
		t.Fatalf("Avatar shop module switch is closed: %d", payload.ModuleState)
	}
}

func TestCN602LocalSDKLoginContract(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(
		http.MethodPost,
		"http://local//loginSDK.php",
		strings.NewReader(`{"uuid":"00000000-0000-0000-0000-000000000001","clver":"6.0.3"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"res_code", "res_str", "sess_key", "api_url", "res_patch_url",
		"realname_status", "version_url", "res_img_url", "res_cpk_url",
		"web_url", "charge_url", "products_url", "update_url", "gid",
		"userid", "uid", "session", "room_config", "comment_url",
		"display_pictures", "sp_resource_flag",
	} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("required client field %q is absent", key)
		}
	}
	if payload["api_url"] != "http://10.0.2.2:18081/" ||
		payload["uid"] != "local-cn-user" || payload["session"] != "local-cn-session" {
		t.Fatalf("unexpected local login identity or API boundary: %+v", payload)
	}
	sessionKey, ok := payload["sess_key"].(string)
	if !ok || !strings.HasPrefix(sessionKey, "local-cn-") || sessionKey == cnSessionForUserID(cnPrimaryUserID) {
		t.Fatalf("login returned a predictable or malformed account session: %q", sessionKey)
	}
	if pictures, ok := payload["display_pictures"].(map[string]any); !ok || len(pictures) != 0 {
		t.Fatalf("display_pictures must be an empty object: %#v", payload["display_pictures"])
	}
}

func TestCN602BusinessRoutesRequirePersistedRandomSession(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	testHandler, ok := handler.(testSessionHandler)
	if !ok || testHandler.sessionKey == "" {
		t.Fatal("test handler has no authenticated session")
	}

	valid := httptest.NewRecorder()
	testHandler.Handler.ServeHTTP(valid, httptest.NewRequest(
		http.MethodPost,
		"http://local/Connect",
		strings.NewReader(testHandler.sessionKey+"{}"),
	))
	if valid.Code != http.StatusOK {
		t.Fatalf("valid session status=%d body=%q", valid.Code, valid.Body.String())
	}

	forged := httptest.NewRecorder()
	forgedRequest := httptest.NewRequest(
		http.MethodPost,
		"http://local/Connect",
		strings.NewReader("local-cn-0000000000000000000000000000000000000000000000000000000000000000{}"),
	)
	forgedRequest.Header.Set(cnAccountUserHeader, strconv.Itoa(cnPrimaryUserID))
	testHandler.Handler.ServeHTTP(forged, forgedRequest)
	if forged.Code != http.StatusUnauthorized {
		t.Fatalf("forged session status=%d body=%q", forged.Code, forged.Body.String())
	}
}

func TestCN602PingAndConnectProtocolEnvelope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	for _, test := range []struct {
		path        string
		payloadKeys []string
	}{
		{path: "/Ping", payloadKeys: []string{"stamp"}},
		{path: "/Connect", payloadKeys: []string{
			"is_user", "push_option", "game_option", "revision",
			"navi_unlock_flag", "cl_behavior_flag",
		}},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(
			http.MethodPost,
			"http://local"+test.path,
			strings.NewReader("local-cn-session-key{}"),
		))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%q", test.path, response.Code, response.Body.String())
		}
		lines := strings.Split(strings.TrimSuffix(response.Body.String(), "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("%s: protocol lines=%d body=%q", test.path, len(lines), response.Body.String())
		}
		var common map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
			t.Fatalf("%s common: %v", test.path, err)
		}
		for _, key := range []string{
			"res_code", "res_str", "notification", "revision", "is_appupdate",
			"res_err_action", "res_is_del_savedata",
		} {
			if _, ok := common[key]; !ok {
				t.Fatalf("%s: common field %q is absent", test.path, key)
			}
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
			t.Fatalf("%s payload: %v", test.path, err)
		}
		for _, key := range test.payloadKeys {
			if _, ok := payload[key]; !ok {
				t.Fatalf("%s: payload field %q is absent", test.path, key)
			}
		}
	}
}

func TestCN602BusinessAdapterBootstrapReadChain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	for _, test := range []struct {
		path        string
		payloadKeys []string
	}{
		{path: "/HonorDeckShow", payloadKeys: []string{"deck_honorids"}},
		{path: "/CardCollectionShow", payloadKeys: []string{"0", "1", "2"}},
		{path: "/HomeShow", payloadKeys: []string{
			"user", "login_bonus_daily", "login_bonus_beginner",
			"login_bonus_total", "login_bonus_event", "quests", "banners",
			"fp_reward", "function_flag", "function_show_state", "server_time",
			"background", "max_rank", "tutorial_flag", "firstpay",
		}},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(
			http.MethodPost,
			"http://local"+test.path,
			strings.NewReader("local-cn-session-key"),
		))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%q", test.path, response.Code, response.Body.String())
		}
		lines := strings.Split(response.Body.String(), "\n")
		if len(lines) != 3 {
			t.Fatalf("%s: protocol lines=%d body=%q", test.path, len(lines), response.Body.String())
		}
		var common map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
			t.Fatalf("%s common: %v", test.path, err)
		}
		if common["revision"] != float64(790) || common["res_code"] != float64(0) {
			t.Fatalf("%s: unexpected common response %#v", test.path, common)
		}
		notifications, ok := common["notification"].([]any)
		if !ok || len(notifications) != 1 {
			t.Fatalf("%s: notification contract %#v", test.path, common["notification"])
		}
		notification, ok := notifications[0].(map[string]any)
		if !ok || int64(notification["unlock_feature_flag"].(float64))&(int64(1)<<15) == 0 {
			t.Fatalf("%s: CARD_DECK feature is not unlocked: %#v", test.path, notifications[0])
		}
		if int64(notification["unlock_feature_flag"].(float64))&(int64(1)<<30) == 0 {
			t.Fatalf("%s: AVATAR_PARTS_SHOP feature is not unlocked: %#v", test.path, notifications[0])
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
			t.Fatalf("%s payload: %v", test.path, err)
		}
		for _, key := range test.payloadKeys {
			if _, ok := payload[key]; !ok {
				t.Fatalf("%s: payload field %q is absent", test.path, key)
			}
		}
		if test.path == "/HomeShow" && payload["firstpay"] != float64(2147483647) {
			t.Fatalf("%s: local no-payment firstpay flag=%#v", test.path, payload["firstpay"])
		}
	}
}

func TestCN602TutorialFlagRouteAndDTOAdapter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"http://local/TutorialFlag",
		strings.NewReader(cn602LocalSession+`{"flag":67108863,"step":25}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	lines := strings.Split(response.Body.String(), "\n")
	if len(lines) != 3 {
		t.Fatalf("protocol lines=%d body=%q", len(lines), response.Body.String())
	}
	var common map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
		t.Fatal(err)
	}
	if common["res_code"] != float64(0) {
		t.Fatalf("unexpected common response %#v", common)
	}
}

func TestCN602ClickLogIsAcknowledgedWithoutAnalytics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"http://local/ClickLog",
		strings.NewReader(cn602LocalSession+`{"info":[{"click_id":0,"click_num":0}],"click_time":1785692073}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	lines := strings.Split(response.Body.String(), "\n")
	if len(lines) != 3 {
		t.Fatalf("protocol lines=%d body=%q", len(lines), response.Body.String())
	}
	var common map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
		t.Fatal(err)
	}
	if common["res_code"] != float64(0) {
		t.Fatalf("unexpected common response %#v", common)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 0 {
		t.Fatalf("ClickLog ACK must not return analytics payload: %#v", payload)
	}
	var popup map[string][]any
	if err := json.Unmarshal([]byte(lines[2]), &popup); err != nil {
		t.Fatal(err)
	}
	if entries, ok := popup["popup"]; !ok || len(entries) != 0 {
		t.Fatalf("unexpected popup response %#v", popup)
	}
}

func TestCN602ItemShowSessionAdapter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"http://local/ItemShow",
		strings.NewReader(cn602LocalSession+`{"item_type":0}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	lines := strings.Split(response.Body.String(), "\n")
	if len(lines) != 3 {
		t.Fatalf("protocol lines=%d body=%q", len(lines), response.Body.String())
	}
	var payload map[string][]any
	if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"items", "gachas"} {
		entries, ok := payload[key]
		if !ok || len(entries) != 0 {
			t.Fatalf("unexpected %s payload %#v", key, payload)
		}
	}
}

func TestCN602NoPaymentProductsAndLocalCatalog(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	products := httptest.NewRecorder()
	handler.ServeHTTP(products, httptest.NewRequest(
		http.MethodGet, "http://local/disabled/products", nil,
	))
	var productCatalog struct {
		Code     int `json:"code"`
		Products []struct {
			ID      string `json:"bid"`
			Price   string `json:"price"`
			Crystal int    `json:"gold"`
		} `json:"product_list"`
	}
	if products.Code != http.StatusOK || json.Unmarshal(products.Body.Bytes(), &productCatalog) != nil || productCatalog.Code != 200 || len(productCatalog.Products) != 8 {
		t.Fatalf("products: status=%d body=%q", products.Code, products.Body.String())
	}

	for _, product := range productCatalog.Products {
		if product.Price != "0" || product.Crystal <= 0 {
			t.Fatalf("invalid free local product: %#v", product)
		}
	}

	schedule := httptest.NewRecorder()
	handler.ServeHTTP(schedule, httptest.NewRequest(
		http.MethodGet,
		"http://local/disabled/web/information/2015/7/kechengbiao?userid=1000001&type_id=1",
		nil,
	))
	if schedule.Code != http.StatusOK ||
		!strings.Contains(schedule.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(schedule.Body.String(), "副本日程表") ||
		!strings.Contains(schedule.Body.String(), "全天开放") {
		t.Fatalf("schedule: status=%d content-type=%q body=%q", schedule.Code, schedule.Header().Get("Content-Type"), schedule.Body.String())
	}

	intro := httptest.NewRecorder()
	handler.ServeHTTP(intro, httptest.NewRequest(
		http.MethodGet,
		"http://local/disabled/web/netease/fourplusone/20160626/Intro_4.png",
		nil,
	))
	if intro.Code != http.StatusOK || intro.Header().Get("Content-Type") != "image/png" ||
		intro.Header().Get("X-Kairisei-Source-State") != "PLACEHOLDER" ||
		!bytes.HasPrefix(intro.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("intro placeholder: status=%d headers=%v body=%x", intro.Code, intro.Header(), intro.Body.Bytes())
	}

	catalog := httptest.NewRecorder()
	handler.ServeHTTP(catalog, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/version/default/Android/patch/catalog.dat?v=1",
		nil,
	))
	if catalog.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%q", catalog.Code, catalog.Body.String())
	}
	reader, err := gzip.NewReader(catalog.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, confirmedRow := range []string{
		"<v>,790\n",
		"<b>,main_c/scenes/scene_Menu.dat,93936638,00000000,4,N,1,0,1\n",
		"<a>,scenes,Menu,,.prefab,main_c/scenes/scene_Menu.dat\n",
	} {
		if !strings.Contains(string(plain), confirmedRow) {
			t.Fatalf("catalog is missing confirmed CN Menu row %q", confirmedRow)
		}
	}
}

func TestCN602OfficialPatchOverlayAndNimueDelivery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cpkRoot := filepath.Join(root, "CPK")
	if err := os.Mkdir(cpkRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpkRoot, "alpha.cpk"), []byte("cpk!"), 0o600); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(root, "primary-patch")
	supplement := filepath.Join(root, "supplement-patch")
	nimuePath := filepath.Join(primary, "live2d", "live2d_nimue.dat")
	menuPath := filepath.Join(supplement, "main_c", "scenes", "scene_Menu.dat")
	containerPath := filepath.Join(primary, "main_c", "container.dat")
	if err := os.MkdirAll(filepath.Dir(nimuePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(menuPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(containerPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nimuePath, []byte("nimue"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(menuPath, []byte("menu"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(containerPath, []byte("container"), 0o600); err != nil {
		t.Fatal(err)
	}
	version := []byte(
		"<version>,790\n" +
			"<bundle_ver>,main_c/scenes/scene_Menu.dat,0,93936638\n" +
			"<bundle_ver>,main_c/container.dat,0,01020304\n" +
			"<bundle_ver>,live2d/live2d_nimue.dat,0,2206394C\n",
	)
	for index := range version {
		version[index] += cn602ScrambleKey[index%len(cn602ScrambleKey)]
	}
	for _, patchRoot := range []string{primary, supplement} {
		if err := os.WriteFile(filepath.Join(patchRoot, "version.dat"), version, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	assetMapPath := writeTestAssetMap(
		t,
		root,
		version,
		testAsset("scenes", "Menu", ".prefab", cn602MenuBundle),
		testAsset("Live2D/nimue", "nimue.model.json", ".bytes", "live2d/live2d_nimue.dat"),
		testAsset("Live2D/nimue/moc/nimue1024", "texture_00", ".png", "live2d/live2d_nimue.dat"),
		testAsset("Container/card", "card.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/card", "card_base_category.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "skill_role_player.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "support_skill_role.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "burst_skill_role.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "skill_role_enemy.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "skill_player.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "support_skill.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "burst_skill.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/skill", "skill_enemy.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/card", "card_loveup_price.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/enemy", "enemy.csv", ".txt", "main_c/container.dat"),
		testAsset("Container/icon", "buff_icon.csv", ".txt", "main_c/container.dat"),
	)
	if err := os.WriteFile(filepath.Join(root, cn602HomeEventBannerFile), []byte("event-png"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range cn602GachaBannerFiles {
		if err := os.WriteFile(filepath.Join(root, fileName), []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cardMasterPath := writeTestCardMaster(t, root)
	handler, err := New(
		logPath,
		writeTestSave(t, root),
		filepath.Join("..", "..", "config", "cn602-save-template.json"),
		assetMapPath,
		cardMasterPath,
		writeTestExploreMaster(t, root),
		writeTestStoryMaster(t, root),
		"",
		writeTestNaviMaster(t, root),
		writeTestItemMaster(t, root),
		writeTestAvatarMaster(t, root),
		assetMapPath,
		assetMapPath,
		assetMapPath,
		writeTestStampMaster(t, root),
		writeTestHonorMaster(t, root),
		filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"),
		filepath.Join("..", "..", "config", "cn602-login-bonus-runtime.json"),
		"10.0.2.2",
		18081,
		cpkRoot,
		writeTestCPKAliases(t, root),
		[]string{primary, supplement},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}

	catalog := httptest.NewRecorder()
	handler.ServeHTTP(catalog, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/version/default/Android/patch/catalog.dat",
		nil,
	))
	reader, err := gzip.NewReader(catalog.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<b>,main_c/scenes/scene_Menu.dat,93936638,00000000,4,N,1,0,1\n",
		"<b>,live2d/live2d_nimue.dat,2206394C,00000000,5,N,1,0,1\n",
		"<a>,Live2D/nimue,nimue.model.json,,.bytes,live2d/live2d_nimue.dat\n",
		"<a>,Live2D/nimue/moc/nimue1024,texture_00,,.png,live2d/live2d_nimue.dat\n",
		"<a>,Container/card,card.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/card,card_base_category.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,skill_role_player.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,support_skill_role.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,burst_skill_role.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,skill_role_enemy.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,skill_player.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,support_skill.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,burst_skill.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/skill,skill_enemy.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/card,card_loveup_price.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/enemy,enemy.csv,,.txt,main_c/container.dat\n",
		"<a>,Container/icon,buff_icon.csv,,.txt,main_c/container.dat\n",
	} {
		if !strings.Contains(string(plain), want) {
			t.Fatalf("catalog is missing overlay row %q", want)
		}
	}
	if count := strings.Count(string(plain), "<a>,Container/card,card.csv,,.txt,main_c/container.dat\n"); count != 1 {
		t.Fatalf("catalog contains card.csv %d times, want exactly once", count)
	}

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/local/version/default/Android/patch/live2d/live2d_nimue.dat", want: "nimue"},
		{path: "/local/version/default/Android/patch/main_c/scenes/scene_Menu.dat", want: "menu"},
		{path: "/local/resources/patch/Android/patch/live2d/live2d_nimue.dat.v2206394C", want: "nimue"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://local"+test.path, nil))
		if response.Code != http.StatusOK || response.Body.String() != test.want {
			t.Fatalf("%s: status=%d body=%q", test.path, response.Code, response.Body.String())
		}
	}
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(
		http.MethodHead,
		"http://local/local/resources/patch/Android/patch/live2d/live2d_nimue.dat.v2206394C",
		nil,
	))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "5" {
		t.Fatalf("versioned HEAD: status=%d length=%q body=%q", head.Code, head.Header().Get("Content-Length"), head.Body.String())
	}
}

func TestCN602PushNoOpAndPreloadedCPKList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	push := httptest.NewRecorder()
	handler.ServeHTTP(push, httptest.NewRequest(
		http.MethodPost,
		"http://local//subcribe_push.php",
		strings.NewReader(`{"userid":1000001,"flag":1}`),
	))
	if push.Code != http.StatusOK || push.Body.String() != "{}" {
		t.Fatalf("push no-op: status=%d body=%q", push.Code, push.Body.String())
	}

	cpk := httptest.NewRecorder()
	handler.ServeHTTP(cpk, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/version/default/CPK/cpk_file.csv?v=790",
		nil,
	))
	const want = "# cpk_name,type,id,file_size_mb,download_type,size_bytes\nalpha.cpk,NONE,0,0.000004,ALL,4\n"
	if cpk.Code != http.StatusOK || cpk.Body.String() != want {
		t.Fatalf("CPK list: status=%d body=%q", cpk.Code, cpk.Body.String())
	}

	patchState := httptest.NewRecorder()
	handler.ServeHTTP(patchState, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/version/default/CPK/cpk_patch.txt?v=790",
		nil,
	))
	if patchState.Code != http.StatusOK || patchState.Body.String() != "alpha.cpk,1\n" {
		t.Fatalf("CPK patch state: status=%d body=%q", patchState.Code, patchState.Body.String())
	}
}
