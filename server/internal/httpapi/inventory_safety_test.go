package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

func TestCardSellChecksEachQuantityBeforeMerging(t *testing.T) {
	for _, input := range []struct {
		name, cards string
		sold        int
	}{
		{"negative cancellation", `[{"cardid":9,"num":-1},{"0":9,"1":2}]`, 0},
		{"overflow wraps positive", fmt.Sprintf(`[{"cardid":9,"num":%d},{"0":9,"1":%d},9,9,9]`, math.MaxInt, math.MaxInt), 0},
		{"ordinary mixed entries", `[9,{"0":9,"1":1},{"cardid":9,"num":1}]`, 3},
	} {
		t.Run(input.name, func(t *testing.T) {
			uses, err := decodeStackUses(json.RawMessage(input.cards))
			if input.sold == 0 {
				if err == nil {
					t.Fatal("invalid individual quantity accepted")
				}
			} else if err != nil || len(uses) != 1 || uses[0].CardID != 9 || uses[0].Num != input.sold {
				t.Fatalf("merged selection = %+v, err=%v", uses, err)
			}
		})
	}
}
