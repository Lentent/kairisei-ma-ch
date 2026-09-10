package cnbootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/release"
)

func newFriendCapacityTestAccounts(t *testing.T) *cnAccountStore {
	t.Helper()
	seedPath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	jsonPath := filepath.Join(t.TempDir(), "save.json")
	if err := os.WriteFile(jsonPath, seed, 0o600); err != nil {
		t.Fatalf("write temporary primary seed: %v", err)
	}
	storage, err := newCNSaveDatabase(
		jsonPath,
		seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.loadOrImport(); err != nil {
		t.Fatalf("import primary account: %v", err)
	}
	accounts, err := newCNAccountStore(storage)
	if err != nil {
		t.Fatalf("initialize account store: %v", err)
	}
	return accounts
}

func TestActiveBattleUsesRuntimeMemoryAcrossAccountReload(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	state, err := accounts.loadState(cnPrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	// First normal quest: its final body drops gold and a stack material.
	state.Navigation = release.NavigationState{MainStoryID: 1001, MainStoryCN: true, StageAreaID: 100001}
	state.BurstProgress = [4]uint8{2}
	state.StoryTeamBattleSession = release.StoryTeamBattleSession{StoryID: 45001020}
	state.ActiveTeamBattle = &release.TeamBattleActiveState{
		Seed:   478,
		BossID: 10000101, BattleEnemyTypes: []int8{1, 1, 1, 1},
		StageQuestAreaID: 100001, StageQuestStageID: 10000101,
		BPUse: 7, ConsumesBattlePoints: true, FameSeed: "normal-quest-start",
		FameSources: []release.TeamBattleFameSourceState{{ArthurType: 1, LeaderFame: 100}},
		DropPlanSet: true,
		DropPlan: []release.TeamBattleEnemyDrop{
			{BattleIndex: 3, Reward: release.Reward{Type: 4, Num: 100, CardSkillLevels: []int16{}}},
			{BattleIndex: 3, Reward: release.Reward{Type: 13, Num: 1, RewardTypeID: 20000001, CardSkillLevels: []int16{}}},
		},
	}
	if err := accounts.persistState(cnPrimaryUserID, state); err != nil {
		t.Fatalf("persist normal quest start: %v", err)
	}
	reloaded, err := accounts.loadState(cnPrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ActiveTeamBattle == nil || len(reloaded.ActiveTeamBattle.DropPlan) != 2 || reloaded.ActiveTeamBattle.Seed != 478 {
		t.Fatal("active battle drop plan was lost")
	}
	if reloaded.Navigation != state.Navigation {
		t.Fatal("selected story/dungeon context was lost on account reload")
	}
	if reloaded.StoryTeamBattleSession.StoryID != 45001020 {
		t.Fatal("learning battle was lost on handler reload")
	}
	for _, drop := range reloaded.ActiveTeamBattle.DropPlan {
		if drop.Reward.CardSkillLevels == nil {
			t.Fatal("empty reward skill array became null on reload")
		}
	}
	if err := accounts.persistState(cnPrimaryUserID, reloaded); err != nil {
		t.Fatalf("persist resumed battle: %v", err)
	}
	encoded, err := encodeCNAccountMetadata(reloaded)
	if err != nil || bytes.Contains(encoded, []byte(`"active_team_battle"`)) {
		t.Fatal("ongoing battle was written to the durable account", err)
	}
	// A new repository instance is a new server lifetime, even with the same DB.
	restarted := &cnAccountStore{storage: accounts.storage}
	withoutBattle, err := restarted.loadState(cnPrimaryUserID)
	if err != nil || withoutBattle.ActiveTeamBattle != nil || withoutBattle.Navigation != state.Navigation {
		t.Fatal("restart restored a battle lock or lost persistent account data", err)
	}
	if withoutBattle.StoryTeamBattleSession.StoryID != 0 || withoutBattle.BurstProgress != state.BurstProgress {
		t.Fatal("learning battle survived restart or completed chapters were lost")
	}
	accounts.rememberBattle(cnPrimaryUserID, nil, release.StoryTeamBattleSession{})
}

func createNamedFriendCapacityAccount(t *testing.T, accounts *cnAccountStore, number int) int {
	t.Helper()
	identity, err := accounts.resolveLogin(fmt.Sprintf("00000000-0000-4000-8000-%012x", number))
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.User.Name = fmt.Sprintf("FriendEdge%02d", number)
	if err := accounts.persistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	return identity.UserID
}

func requireFollowAddError(t *testing.T, err error, resultCode int) {
	t.Helper()
	var followErr *httpapi.FollowAddError
	if !errors.As(err, &followErr) || followErr.ResultCode != resultCode || followErr.ResultString == "" {
		t.Fatalf("follow error = %v, want result code %d", err, resultCode)
	}
}

func TestFollowCapacityPreservesOriginalClientResultCodes(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	requester := createNamedFriendCapacityAccount(t, accounts, 0x101)
	targetA := createNamedFriendCapacityAccount(t, accounts, 0x102)
	targetB := createNamedFriendCapacityAccount(t, accounts, 0x103)
	targetC := createNamedFriendCapacityAccount(t, accounts, 0x104)
	if _, err := accounts.FollowFriendPointAccounts(requester, []int{targetA, targetB}, 2, 20); err != nil {
		t.Fatal(err)
	}
	_, err := accounts.FollowFriendPointAccounts(requester, []int{targetC}, 2, 20)
	requireFollowAddError(t, err, -3410)

	followerA := createNamedFriendCapacityAccount(t, accounts, 0x105)
	followerB := createNamedFriendCapacityAccount(t, accounts, 0x106)
	followerC := createNamedFriendCapacityAccount(t, accounts, 0x107)
	fullTarget := createNamedFriendCapacityAccount(t, accounts, 0x108)
	if _, err := accounts.FollowFriendPointAccounts(followerA, []int{fullTarget}, 2, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(followerB, []int{fullTarget}, 2, 20); err != nil {
		t.Fatal(err)
	}
	_, err = accounts.FollowFriendPointAccounts(followerC, []int{fullTarget}, 2, 20)
	requireFollowAddError(t, err, -3412)

	mutualOwner := createNamedFriendCapacityAccount(t, accounts, 0x109)
	mutualA := createNamedFriendCapacityAccount(t, accounts, 0x10a)
	mutualB := createNamedFriendCapacityAccount(t, accounts, 0x10b)
	if _, err := accounts.FollowFriendPointAccounts(mutualA, []int{mutualOwner}, 10, 20); err != nil {
		t.Fatal(err)
	}
	result, err := accounts.FollowFriendPointAccounts(mutualOwner, []int{mutualA}, 10, 1)
	if err != nil || !result.IsFriendFull {
		t.Fatalf("fill requester mutual capacity = (%+v, %v)", result, err)
	}
	if _, err := accounts.FollowFriendPointAccounts(mutualB, []int{mutualOwner}, 10, 20); err != nil {
		t.Fatal(err)
	}
	_, err = accounts.FollowFriendPointAccounts(mutualOwner, []int{mutualB}, 10, 1)
	requireFollowAddError(t, err, -3410)

	otherFull := createNamedFriendCapacityAccount(t, accounts, 0x10c)
	otherFriend := createNamedFriendCapacityAccount(t, accounts, 0x10d)
	otherRequester := createNamedFriendCapacityAccount(t, accounts, 0x10e)
	otherFullState, err := accounts.loadState(otherFull)
	if err != nil {
		t.Fatal(err)
	}
	otherFullState.User.FriendMax = 1
	if err := accounts.persistState(otherFull, otherFullState); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(otherFriend, []int{otherFull}, 10, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(otherFull, []int{otherFriend}, 10, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(otherFull, []int{otherRequester}, 10, 1); err != nil {
		t.Fatal(err)
	}
	_, err = accounts.FollowFriendPointAccounts(otherRequester, []int{otherFull}, 10, 20)
	requireFollowAddError(t, err, -3412)
}

func TestFriendPointAccountStatesReadsTargetedRelationshipStates(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	requester := createNamedFriendCapacityAccount(t, accounts, 0x201)
	outbound := createNamedFriendCapacityAccount(t, accounts, 0x202)
	inbound := createNamedFriendCapacityAccount(t, accounts, 0x203)
	mutual := createNamedFriendCapacityAccount(t, accounts, 0x204)
	unrelated := createNamedFriendCapacityAccount(t, accounts, 0x205)
	if _, err := accounts.FollowFriendPointAccounts(requester, []int{outbound, mutual}, 10, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(inbound, []int{requester}, 10, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(mutual, []int{requester}, 10, 20); err != nil {
		t.Fatal(err)
	}

	states, err := accounts.FriendPointAccountStates(
		requester,
		[]int{outbound, inbound, mutual, unrelated, outbound},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]int8{outbound: 5, inbound: 6, mutual: 3, unrelated: 0}
	if !maps.Equal(states, want) {
		t.Fatalf("targeted relationship states = %v, want %v", states, want)
	}
}

func TestPartnerDiscoveryReadsProjectionInsteadOfFullSnapshots(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	requester := createNamedFriendCapacityAccount(t, accounts, 0x301)
	target := createNamedFriendCapacityAccount(t, accounts, 0x302)
	database, err := accounts.storage.open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE cn_account_snapshot SET payload_json = ? WHERE user_id = ?`,
		[]byte("not-json"),
		target,
	); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	relations, err := accounts.ListFriendPointAccountRelations(requester)
	if err != nil {
		t.Fatalf("partner discovery decoded a full account snapshot: %v", err)
	}
	found := false
	for _, relation := range relations {
		if relation.State.User.UserID == target {
			found = relation.State.User.Name == "FriendEdge770"
			break
		}
	}
	if !found {
		t.Fatalf("target %d was absent from account projections", target)
	}
}

func TestAccountProjectionBoundsInventoryAndKeepsDeckReferences(t *testing.T) {
	state := release.State{User: release.User{UserID: cnPrimaryUserID},
		Cards:       make([]release.Card, 10000),
		Decks:       []release.Deck{{CardUniqueIDs: []int64{1}, SupportCardUniqueIDs: []int64{2}}},
		PVP:         release.PVPPlayerState{DefenseDecks: []release.PVPDeckSelection{{CardUniqueIDs: []int64{10000}}}},
		SupportDeck: release.SupportDeckState{CardCollectionIDs: []int{101}, CardCollectionLoveMaxIDs: []int{101}, UnlockSlotNums: []int8{1, 0, 0, 0}},
	}
	for i := range state.Cards {
		state.Cards[i] = release.Card{UniqueID: int64(i + 1), CardID: 101 + i, Level: 50, HP: 1234, Love: 5000, LoveMax: 10000, SkillLevels: []int16{3, 4}}
	}
	content, digest, err := encodeCNAccountProjection(state)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeCNAccountProjection(content, digest)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > 16*1024 || len(loaded.Cards) != 3 || loaded.Cards[2].UniqueID != 10000 || loaded.Cards[1].LoveMax != 10000 || loaded.Cards[0].HP != 1234 || !slices.Equal(loaded.Cards[0].SkillLevels, []int16{3, 4}) || len(loaded.SupportDeck.CardCollectionIDs) != 0 || len(loaded.SupportDeck.CardCollectionLoveMaxIDs) != 0 || loaded.SupportDeck.UnlockSlotNums[0] != 1 {
		t.Fatalf("public projection lost deck data or retained inventory/history: %d bytes, %+v", len(content), loaded.Cards)
	}
}

func TestSystemPartnerAccountsArePersistentFriendTargetsAndDoNotConsumePlayerIDs(t *testing.T) {
	seedPath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	temporary := t.TempDir()
	jsonPath := filepath.Join(temporary, "save.json")
	if err := os.WriteFile(jsonPath, seed, 0o600); err != nil {
		t.Fatalf("write temporary primary seed: %v", err)
	}
	storage, err := newCNSaveDatabase(
		jsonPath,
		seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.loadOrImport(); err != nil {
		t.Fatalf("import primary account: %v", err)
	}
	seedState, err := loadCNSaveState(seedPath)
	if err != nil {
		t.Fatalf("load system-partner seed: %v", err)
	}
	expectedPopulationCards, expectedPopulationDecks, _, err := cnSystemPartnerPopulationFromSeed(seedState)
	if err != nil {
		t.Fatalf("load expected system-partner population: %v", err)
	}
	accounts, err := newCNAccountStore(storage)
	if err != nil {
		t.Fatalf("initialize account store: %v", err)
	}

	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		_, expectedDeck, expectedLeader, err := cnSystemPartnerLoadoutFromSeed(seedState, arthurType)
		if err != nil {
			t.Fatalf("load expected system partner %d: %v", arthurType, err)
		}
		userID := cnSystemPartnerUserID(arthurType)
		state, err := accounts.loadState(userID)
		if err != nil {
			t.Fatalf("load system partner %d: %v", arthurType, err)
		}
		if state.User.Name != cnSystemPartnerNameByArthur[arthurType] ||
			state.User.ActiveArthurType != int(arthurType) || len(state.Cards) != len(expectedPopulationCards) {
			t.Fatalf("system partner %d identity = %+v, cards = %d", arthurType, state.User, len(state.Cards))
		}
		actualCardIDs := make([]int, len(state.Cards))
		expectedCardIDs := make([]int, len(expectedPopulationCards))
		for index, card := range state.Cards {
			actualCardIDs[index] = card.CardID
			expectedCardIDs[index] = expectedPopulationCards[index].CardID
		}
		var activeDeck *release.Deck
		for index := range state.Decks {
			if state.Decks[index].ArthurType == arthurType && state.Decks[index].Index == 0 {
				activeDeck = &state.Decks[index]
				break
			}
		}
		if !slices.Equal(actualCardIDs, expectedCardIDs) || len(state.Decks) != len(expectedPopulationDecks) ||
			activeDeck == nil || !slices.Equal(activeDeck.CardUniqueIDs, expectedDeck.CardUniqueIDs) {
			t.Fatalf(
				"system partner %d loadout = cards %v decks %+v, want cards %v deck %+v",
				arthurType,
				actualCardIDs,
				state.Decks,
				expectedCardIDs,
				expectedDeck,
			)
		}
		if arthurType == 4 {
			cardIDByUniqueID := make(map[int64]int, len(state.Cards))
			for _, card := range state.Cards {
				cardIDByUniqueID[card.UniqueID] = card.CardID
			}
			activeCardIDs := make([]int, len(activeDeck.CardUniqueIDs))
			for index, uniqueID := range activeDeck.CardUniqueIDs {
				activeCardIDs[index] = cardIDByUniqueID[uniqueID]
			}
			if slices.Contains(activeCardIDs, 10000030) || activeCardIDs[5] != 10000034 {
				t.Fatalf(
					"singer system-partner deck retained the mixed starter attack: %v",
					activeCardIDs,
				)
			}
		}
		if state.User.LeaderCardID != expectedLeader.CardID ||
			state.User.LeaderCardUniqueID != expectedLeader.UniqueID ||
			state.Onboarding.Step != cnOnboardingStepCount {
			t.Fatalf(
				"system partner %d leader/tutorial = (%d, %d, %+v)",
				arthurType,
				state.User.LeaderCardID,
				state.User.LeaderCardUniqueID,
				state.Onboarding,
			)
		}
	}

	primaryIdentity, err := accounts.resolveLogin("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if primaryIdentity.UserID != cnPrimaryUserID {
		t.Fatalf("primary player user ID = %d, want %d", primaryIdentity.UserID, cnPrimaryUserID)
	}
	identity, err := accounts.resolveLogin("00000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != cnPrimaryUserID+1 {
		t.Fatalf("first new player user ID = %d, want %d", identity.UserID, cnPrimaryUserID+1)
	}
	pvpOpponents, err := accounts.ListPVPOpponents(cnPrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pvpOpponents) != 1 || pvpOpponents[0].User.UserID != identity.UserID {
		t.Fatalf("PVP opponents included system partners: %+v", pvpOpponents)
	}

	targetID := cnSystemPartnerUserID(2)
	added, err := accounts.FollowFriendPointAccounts(cnPrimaryUserID, []int{targetID}, 100, 100)
	if err != nil {
		t.Fatalf("follow system partner: %v", err)
	}
	if !slices.Equal(added.RequestUserIDs, []int{targetID}) || added.IsFriendFull {
		t.Fatalf("follow response = %v", added)
	}
	relations, err := accounts.ListFriendPointAccountRelations(cnPrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, relation := range relations {
		if relation.State.User.UserID == targetID {
			found = relation.System && relation.FriendState == 5
			break
		}
	}
	if !found {
		t.Fatal("followed system partner was not returned as a persistent follow relation")
	}
}

func TestPlayerAccountLoadsSharedItemShopWithoutResettingOwnedItems(t *testing.T) {
	seedPath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	temporary := t.TempDir()
	jsonPath := filepath.Join(temporary, "save.json")
	if err := os.WriteFile(jsonPath, seed, 0o600); err != nil {
		t.Fatalf("write temporary primary seed: %v", err)
	}
	storage, err := newCNSaveDatabase(
		jsonPath,
		seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.loadOrImport(); err != nil {
		t.Fatalf("import primary account: %v", err)
	}
	accounts, err := newCNAccountStore(storage)
	if err != nil {
		t.Fatalf("initialize account store: %v", err)
	}
	identity, err := accounts.resolveLogin("00000000-0000-4000-8000-000000000099")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Items = append(state.Items, release.Item{ItemID: 9010, Num: 7})
	state.ItemShopConfigVersion = cnItemShopConfigVersion - 1
	state.ItemShopTabs = state.ItemShopTabs[:1]
	state.ItemShopTabs[0].Lineup = nil
	if err := accounts.persistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}

	migrated, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	hasQuickBuy := false
	if len(migrated.ItemShopTabs) == 5 {
		for _, lineup := range migrated.ItemShopTabs[0].Lineup {
			if lineup.LineupID == 992001 && lineup.Hidden {
				hasQuickBuy = true
				break
			}
		}
	}
	if migrated.ItemShopConfigVersion != cnItemShopConfigVersion || !hasQuickBuy {
		t.Fatalf("player item shop config was not migrated: %+v", migrated.ItemShopTabs)
	}
	for _, item := range migrated.Items {
		if item.ItemID == 9010 {
			if item.Num != 7 {
				t.Fatalf("water-crystal gacha coin balance = %d, want 7", item.Num)
			}
			return
		}
	}
	t.Fatal("water-crystal gacha coin is absent after migration")
}

func TestAccountSnapshotExcludesDerivedFriendProfiles(t *testing.T) {
	started := time.Now().Unix()
	accounts := newFriendCapacityTestAccounts(t)
	primary, err := accounts.resolveLogin("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if primary.UserID != cnPrimaryUserID {
		t.Fatalf("primary user ID = %d, want %d", primary.UserID, cnPrimaryUserID)
	}
	identity, err := accounts.resolveLogin("00000000-0000-4000-8000-000000000199")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Friends.Users = []release.Friend{{UserID: 1000002, Name: "legacy fake"}}
	if state.User.Comment != "请多关照！" || len(state.User.InviteID) != 9 {
		t.Fatal("new profile contains local diagnostic placeholders")
	}
	if _, err := strconv.Atoi(state.User.InviteID); err != nil {
		t.Fatal("friend-search ID is not numeric", err)
	}
	state.User.Comment = "一起打冰龙吧"
	if err := accounts.persistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}

	migrated, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Friends.Users == nil || len(migrated.Friends.Users) != 0 {
		t.Fatalf("migrated friend rows = %#v, want non-nil empty list", migrated.Friends.Users)
	}
	if migrated.User.Comment != state.User.Comment {
		t.Fatal("saved custom profile comment was replaced")
	}
	projections, err := accounts.listLocalAccountStates(primary.UserID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, projection := range projections {
		if projection.LastLoginUnix < started || projection.LastLoginUnix > time.Now().Unix() || len(projection.User.InviteID) != 9 {
			t.Fatal("friend projection has invalid login time or search ID", projection.User.UserID)
		}
	}

	database, err := accounts.storage.open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var content []byte
	if err := database.QueryRow(
		`SELECT payload_json FROM cn_account_snapshot WHERE user_id = ?`,
		identity.UserID,
	).Scan(&content); err != nil {
		t.Fatal(err)
	}
	persisted, err := decodeCNAccountSnapshot(content)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Friends.Users == nil || len(persisted.Friends.Users) != 0 {
		t.Fatalf("persisted friend rows = %#v, want non-nil empty list", persisted.Friends.Users)
	}
}
