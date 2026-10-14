package admin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

func TestCustomCardPublishedIDAcceptsLegacyTemplateMismatch(t *testing.T) {
	s, d := customEvolutionFixture(t)
	old := d.Cards[0]
	row, _, _, _, err := materializeCustomCard(old, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Cards[old.ID], s.Applied[old.ID] = row, old.TemplateID
	// The legacy saved design has another native template, just like the
	// screenshot: the known published ID must still be editable.
	updated, err := s.template(102)
	if err != nil {
		t.Fatal(err)
	}
	updated.ID, updated.Name = old.ID, "原卡修改"
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}, "6:102": {ResourceState: "ready"}}}
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{updated}}, s); err != nil {
		t.Fatal("rejected owned custom ID", err)
	}
	delete(s.Applied, updated.ID)
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{updated}}, s); err == nil {
		t.Fatal("accepted unowned occupied ID")
	}
}

func TestCustomCardReceiptsKeepOmittedPublishedCards(t *testing.T) {
	old := []customCardReceipt{{customCardFirstID, 101, true}, {customCardFirstID + 1, 101, true}}
	source, _ := json.Marshal(map[string]any{"admin_custom_cards": old})
	got, err := mergeCustomCardReceipts(source, []customCardReceipt{{customCardFirstID, 102, true}})
	if err != nil || len(got) != 2 || got[0].TemplateID != 102 || got[1] != old[1] {
		t.Fatal("lost ID ownership", got, err)
	}
	if again, err := mergeCustomCardReceipts(source, nil); err != nil || !reflect.DeepEqual(again, old) {
		t.Fatal("empty export dropped receipts", err)
	}
}

func TestCustomCardLegacyPublishedUpdateCompleteResourceExport(t *testing.T) {
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
	if s.Applied[customCardFirstID] == 0 {
		t.Skip("requires existing card 98000001")
	}
	updated, err := s.template(10108001)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TemplateID == s.Applied[customCardFirstID] {
		t.Skip("requires baseline receipt mismatch")
	}
	updated.ID, updated.Name, updated.Attribute = customCardFirstID, "复制型库巴姬兼容验收", "LIGHT"
	var art bytes.Buffer
	if err := png.Encode(&art, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	updated.Artwork = art.Bytes()
	// Validate a full editor save without removing any of the existing IDs.
	d := customCardDraft{Cards: []customCard{updated}}
	for id, templateID := range s.Applied {
		if id == updated.ID {
			continue
		}
		c, err := s.template(templateID)
		if err != nil {
			t.Fatal(err)
		}
		c.ID = id
		d.Cards = append(d.Cards, c)
		a.catalogByKey[adminCatalogKey(6, templateID)] = AdminCatalogEntry{ResourceState: "ready"}
	}
	a.catalogByKey[adminCatalogKey(6, updated.TemplateID)] = AdminCatalogEntry{ResourceState: "ready"}
	if err := a.validateCustomCards(&d, s); err != nil {
		t.Fatal("legacy draft could not be saved", err)
	}
	before, _ := json.Marshal(s.Master)
	// A partial export additionally proves other published cards survive byte
	// for byte and retain their receipts. Everything remains in a temporary ZIP.
	archive, err := a.buildCustomCardPatch(customCardDraft{Cards: []customCard{updated}}, s)
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := json.Marshal(s.Master); !bytes.Equal(before, after) {
		t.Fatal("export modified source in memory")
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
	var master masterdata.CardRuntimeMaster
	if err := json.Unmarshal(files["resource-set/"+customCardMasterPath], &master); err != nil {
		t.Fatal(err)
	}
	if len(master.CardTemplates) != len(s.Master.CardTemplates) {
		t.Fatal("update changed card count")
	}
	oldCards := map[int]gamestate.Card{}
	for _, c := range s.Master.CardTemplates {
		oldCards[c.CardID] = c
	}
	for _, c := range master.CardTemplates {
		if c.CardID == updated.ID {
			if c.Name != updated.Name {
				t.Fatal("update did not reach original ID")
			}
		} else if !reflect.DeepEqual(c, oldCards[c.CardID]) {
			t.Fatalf("changed unrelated card %d", c.CardID)
		}
	}
	rows, err := customCSVRows(files["resource-set/"+customCardCSVPath])
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		id := customRowInt(row, 0)
		if id != updated.ID && !reflect.DeepEqual(row, s.Cards[id]) {
			t.Fatalf("changed unrelated client row %d", id)
		}
	}
	var source struct {
		Cards []customCardReceipt `json:"admin_custom_cards"`
	}
	if err := json.Unmarshal(master.Source, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Cards) != len(s.Applied) {
		t.Fatal("lost published receipts")
	}
	for _, c := range source.Cards {
		if c.ID == updated.ID {
			if c.TemplateID != updated.TemplateID {
				t.Fatal("receipt did not follow update")
			}
		} else if c.TemplateID != s.Applied[c.ID] {
			t.Fatal("lost other template receipt")
		}
	}
	// Apply the exported master to disposable player inventories, including
	// a warehouse copy. Existing ownership and mutable values must survive.
	owned := oldCards[updated.ID]
	owned.UniqueID, owned.Level, owned.Experience = 1234, 10, 0
	owned.Fame, owned.Love, owned.IsLock = 17, 400, 1
	warehouse := owned
	warehouse.UniqueID = 5678
	state := gamestate.State{
		CardProgressionConfigVersion: s.Master.CardProgressionConfigVersion,
		Cards:                        []gamestate.Card{owned}, ContainerCards: []gamestate.Card{warehouse},
	}
	if _, err := masterdata.ApplyCardRuntimeMaster(&state, master); err != nil {
		t.Fatal("existing save cannot load updated card", err)
	}
	if len(state.Cards) != 1 || len(state.ContainerCards) != 1 {
		t.Fatal("inventory copies disappeared")
	}
	for i, c := range []gamestate.Card{state.Cards[0], state.ContainerCards[0]} {
		wantID := []int64{1234, 5678}[i]
		if c.CardID != updated.ID || c.UniqueID != wantID || c.Fame != owned.Fame || c.Love != owned.Love || c.IsLock != owned.IsLock || c.Level != owned.Level {
			t.Fatal("update changed ownership or mutable card values", c)
		}
	}
}
