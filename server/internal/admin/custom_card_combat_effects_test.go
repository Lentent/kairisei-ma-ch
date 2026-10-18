package admin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/multiplayer"
)

func TestCustomCardCombatEffectsDraftAndExport(t *testing.T) {
	s := customUnitSources()
	card, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	card.ID, card.TurnSingleToAll = customCardFirstID, true
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{card}}, s); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	var restored customCard
	if err := json.Unmarshal(raw, &restored); err != nil || !restored.TurnSingleToAll {
		t.Fatalf("draft roundtrip lost the effect: %v", err)
	}
	// A custom effect must not introduce an enum the unmodified client cannot parse.
	_, enabledSkills, enabledRoles, _, err := materializeCustomCard(card, s)
	if err != nil {
		t.Fatal(err)
	}
	card.TurnSingleToAll = false
	_, disabledSkills, disabledRoles, _, err := materializeCustomCard(card, s)
	if err != nil || !reflect.DeepEqual(enabledSkills, disabledSkills) || !reflect.DeepEqual(enabledRoles, disabledRoles) {
		t.Fatalf("custom effect changed native client rows: %v", err)
	}
	root := t.TempDir()
	files, original := map[string][]byte{}, map[string][]byte{}
	if err := buildCustomCardCombatEffects(customCardDraft{Cards: []customCard{card}}, root, files, original); err != nil || len(files) != 0 {
		t.Fatalf("legacy cards unexpectedly require an extension: %v", err)
	}
	card.TurnSingleToAll = true
	if err := buildCustomCardCombatEffects(customCardDraft{Cards: []customCard{card}}, root, files, original); err != nil {
		t.Fatal(err)
	}
	effects, err := multiplayer.ParseCustomCardEffects(files[customCardEffectsPath])
	if err != nil || !effects.Cards[card.ID].TurnSingleToAll || len(original) != 0 {
		t.Fatalf("new export lost its effect: %+v, %v", effects, err)
	}
	// Applying a later export may disable this card without losing another card.
	effects.Cards[card.ID+1] = multiplayer.CustomCardCombatEffects{TurnSingleToAll: true}
	previous, _ := json.Marshal(effects)
	path := filepath.Join(root, filepath.FromSlash(customCardEffectsPath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, previous, 0600); err != nil {
		t.Fatal(err)
	}
	card.TurnSingleToAll = false
	if err := buildCustomCardCombatEffects(customCardDraft{Cards: []customCard{card}}, root, files, original); err != nil {
		t.Fatal(err)
	}
	effects, err = multiplayer.ParseCustomCardEffects(files[customCardEffectsPath])
	if err != nil || len(effects.Cards) != 1 || !effects.Cards[card.ID+1].TurnSingleToAll || !reflect.DeepEqual(original[customCardEffectsPath], previous) {
		t.Fatalf("re-export lost another effect or its baseline: %+v, %v", effects, err)
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(after, previous) {
		t.Fatal("export changed the running resource set")
	}
}

func TestCustomCardSingleToAllResourceExport(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires game resources")
	}
	a := &API{collectionResourceRoot: root}
	s, err := a.customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.template(10000010)
	if err != nil {
		t.Fatal(err)
	}
	card.ID = customCardNextID(s, customCardDraft{})
	for i, row := range card.Roles {
		card.Roles[i] = customSingleToAllRole(customRowInt(row, 0))
		card.RoleSources[i] = customRoleSource{}
	}
	archive, err := a.buildCustomCardPatch(customCardDraft{Cards: []customCard{card}}, s)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, file := range z.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	serverRoles, err := customCSVRows(files["resource-set/"+customRoleCSVPath])
	if err != nil {
		t.Fatal(err)
	}
	ids := customSkillIDs(card)
	functions := map[string]bool{}
	for _, row := range card.Roles {
		functions[strconv.Itoa(ids[customRowInt(row, 0)])] = true
	}
	seen := map[string]bool{}
	for _, row := range serverRoles {
		if functions[row[0]] {
			if !isCustomSpecialRole(row) {
				t.Fatal("standalone export retained an inherited server effect")
			}
			seen[row[0]] = true
		}
	}
	if len(seen) != len(functions) {
		t.Fatal("server archive lost a standalone effect group")
	}
	client, err := readResourceBundle(files["resource-set/resources/patch/main_c/container.dat"])
	if err != nil {
		t.Fatal(err)
	}
	_, err = replaceResourceTexts(client.nodes[0].data, map[string]func(string) (string, error){
		"skill_role_player.csv": func(text string) (string, error) {
			rows, err := customCSVRows([]byte(text))
			if err != nil {
				return "", err
			}
			found := map[string]bool{}
			for _, row := range rows {
				if functions[row[0]] {
					found[row[0]] = true
					if row[8] != "ATK_OP_PIERCING" || row[20] != "0" || row[21] != "0" {
						t.Fatal("client archive contains a non-native or active placeholder")
					}
				}
			}
			if len(found) != len(functions) {
				t.Fatal("client archive lost a standalone effect group")
			}
			if strings.Contains(text, multiplayer.CombatFunctionTurnSingleToAll) {
				t.Fatal("client archive contains a server-only effect enum")
			}
			return text, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files []struct {
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(files["resource-set/resource-set.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	registered := false
	for _, file := range manifest.Files {
		if file.Path == customRoleCSVPath {
			raw := files["resource-set/"+customRoleCSVPath]
			registered = file.Bytes == len(raw) && file.SHA256 == resourceHash(raw)
		}
	}
	if !registered {
		t.Fatal("custom effects were not registered with their size and hash")
	}
}

func TestCustomSpecialEffectCanBeTheOnlyRole(t *testing.T) {
	s := customUnitSources()
	for _, skill := range s.Skills {
		skill[4], skill[5], skill[6] = "1", "1", "100"
	}
	card, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	card.ID = customCardFirstID
	for i, role := range card.Roles {
		card.Roles[i] = customSingleToAllRole(customRowInt(role, 0))
		card.RoleSources[i] = customRoleSource{}
	}
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{card}}, s); err != nil {
		t.Fatal(err)
	}
	_, skills, roles, _, err := materializeCustomCard(card, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		if skill[4] != "0" || skill[5] != "0" || skill[6] != "0" {
			t.Fatal("standalone skill retained the template's displayed power")
		}
	}
	client := customClientRoleRows(roles)
	for i, role := range roles {
		if !isCustomSpecialRole(role) || client[i][8] != "ATK_OP_PIERCING" || client[i][20] != "0" || !reflect.DeepEqual(role[1:8], client[i][1:8]) {
			t.Fatal("special effect lost its server identity or native presentation")
		}
	}
	for _, tc := range []struct {
		column int
		value  string
	}{{9, "ENEMY_ALL"}, {20, "100"}, {0, "999"}} {
		bad := card
		bad.Roles = cloneCustomRows(card.Roles)
		bad.Roles[0][tc.column] = tc.value
		if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{bad}}, s); err == nil {
			t.Fatalf("accepted a modified special effect column %d", tc.column)
		}
	}
	// Restoring another ordinary effect makes this a normal combined skill.
	card.Roles = append(card.Roles, append([]string(nil), s.Roles[0]...))
	card.RoleSources = append(card.RoleSources, customRoleSource{CardID: 101})
	_, skills, _, _, err = materializeCustomCard(card, s)
	if err != nil || skills[0][5] != "1" {
		t.Fatalf("combined skill lost its ordinary power display: %v", err)
	}
}
