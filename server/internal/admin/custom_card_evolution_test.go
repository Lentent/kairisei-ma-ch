package admin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strconv"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestCustomEvolutionRetiredClosureDoesNotBlockRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := game.EvolutionPath{FromCardID: customCardFirstID, ToCardID: customCardFirstID + 1}
	if _, err := o.writeDocument(evolutionPolicyKey, 0, evolutionPolicyDocument{Closed: []game.EvolutionPath{path}}); err != nil {
		t.Fatal(err)
	}
	o.evolutionEdges[path] = 0
	if err := o.loadEvolutionRestrictions(); err != nil || len(o.evolutionClosed) != 1 {
		t.Fatal("active route closure lost", err)
	}
	delete(o.evolutionEdges, path)
	if err := o.loadEvolutionRestrictions(); err != nil || len(o.evolutionClosed) != 0 || o.evolutionRevision != 1 {
		t.Fatal("retired closure blocks restart", err)
	}
	// Invalid native paths remain errors, as do newly submitted unknown paths.
	if err := o.validateEvolutionPaths([]game.EvolutionPath{path}); err == nil {
		t.Fatal("mutation accepted absent route")
	}
	if _, err := o.writeDocument(evolutionPolicyKey, 1, evolutionPolicyDocument{Closed: []game.EvolutionPath{{FromCardID: 10, ToCardID: 99}}}); err != nil {
		t.Fatal(err)
	}
	if err := o.loadEvolutionRestrictions(); err == nil {
		t.Fatal("silently ignored invalid native path")
	}
}

func customEvolutionFixture(t *testing.T) (customCardSources, customCardDraft) {
	t.Helper()
	s := customUnitSources()
	s.Cards[101][7] = "ULTRARARE"
	s.Cards[102] = append([]string(nil), s.Cards[101]...)
	s.Cards[102][0], s.Cards[102][7] = "102", "MILLIONRARE"
	base := s.Master.CardTemplates[0]
	base.RarityRank = 5
	s.Master.CardTemplates[0] = base
	base.CardID, base.RarityRank, base.LevelMax = 102, 6, 60
	s.Master.CardTemplates = append(s.Master.CardTemplates, base)
	s.Master.DeckRankPolicy.Cards[102] = s.Master.DeckRankPolicy.Cards[101]
	s.Master.EvolutionTransitions = []gamestate.EvolutionTransition{{FromCardID: 101, ToCardID: 102, Gold: 30000, Materials: []gamestate.EvolutionMaterial{{CardID: 201, Num: 2, Fame: 1}}}}
	s.EvolutionRows = [][]string{{"101", "NORMAL", "", "30000", "102", "201", "1", "1", "201", "1", "1", ""}}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	target, err := s.template(102)
	if err != nil {
		t.Fatal(err)
	}
	target.ID = customCardFirstID + 1
	c.Evolution = &customCardEvolution{target.ID, 101, 102}
	return s, customCardDraft{Cards: []customCard{c, target}}
}

func TestCustomCardEvolutionValidationAndReplacement(t *testing.T) {
	s, d := customEvolutionFixture(t)
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}, "6:102": {ResourceState: "ready"}}}
	if err := a.validateCustomCards(&d, s); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(d)
	var roundTrip customCardDraft
	if err := json.Unmarshal(raw, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip.Cards[0].Evolution, d.Cards[0].Evolution) {
		t.Fatal("saved evolution lost", err)
	}
	for _, kind := range []string{"missing", "self", "rarity", "job", "recipe", "divergent", "merge", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			var bad customCardDraft
			_ = json.Unmarshal(raw, &bad)
			ss := s
			switch kind {
			case "missing":
				bad.Cards[0].Evolution.TargetID += 99
			case "self":
				bad.Cards[0].Evolution.TargetID = bad.Cards[0].ID
			case "rarity":
				rank := 5
				bad.Cards[1].RarityRank = &rank
			case "job":
				bad.Cards[1].ArthurType = 2
			case "recipe":
				bad.Cards[0].Evolution.RecipeToID++
			case "divergent":
				ss.Master.EvolutionTransitions = append([]gamestate.EvolutionTransition(nil), s.Master.EvolutionTransitions...)
				ss.Master.EvolutionTransitions[0].Type = 1
			case "merge":
				c := bad.Cards[0]
				c.ID += 2
				bad.Cards = append(bad.Cards, c)
			case "cycle":
				bad.Cards[1].Evolution = &customCardEvolution{bad.Cards[0].ID, 101, 102}
			}
			if err := validateCustomCardEvolutions(bad, ss); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
	master := s.Master
	master.CardTemplates = append([]gamestate.Card(nil), master.CardTemplates...)
	cards := map[string][][]string{}
	for _, c := range d.Cards {
		row, _, _, card, err := materializeCustomCard(c, s)
		if err != nil {
			t.Fatal(err)
		}
		cards[strconv.Itoa(c.ID)] = [][]string{row}
		master.CardTemplates = append(master.CardTemplates, card)
	}
	updates, err := materializeCustomEvolutions(d, s, &master, cards)
	if err != nil {
		t.Fatal(err)
	}
	row := updates[strconv.Itoa(customCardFirstID)][0]
	if row[4] != strconv.Itoa(customCardFirstID+1) || row[5] != "201" || row[8] != "201" || s.EvolutionRows[0][0] != "101" {
		t.Fatal("client recipe slots changed", row)
	}
	tr := master.EvolutionTransitions[1]
	if tr.FromCardID != customCardFirstID || tr.ToCardID != customCardFirstID+1 || !reflect.DeepEqual(tr.Materials, s.Master.EvolutionTransitions[0].Materials) {
		t.Fatal("runtime recipe changed", tr)
	}
	if cards[strconv.Itoa(customCardFirstID+1)][0][2] != strconv.Itoa(customCardFirstID) || master.CardTemplates[3].SameCardID != customCardFirstID {
		t.Fatal("evolution forms do not share family")
	}
	// Re-export is idempotent and clearing restores independent families.
	if _, err = materializeCustomEvolutions(d, s, &master, cards); err != nil || len(master.EvolutionTransitions) != 2 {
		t.Fatal("duplicate transition", err)
	}
	d.Cards[0].Evolution = nil
	updates, err = materializeCustomEvolutions(d, s, &master, cards)
	if err != nil || len(updates[strconv.Itoa(customCardFirstID)]) != 0 || len(master.EvolutionTransitions) != 1 || master.CardTemplates[3].SameCardID != customCardFirstID+1 {
		t.Fatal("cleared route remains", err)
	}
}

func TestCustomCardEvolutionCompleteResourceExport(t *testing.T) {
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
	var recipe customEvolutionRecipe
	for _, r := range customEvolutionRecipes(s) {
		if r.FromRank == 5 && r.ToRank == 6 {
			if _, err := s.template(r.FromID); err != nil {
				continue
			}
			if _, err := s.template(r.ToID); err != nil {
				continue
			}
			recipe = r
			break
		}
	}
	if recipe.FromID == 0 {
		t.Fatal("no 5 -> 6 native recipe")
	}
	from, _ := s.template(recipe.FromID)
	to, _ := s.template(recipe.ToID)
	from.ID = customCardNextID(s, customCardDraft{})
	to.ID = customCardNextID(s, customCardDraft{Cards: []customCard{from}})
	to.ArthurType = from.ArthurType
	from.Evolution = &customCardEvolution{to.ID, recipe.FromID, recipe.ToID}
	archive, err := a.buildCustomCardPatch(customCardDraft{Cards: []customCard{from, to}}, s)
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
	var master struct {
		EvolutionTransitions []gamestate.EvolutionTransition `json:"evolution_transitions"`
		Cards                []gamestate.Card                `json:"card_templates"`
	}
	if err := json.Unmarshal(files["resource-set/"+customCardMasterPath], &master); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tr := range master.EvolutionTransitions {
		if tr.FromCardID == from.ID {
			count++
			if tr.ToCardID != to.ID || tr.Gold != recipe.Gold || !reflect.DeepEqual(tr.Materials, recipe.Materials) {
				t.Fatal("wrong server recipe", tr)
			}
		}
	}
	if count != 1 {
		t.Fatalf("server routes = %d", count)
	}
	rows, err := customCSVRows(files["resource-set/"+customEvolutionCSVPath])
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, row := range rows {
		if customRowInt(row, 0) == from.ID {
			count++
			if customRowInt(row, 4) != to.ID || row[1] != "NORMAL" {
				t.Fatal("wrong client recipe", row)
			}
		}
	}
	if count != 1 {
		t.Fatalf("client routes = %d", count)
	}
	bundle, err := readResourceBundle(files["resource-set/resources/patch/main_c/container.dat"])
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	_, err = replaceResourceTexts(bundle.nodes[0].data, map[string]func(string) (string, error){"card_evolution.csv": func(text string) (string, error) {
		seen = true
		client, err := customCSVRows([]byte(text))
		if err != nil || !reflect.DeepEqual(client, rows) {
			t.Fatal("client/server evolution CSV differ", err)
		}
		return text, nil
	}})
	if err != nil || !seen {
		t.Fatal("client table missing", err)
	}
}
