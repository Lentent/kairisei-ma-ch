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
)

func customAnimationInventoryFixture() ([]byte, []byte) {
	var original, patched strings.Builder
	original.WriteString("#original header\r\n")
	patched.WriteString("#original header\r\n")
	index := 0
	for _, kind := range []string{"support", "disruption", "defense", "healing"} {
		for _, attribute := range []string{"fire", "ice", "wind", "light", "dark"} {
			row := make([]string, 33)
			row[0], row[1], row[3] = strconv.Itoa(11+index), "player_skill_buff", "pl_buff_b"
			row[8], row[9], row[20], row[21], row[22], row[23] = "ATK_UP_FIXED", "SELECT", "3", "ATK", "1000", "500"
			original.WriteString(strings.Join(row, ",") + "\r\n")
			row[3] += "_cp_" + kind + "_" + attribute
			ending := "\r\n"
			if index%2 == 0 {
				ending = "\n"
			}
			patched.WriteString(strings.Join(row, ",") + ending)
			index++
		}
	}
	return []byte(original.String()), []byte(patched.String())
}

func customInventoryEntry(name string, data []byte) map[string]any {
	return map[string]any{"path": name, "bytes": float64(len(data)), "sha256": resourceHash(data)}
}

func TestCustomCardInventoryAcceptsOnlyProvenAnimationBaseline(t *testing.T) {
	original, patched := customAnimationInventoryFixture()
	entry := customInventoryEntry(customRoleCSVPath, original)
	before := bytes.Clone(patched)
	if !customCardInventoryMatches(customRoleCSVPath, patched, entry) {
		t.Fatal("rejected presentation-only changes against their exact original digest")
	}
	if !bytes.Equal(patched, before) {
		t.Fatal("baseline check rewrote current animations")
	}
	if !customCardInventoryMatches(customRoleCSVPath, original, entry) || !customCardInventoryMatches(customRoleCSVPath, patched, customInventoryEntry(customRoleCSVPath, patched)) {
		t.Fatal("rejected a normally matched inventory")
	}
	for _, tc := range []struct {
		name, old, next string
	}{
		{"buff amount", ",500,", ",999,"},
		{"target scope", ",SELECT,", ",USER_ALL,"},
		{"effect function", "ATK_UP_FIXED", "ATK_BREAK_FIXED"},
		{"function ID", "11,player_skill", "91,player_skill"},
		{"2D animation", "player_skill_buff", "different_2d_effect"},
		{"unknown animation kind", "_cp_support_fire", "_cp_attack_fire"},
		{"unknown animation attribute", "_cp_support_fire", "_cp_support_other"},
		{"different base animation", "pl_buff_b_cp_support_fire", "pl_buff_a_cp_support_fire"},
		{"modified header", "#original header", "#changed header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := []byte(strings.Replace(string(patched), tc.old, tc.next, 1))
			if bytes.Equal(changed, patched) {
				t.Fatal("test did not modify the fixture")
			}
			if customCardInventoryMatches(customRoleCSVPath, changed, entry) {
				t.Fatal("accepted an unproven resource change")
			}
		})
	}
	if customCardInventoryMatches(customRoleCSVPath, append(bytes.Clone(patched), []byte("#new row\n")...), entry) {
		t.Fatal("accepted added rows")
	}
	if customCardInventoryMatches(customRoleCSVPath, bytes.TrimSuffix(patched, []byte("\r\n")), entry) {
		t.Fatal("accepted a truncated original file")
	}
	if customCardInventoryMatches(customSkillCSVPath, patched, entry) {
		t.Fatal("applied the exception to an unrelated resource")
	}
	if customCardInventoryMatches(customRoleCSVPath, bytes.ReplaceAll(original, []byte("\r\n"), []byte("\n")), entry) {
		t.Fatal("accepted newline conversion without an animation patch")
	}
	wrong := customInventoryEntry(customRoleCSVPath, original)
	wrong["bytes"] = float64(len(original) + 1)
	if customCardInventoryMatches(customRoleCSVPath, patched, wrong) {
		t.Fatal("accepted a different original size")
	}
	wrong["bytes"], wrong["sha256"] = float64(len(original)), resourceHash([]byte("unrelated"))
	if customCardInventoryMatches(customRoleCSVPath, patched, wrong) {
		t.Fatal("accepted a different original digest")
	}
}

func TestCustomCardAnimationBaselinePackageKeepsCurrentFileAndUpdatesInventory(t *testing.T) {
	baseline, current := customAnimationInventoryFixture()
	root := t.TempDir()
	versionPath := "resources/patch/version.dat"
	containerPath := "resources/patch/main_c/container.dat"
	version := []byte("<bundle_ver>,main_c/container.dat,0,00000000\n")
	key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
	for i := range version {
		version[i] += key[i%8]
	}
	assets := map[string]any{
		"source":  map[string]any{},
		"bundles": []any{map[string]any{"bundle": "main_c/container.dat", "scrambled": false}},
	}
	assetBytes, err := resourceJSON(assets)
	if err != nil {
		t.Fatal(err)
	}
	container := []byte("existing client container")
	original := map[string][]byte{
		customRoleCSVPath: current, containerPath: container, "asset-map.json": assetBytes,
	}
	inventoried := map[string][]byte{
		customRoleCSVPath: baseline, containerPath: container, "asset-map.json": assetBytes, versionPath: version,
	}
	entries := []any{}
	total := 0
	for name, data := range inventoried {
		entries = append(entries, customInventoryEntry(name, data))
		total += len(data)
	}
	// Existing honor and banner metadata must survive the card export untouched.
	unrelated := customInventoryEntry("resources/banners/custom.png", []byte("banner"))
	entries = append(entries, unrelated)
	total += len("banner")
	manifest := map[string]any{
		"files":             entries,
		"summary":           map[string]any{"bytes": float64(total), "bytes_to_copy": float64(total), "file_count": float64(len(entries)), "files_to_copy": float64(len(entries))},
		"operator_metadata": map[string]any{"honor_revision": float64(8)},
	}
	original["resource-set.json"], err = resourceJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{customRoleCSVPath: current, versionPath: version, "resource-set.json": original["resource-set.json"]} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	generated := append(bytes.Clone(current), []byte("#new custom card effect\r\n")...)
	files := map[string][]byte{customRoleCSVPath: generated, containerPath: container}
	archive, err := packageCustomCardResources(customCardDraft{}, root, files, original, manifest, assets)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(contents["resource-set/"+customRoleCSVPath], generated) {
		t.Fatal("export reverted the current animation playlists")
	}
	var output, config map[string]any
	if err := json.Unmarshal(contents["resource-set/resource-set.json"], &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents["configuration.json"], &config); err != nil {
		t.Fatal(err)
	}
	if config["base_sha256"].(map[string]any)[customRoleCSVPath] != resourceHash(current) {
		t.Fatal("apply precondition refers to the obsolete original instead of the current server CSV")
	}
	newTotal := 0
	for _, value := range output["files"].([]any) {
		entry := value.(map[string]any)
		name := entry["path"].(string)
		newTotal += int(entry["bytes"].(float64))
		if name == "resources/banners/custom.png" {
			if !reflect.DeepEqual(entry, unrelated) {
				t.Fatal("changed existing banner metadata")
			}
			continue
		}
		data := contents["resource-set/"+name]
		if entry["sha256"] != resourceHash(data) || int(entry["bytes"].(float64)) != len(data) {
			t.Fatal("generated resource has incorrect inventory")
		}
	}
	summary := output["summary"].(map[string]any)
	if int(summary["bytes"].(float64)) != newTotal || int(summary["bytes_to_copy"].(float64)) != newTotal {
		t.Fatal("summary omitted the already-present animation patch size difference")
	}
	if summary["file_count"] != float64(len(entries)) || summary["files_to_copy"] != float64(len(entries)) || !reflect.DeepEqual(output["operator_metadata"], manifest["operator_metadata"]) {
		t.Fatal("changed unrelated counts or existing metadata")
	}
	for name, want := range map[string][]byte{customRoleCSVPath: current, "resource-set.json": original["resource-set.json"]} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("export wrote to the live resource directory")
		}
	}
}

func TestCustomCardReportedServerAnimationBaseline(t *testing.T) {
	path := os.Getenv("CN602_CUSTOM_ROLE_ANIMATION_CSV")
	if path == "" {
		t.Skip("requires the reported server animation CSV")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"bytes": float64(5315672), "sha256": "59f9a5861e82ad5b8cfe8e1ce1d9e1f656e1e7fe133bdd3be5ebf618ad3d2a28"}
	if len(raw) != 5504384 || resourceHash(raw) != "db9498b587002183fcccf098192b44f0e0b8ead3b641afba50a23333386df619" {
		t.Fatal("reported CSV changed")
	}
	if !customCardInventoryMatches(customRoleCSVPath, raw, entry) {
		t.Fatal("rejected the audited Linux deployment baseline")
	}
}

func TestCustomCardsServerAnimationResourceExport(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_ANIMATION_RESOURCE_SET")
	if root == "" {
		t.Skip("requires a resource fixture with the reported server animation CSV")
	}
	a, _, _ := customGachaTestAPI(t)
	a.collectionResourceRoot = root
	s, err := a.customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.template(10001048)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardNextID(s, customCardDraft{})
	c.Name = "全体双攻验收"
	a.catalogByKey[adminCatalogKey(6, c.TemplateID)] = AdminCatalogEntry{ResourceState: "ready"}
	for _, skill := range c.Skills {
		skill[19] = "USER_ALL"
	}
	animationRows := 0
	for _, role := range c.Roles {
		if role[8] != "ATK_UP_FIXED" {
			t.Fatal("unexpected template buff")
		}
		role[22], role[23], role[24], role[25] = "1000", "500", "0", "0"
		if role[3] != "" && !strings.Contains(role[3], "_cp_support_") {
			t.Fatal("source did not use the server animation baseline")
		}
		if role[3] != "" {
			animationRows++
		}
	}
	if animationRows == 0 {
		t.Fatal("template has no animation to preserve")
	}
	draft := customCardDraft{Cards: []customCard{c}}
	if err := a.validateCustomCards(&draft, s); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(root, customRoleCSVPath))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := a.buildCustomCardPatch(draft, s)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	generated := contents["resource-set/"+customRoleCSVPath]
	if !bytes.HasPrefix(generated, current) {
		t.Fatal("export changed an existing server skill row")
	}
	_, skills, roles, _, err := materializeCustomCard(c, s)
	if err != nil {
		t.Fatal(err)
	}
	ids := customSkillIDs(c)
	client, err := readResourceBundle(contents["resource-set/resources/patch/main_c/container.dat"])
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][][]string{"skill_player.csv": skills, "skill_role_player.csv": roles}
	updates := map[string]func(string) (string, error){}
	for name, expected := range want {
		updates[name] = func(text string) (string, error) {
			rows, err := customCSVRows([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			found := [][]string{}
			for _, row := range rows {
				for _, id := range ids {
					if customRowInt(row, 0) == id {
						found = append(found, row)
						break
					}
				}
			}
			if !reflect.DeepEqual(found, expected) {
				t.Fatalf("client lost server animation or buff settings in %s", name)
			}
			return text, nil
		}
	}
	if _, err := replaceResourceTexts(client.nodes[0].data, updates); err != nil {
		t.Fatal(err)
	}
	var inventory map[string]any
	if err := json.Unmarshal(contents["resource-set/resource-set.json"], &inventory); err != nil {
		t.Fatal(err)
	}
	for _, value := range inventory["files"].([]any) {
		row := value.(map[string]any)
		if row["path"] == customRoleCSVPath && (row["sha256"] != resourceHash(generated) || row["bytes"] != float64(len(generated))) {
			t.Fatal("export did not refresh the stale animation inventory")
		}
	}
	after, err := os.ReadFile(filepath.Join(root, customRoleCSVPath))
	if err != nil || !bytes.Equal(after, current) {
		t.Fatal("export modified the running server's baseline CSV")
	}
	if output := os.Getenv("CN602_CUSTOM_ANIMATION_SAMPLE_ZIP"); output != "" {
		if err := os.WriteFile(output, archive, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
