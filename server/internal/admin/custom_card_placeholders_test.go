package admin

import (
	"reflect"
	"strings"
	"testing"
)

func TestCustomDescriptionSlotsUseNativeDisplayFields(t *testing.T) {
	for _, tc := range []struct {
		code    string
		slot    int
		params  []int
		formula string
	}{
		{"ATTACK_AA", 1, []int{0, 1}, "不包含参照能力"},
		{"ATTACK_AA", 4, []int{6}, "÷10"},
		{"ATK_UP_FIXED", 1, []int{2, 3, 4}, "÷1000"},
		{"ATK_UP_BY_SELF_PARAM", 2, []int{5}, "×等级"},
		{"REFLECTION", 1, []int{1, 2}, "÷100"},
		{"BLESS", 7, []int{3, 4}, "不会自动"},
		{"POISON", 2, []int{3, 4}, "不包含参照能力"},
		{"DEAL_BONUS", 0, []int{0}, "第0个位置"},
	} {
		slot := customDescriptionSlots(tc.code)[tc.slot]
		if slot.Name == "" || !reflect.DeepEqual(slot.Parameters, tc.params) || !strings.Contains(slot.Formula, tc.formula) {
			t.Fatalf("%s slot %d: %+v", tc.code, tc.slot, slot)
		}
	}
	if len(customDescriptionSlots("UNCONFIRMED")) != 0 {
		t.Fatal("unknown display rules must not be guessed")
	}
	if _, ok := customDescriptionSlots("DEAL_BONUS")[1]; ok {
		t.Fatal("draw count must not be mislabeled as slot1")
	}
}
