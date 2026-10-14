package admin

import (
	"encoding/json"
	"sort"
)

// Receipts identify the owned custom ID. Its template describes how that ID
// was last built, and can differ from a legacy draft or an edited design.
// Keep receipts for cards outside this export so they remain editable later.
func mergeCustomCardReceipts(source json.RawMessage, edited []customCardReceipt) ([]customCardReceipt, error) {
	var previous struct {
		Cards []customCardReceipt `json:"admin_custom_cards"`
	}
	if err := json.Unmarshal(source, &previous); err != nil {
		return nil, err
	}
	byID := make(map[int]customCardReceipt, len(previous.Cards)+len(edited))
	for _, c := range previous.Cards {
		byID[c.ID] = c
	}
	for _, c := range edited {
		byID[c.ID] = c
	}
	result := make([]customCardReceipt, 0, len(byID))
	for _, c := range byID {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
