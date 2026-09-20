package testfixture

import (
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/masterdata"
)

// Opt-in production-data reproduction of high balances and large inventories.
func AttachProbeCardCatalog(t *testing.T, storage *accountstore.Database, cards masterdata.CardRuntimeMaster) {
	t.Helper()
	state, err := storage.CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	state.CardTemplates = cards.CardTemplates
	storage.SetCatalog(state)
}
