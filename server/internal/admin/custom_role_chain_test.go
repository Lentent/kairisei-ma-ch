package admin

import "testing"

func TestCustomRoleChainClientExport(t *testing.T) {
	row := append(customSingleToAllRole(1900000001), "3")
	exported := customClientRoleRows([][]string{row})
	if len(exported[0]) != 33 || exported[0][8] != "ATK_OP_PIERCING" {
		t.Fatalf("client export: %v", exported)
	}
	if len(row) != 34 || row[33] != "3" {
		t.Fatal("server prerequisite mutated")
	}
}
