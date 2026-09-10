package cnbootstrap

import (
	"encoding/json"
	"testing"
)

func TestFilterCNGachaPublicationKeepsBaseAndEnabledManagedPools(t *testing.T) {
	method := map[string]json.RawMessage{
		"gacha_category_list": json.RawMessage(`[
			{"category_num":1,"pictid":1},
			{"category_num":2,"pictid":2},
			{"category_num":3,"pictid":3}
		]`),
		"gacha_list": json.RawMessage(`[
			{"gachaid":1,"groupid":1,"category_num":1},
			{"gachaid":100,"groupid":100,"category_num":2},
			{"gachaid":200,"groupid":200,"category_num":3}
		]`),
	}
	active := map[int]struct{}{200: {}}
	managed := map[int]struct{}{100: {}, 200: {}}

	if err := filterCNGachaPublication(method, active, managed); err != nil {
		t.Fatal(err)
	}
	var gachas []struct {
		GroupID int `json:"groupid"`
	}
	if err := json.Unmarshal(method["gacha_list"], &gachas); err != nil {
		t.Fatal(err)
	}
	if len(gachas) != 2 || gachas[0].GroupID != 1 || gachas[1].GroupID != 200 {
		t.Fatalf("filtered gachas = %+v", gachas)
	}
	var categories []struct {
		CategoryNum int `json:"category_num"`
	}
	if err := json.Unmarshal(method["gacha_category_list"], &categories); err != nil {
		t.Fatal(err)
	}
	if len(categories) != 2 || categories[0].CategoryNum != 1 || categories[1].CategoryNum != 3 {
		t.Fatalf("filtered categories = %+v", categories)
	}
}
