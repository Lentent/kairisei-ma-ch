package admin

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestCustomCardLegacyPlaylistRequiresAllAvatarResources(t *testing.T) {
	const alias = "pl_buff_buff_b_cp_support_wind"
	for _, scenario := range []string{"missing", "partial", "complete", "no-original", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			assets := map[string]any{"assets": []any{}}
			add := func(avatar, name string) {
				assets["assets"] = append(assets["assets"].([]any), map[string]any{
					"object_type": "TextAsset", "directory": "text/action/" + avatar + "/playlist", "name": name,
				})
			}
			if scenario != "no-original" {
				add("avt_100", "pl_buff_buff_b")
				add("avt_200", "pl_buff_buff_b")
			}
			if scenario == "partial" || scenario == "complete" {
				add("avt_100", alias)
			}
			if scenario == "complete" {
				add("avt_200", alias)
			}
			roles := [][]string{{"1900000000", "player_skill_buff", "", alias, "hit", "TARGET", "", "", "ATK_UP_FIXED", "USER_ALL", "3", "ATK", "240000"},
				{"1900000000", "player_skill_buff", "", alias, "hit", "TARGET", "", "", "DEF_UP_FIXED", "SELECT", "3", "MDEF", "240000"}}
			if scenario == "unknown" {
				roles[0][3], roles[1][3] = "my_custom_action", "my_custom_action"
			}
			before := cloneCustomRows(roles)
			err := resolveCustomCardPlaylists(roles, assets)
			if scenario == "no-original" {
				if err == nil {
					t.Fatal("accepted a missing original action")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "pl_buff_buff_b"
			if scenario == "complete" {
				want = alias
			} else if scenario == "unknown" {
				want = "my_custom_action"
			}
			for i, role := range roles {
				if role[3] != want {
					t.Fatalf("playlist = %s, want %s", role[3], want)
				}
				before[i][3] = want
			}
			if !reflect.DeepEqual(roles, before) {
				t.Fatal("playlist repair changed skill effects, targets or other direction fields")
			}
		})
	}
}

func TestCustomCardLegacyPlaylistCompleteResourceExport(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires complete game resources")
	}
	a, _, _ := customGachaTestAPI(t)
	a.collectionResourceRoot = root
	s, err := a.customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	// The incident template's two skills acquired server-only aliases.
	// Emulate that baseline in memory without changing the source resources.
	for _, role := range s.Roles {
		if customRowInt(role, 0) == 14400121 || customRowInt(role, 0) == 14400122 {
			if role[3] != "" {
				role[3] = "pl_buff_buff_b_cp_support_wind"
			}
		}
	}
	c, err := s.template(10001048)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardNextID(s, customCardDraft{})
	before := cloneCustomRows(c.Roles)
	archive, err := a.buildCustomCardPatch(customCardDraft{Cards: []customCard{c}}, s)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	ids := customSkillIDs(c)
	selected := map[string]bool{}
	for _, id := range ids {
		selected[strconv.Itoa(id)] = true
	}
	check := func(raw []byte) [][]string {
		rows, err := customCSVRows(raw)
		if err != nil {
			t.Fatal(err)
		}
		var got [][]string
		for _, row := range rows {
			if selected[row[0]] {
				if row[3] != "pl_buff_buff_b" {
					t.Fatalf("exported missing playlist: %s", row[3])
				}
				got = append(got, row)
			}
		}
		if len(got) != len(c.Roles) {
			t.Fatal("export lost effects")
		}
		return got
	}
	server := check(files["resource-set/"+customRoleCSVPath])
	bundle, err := readResourceBundle(files["resource-set/resources/patch/main_c/container.dat"])
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	_, err = replaceResourceTexts(bundle.nodes[0].data, map[string]func(string) (string, error){
		"skill_role_player.csv": func(text string) (string, error) {
			seen = true
			if !reflect.DeepEqual(check([]byte(text)), server) {
				t.Fatal("client/server repair differs")
			}
			return text, nil
		},
	})
	if err != nil || !seen {
		t.Fatalf("missing client role table: %v", err)
	}
	if !reflect.DeepEqual(c.Roles, before) {
		t.Fatal("export mutated the saved design")
	}
}
