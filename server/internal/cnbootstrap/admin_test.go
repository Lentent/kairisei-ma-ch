package cnbootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestCNAdminImagesRetryBeforeTextFallback(t *testing.T) {
	html := string(cnAdminHTML) + string(cnAdminJS)
	if !strings.Contains(html, `onerror="retryImage(this)"`) || !strings.Contains(html, "attempt<2") {
		t.Fatal("Admin image markup does not retry transient failures")
	}
	if strings.Contains(html, `onerror="this.remove()"`) {
		t.Fatal("Admin image markup still permanently removes images after one failure")
	}
}

func TestCNAdminMissingBossImageUsesTextFallback(t *testing.T) {
	root := t.TempDir()
	url := "/assets/boss/60600204.webp"
	if image, err := cnAdminBossImageURL(root, url); err != nil || image != "" {
		t.Fatalf("missing decorative image blocks the server: %q %v", image, err)
	}
	if err := os.Mkdir(filepath.Join(root, "boss"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "boss", "60600204.webp"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	if image, err := cnAdminBossImageURL(root, url); err != nil || image != url {
		t.Fatalf("existing image was hidden: %q %v", image, err)
	}
	if _, err := cnAdminBossImageURL(root, "/assets/../../outside.webp"); err == nil {
		t.Fatal("unsafe image path accepted as a missing thumbnail")
	}
}

func TestCNAdminLoadsViewsOnDemandAndBoundsBossRendering(t *testing.T) {
	html := string(cnAdminHTML) + string(cnAdminJS)
	for _, contract := range []string{
		`async function loadView(name,force=false)`,
		`if(name==='dashboard'){const status=await api('/api/status')`,
		`else if(name==='bosses'){await loadBossPublication(state.bossCatalog)}`,
		`const visible=rows.slice(state.bossPage*120,(state.bossPage+1)*120)`,
		`data-kind="material">素材副本`,
		`最多 ${g.max_segments||1} 波`,
	} {
		if !strings.Contains(html, contract) {
			t.Fatalf("Admin UI is missing performance/content contract %q", contract)
		}
	}
	if strings.Contains(html, `async function loadAll`) {
		t.Fatal("Admin UI restored the eager all-endpoint loader")
	}
}

func TestCNAdminAccountListDoesNotDecodeSnapshots(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	identity, err := accounts.resolveLogin("00000000-0000-4000-8000-00000000a001")
	if err != nil {
		t.Fatal(err)
	}
	database, err := accounts.storage.open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE cn_account_snapshot SET payload_json = ? WHERE user_id = ?`,
		[]byte("not-json"),
		identity.UserID,
	); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	admin := &cnAdmin{accounts: accounts}
	items, err := admin.loadAccounts()
	if err != nil {
		t.Fatalf("lightweight account list decoded a snapshot: %v", err)
	}
	found := false
	for _, item := range items {
		if item.UserID == identity.UserID {
			found = true
			if item.LoginUUID != identity.LoginUUID || item.Revision != 1 {
				t.Fatalf("unexpected lightweight account row: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("account %d missing from lightweight list", identity.UserID)
	}
	count, err := admin.accountCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != len(items) {
		t.Fatalf("account count = %d, list length = %d", count, len(items))
	}
}

func TestApplyCNAdminCatalogAssetCoverage(t *testing.T) {
	root := t.TempDir()
	catalog := []cnAdminCatalogEntry{
		{Kind: "currency", RewardType: 4, Name: "gold"},
		{Kind: "card", RewardType: 6, RewardTypeID: 1, Name: "card one", ImageURL: "/assets/card/1.webp"},
		{Kind: "card", RewardType: 6, RewardTypeID: 2, Name: "card source gap", ImageURL: "/assets/card/2.webp"},
		{Kind: "item", RewardType: 8, RewardTypeID: 3, Name: "item", ImageURL: "/assets/item/3.webp"},
		{Kind: "material", RewardType: 13, RewardTypeID: 4, Name: "material", ImageURL: "/assets/card/4.webp"},
		{Kind: "sphere", RewardType: 15, RewardTypeID: 5, Name: "sphere", ImageURL: "/assets/sphere/5.webp"},
		{Kind: "buddy", RewardType: 19, RewardTypeID: 6, Name: "buddy", ImageURL: "/assets/buddy/6.webp"},
	}
	for _, relative := range []string{"card/1.webp", "item/3.webp", "card/4.webp", "sphere/5.webp", "buddy/6.webp"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("webp"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := cnAdminAssetManifest{
		SchemaVersion: 2, ClientProfile: "cn602-bootstrap", State: "PASS",
		Purpose: "loopback_admin_web_thumbnail_only",
		CatalogImageCoverage: map[string]cnAdminCatalogImageCoverage{
			"card":     {EntryCount: 2, ResolvedSourceCount: 1, SourceGapCount: 1, SourceGapIDs: []int{2}},
			"item":     {EntryCount: 1, ResolvedSourceCount: 1},
			"material": {EntryCount: 1, ResolvedSourceCount: 1},
			"sphere":   {EntryCount: 1, ResolvedSourceCount: 1},
			"buddy":    {EntryCount: 1, ResolvedSourceCount: 1},
		},
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	publishable, err := applyCNAdminCatalogAssetCoverage(catalog, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(publishable) != len(catalog)-1 {
		t.Fatalf("publishable catalog count = %d, want %d", len(publishable), len(catalog)-1)
	}
	for _, entry := range publishable {
		if entry.RewardTypeID == 2 {
			t.Fatal("source-gap card remains grantable")
		}
		if entry.Kind != "currency" && entry.ImageURL == "" {
			t.Fatalf("publishable %s/%d has no image", entry.Kind, entry.RewardTypeID)
		}
	}
	if err := os.Remove(filepath.Join(root, "buddy", "6.webp")); err != nil {
		t.Fatal(err)
	}
	if _, err := applyCNAdminCatalogAssetCoverage(catalog, root); err == nil {
		t.Fatal("missing resolved Buddy output passed the Admin asset gate")
	}
}

func TestCNAdminMailCardQuantityAcceptsUpToOneHundred(t *testing.T) {
	entry := cnAdminCatalogEntry{
		Kind: "card", RewardType: 6, RewardTypeID: 1001, Name: "test card",
		LevelMax: 60, FameMax: 90, LoveMax: 100,
	}
	admin := &cnAdmin{catalogByKey: map[string]cnAdminCatalogEntry{
		cnAdminCatalogKey(entry.RewardType, entry.RewardTypeID): entry,
	}}
	request := cnAdminMailRequest{
		RewardType: 6, RewardTypeID: entry.RewardTypeID,
		CardLevel: 1, CardFame: 1,
	}
	for _, quantity := range []int{1, cnAdminMaximumInstanceRewardQuantity} {
		request.Quantity = quantity
		reward, _, err := admin.mailReward(request)
		if err != nil {
			t.Fatalf("card quantity %d was rejected: %v", quantity, err)
		}
		if reward.Num != quantity {
			t.Fatalf("card quantity = %d, want %d", reward.Num, quantity)
		}
	}
	request.Quantity = cnAdminMaximumInstanceRewardQuantity + 1
	if _, _, err := admin.mailReward(request); err == nil {
		t.Fatal("card quantity above 100 was accepted")
	}
}

func TestApplyCNAdminTargetLevelRaisesCapsWithoutTouchingInventory(t *testing.T) {
	policy, err := loadCNPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	state := release.State{
		User:  release.User{Level: 1, AP: 3, APMax: 20, BP: 20, BPMax: 20},
		Cards: []release.Card{{UniqueID: 1, CardID: 1001, Level: 1}},
	}
	if err := applyCNAdminTargetLevel(&state, policy, 50); err != nil {
		t.Fatal(err)
	}
	if state.User.Level != 50 || state.User.BP != 45 || state.User.BPMax != 45 ||
		state.User.AP != state.User.APMax || state.User.Experience != policy.CumulativeExperience(50) ||
		state.User.NowLevelExperience != 0 || state.User.NextLevelExperience != policy.ExperienceRequired(50) {
		t.Fatalf("unexpected level grant result: %+v", state.User)
	}
	if len(state.Cards) != 1 || state.Cards[0].CardID != 1001 || state.Cards[0].Level != 1 {
		t.Fatalf("level grant changed inventory: %+v", state.Cards)
	}
	if err := applyCNAdminTargetLevel(&state, policy, 49); err == nil {
		t.Fatal("level grant allowed an account downgrade")
	}
}

func TestApplyCNAdminTargetArthurRankOnlyRaisesHighWaterMark(t *testing.T) {
	state := release.State{
		User:  release.User{ArthurRank: 1},
		Cards: []release.Card{{UniqueID: 1, CardID: 1001, Level: 1}},
		Decks: []release.Deck{{ArthurType: 1, Index: 1, CardUniqueIDs: []int64{1}}},
	}
	if err := applyCNAdminTargetArthurRank(&state, 10); err != nil {
		t.Fatal(err)
	}
	if state.User.ArthurRank != 10 || len(state.Cards) != 1 || state.Cards[0].Level != 1 ||
		len(state.Decks) != 1 || len(state.Decks[0].CardUniqueIDs) != 1 || state.Decks[0].CardUniqueIDs[0] != 1 {
		t.Fatalf("Arthur-rank grant changed unrelated account state: %+v", state)
	}
	if err := applyCNAdminTargetArthurRank(&state, 9); err == nil {
		t.Fatal("Arthur-rank grant allowed an account downgrade")
	}
	if err := applyCNAdminTargetArthurRank(&state, cnAdminMaximumArthurRank+1); err == nil {
		t.Fatal("Arthur-rank grant allowed a value outside the CN client enum")
	}
}
