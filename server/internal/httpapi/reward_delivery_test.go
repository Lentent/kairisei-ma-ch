package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

func TestSettlementOverflowPreservesProgressAndClaimableReward(t *testing.T) {
	for _, kind := range []string{"battle", "explore", "story", "gacha", "item-gacha"} {
		t.Run(kind, func(t *testing.T) {
			reward := release.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}
			s := &store{items: map[int]release.Item{10: {ItemID: 10, Num: 1}},
				itemDefinitions: map[int]release.ItemDefinition{10: {ItemID: 10, MaxOwned: 1}},
			}
			var complete func() (presentReceiveResult, error)
			switch kind {
			case "battle":
				s.teamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":100001,"9":0,"10":[{"0":10000101,"10":0}]}],"11":[],"12":[]}`)
				s.activeBattle = &teamBattleContext{BossID: 10000101}
				profile := release.TeamBattleRewardProfile{BossID: 10000101,
					ResultRewards:     []release.Reward{{Type: 4, Num: 5}, reward},
					FirstClearRewards: []release.Reward{{Type: 10, Num: 50}},
				}
				complete = func() (presentReceiveResult, error) {
					result, err := s.completeTeamBattle(10000101, true, []release.TeamBattleRewardProfile{profile})
					return result.Result, err
				}
			case "explore":
				s.exploreActive, s.exploreStartedAt = true, time.Now()
				s.decks = []deckInfo{{ArthurType: 1, CardUniqueIDs: []int64{}}}
				complete = func() (presentReceiveResult, error) {
					result, _, _, _, err := s.endExplore([]release.Reward{reward})
					return result, err
				}
			case "story":
				s.storyRewardPolicy.MainFirstClear = reward
				s.storyMainParts = []release.StoryMainPart{{Sections: []release.StoryMainSection{{Stories: []release.StoryMain{{StoryMainID: 1, StateFlag: 1}}}}}}
				s.activeMainStoryID = 1
				complete = func() (presentReceiveResult, error) { return s.endMainStory(true) }
			case "gacha":
				s.coinFree = 5
				s.gachas = []release.GachaProfile{{GachaID: 7, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1,
					RewardPool: []release.WeightedReward{{Weight: 1, Reward: reward}}}}
				complete = func() (presentReceiveResult, error) {
					result, err := s.playGacha(7, 3, nil)
					return result.Reward, err
				}
			case "item-gacha":
				s.items[20] = release.Item{ItemID: 20, Num: 1}
				s.itemDefinitions[20] = release.ItemDefinition{ItemID: 20, ItemType: "GACHA", Function: "GACHA_EXEC", MaxOwned: 10}
				s.itemGachaProfiles = map[int]release.ItemGachaProfile{20: {ItemID: 20, Rewards: []release.Reward{reward}}}
				complete = func() (presentReceiveResult, error) {
					result, err := s.playItemGacha(20, 1)
					return result.Reward, err
				}
			}
			result, err := complete()
			if err != nil {
				t.Fatalf("full inventory blocks completed %s: %v", kind, err)
			}
			if !result.InPresentBox || len(result.Items) != 0 {
				t.Fatal("mailed reward was reported as added inventory")
			}
			if s.activeBattle != nil || s.exploreActive || s.activeMainStoryID != 0 || len(s.presents) != 1 || s.items[10].Num != 1 {
				t.Fatal("settlement did not finish with exactly one unclaimed reward")
			}
			if kind == "battle" && (s.gold != 5 || s.coinFree != 50) {
				t.Fatal("full inventory blocked ordinary currency rewards")
			}
			if (kind == "gacha" && s.coinFree != 0) || (kind == "item-gacha" && s.items[20].Num != 0) {
				t.Fatal("overflow draw did not consume payment")
			}
			if kind == "gacha" || kind == "item-gacha" {
				if len(result.Rewards) != 1 || !result.Rewards[0].InPresentBox || len(gachaItemDeltas(result)) != 0 {
					t.Fatal("mailed gacha reward was sent as an inventory delta")
				}
			}
			_, _ = complete()
			if len(s.presents) != 1 {
				t.Fatal("retry duplicated overflow mail")
			}
			encoded, err := json.Marshal(s.presents)
			if err != nil {
				t.Fatal(err)
			}
			reloaded := &store{items: map[int]release.Item{10: {ItemID: 10}}, itemDefinitions: s.itemDefinitions}
			if err := json.Unmarshal(encoded, &reloaded.presents); err != nil {
				t.Fatal(err)
			}
			id := reloaded.presents[0].PresentID
			issued := reloaded.presents[0].IssuedAtUnix
			if issued <= 0 || issued > time.Now().Unix() {
				t.Fatal("gift issue time was not saved")
			}
			projected := append([]release.Present(nil), reloaded.presents...)
			projectPresentTimes(projected, issued+125)
			if projected[0].AddElapsedSec != 125 || projected[0].IssuedAtUnix != 0 || reloaded.presents[0].IssuedAtUnix != issued {
				t.Fatal("gift age projection changed persisted issuance time")
			}
			if _, err := reloaded.receivePresent(id); err != nil || reloaded.items[10].Num != 1 || len(reloaded.presents) != 1 || reloaded.presents[0].State != 1 {
				t.Fatalf("saved overflow reward is not claimable after freeing space: %v", err)
			}
			_, _ = reloaded.receivePresent(id)
			if _, err := reloaded.deletePresents(id); err != nil {
				t.Fatal(err)
			}
			// A cached row may submit deletion again after it has been archived.
			if _, err := reloaded.deletePresents(id); err != nil {
				t.Fatal(err)
			}
			if reloaded.items[10].Num != 1 || len(reloaded.presentHistories) != 1 {
				t.Fatal("retry duplicated saved reward")
			}
		})
	}
}
