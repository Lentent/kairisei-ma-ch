package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestFameTrainingDisabledStillAllowsSameCardFusion(t *testing.T) {
	s := ConsumptionTestStore(t)
	s.cards[0].Fame = 3
	s.cards[1].Fame = 5
	base := s.cardDefinitions[10]
	base.SameCardID = 20
	base.FameMax = 100
	s.cardDefinitions[10] = base
	material := s.cardDefinitions[20]
	material.FameMax = 100
	s.cardDefinitions[20] = material
	s.cardDevelopmentRules = map[int]cardDevelopmentRule{10: {DevelopmentType: 0, FameMax: 100, DevelopRadix: 20}}
	s.stive = 1000
	if _, e := s.StartCardFameTraining(1, 2, time.Unix(1000, 0)); e == nil {
		t.Fatal("disabled card started training")
	}
	if s.stive != 1000 || s.cards[0].Fame != 3 {
		t.Fatal("rejected training mutated balance or fame")
	}
	_, result, _, _, e := s.FuseCard(1, []int64{2}, nil, nil)
	if e != nil || result.Fame != 8 || len(s.cards) != 1 {
		t.Fatalf("disabled training broke fusion: %+v %v", result, e)
	}
}

func TestFameTrainingAlreadyPaidCanCancelAfterDisable(t *testing.T) {
	s := ConsumptionTestStore(t)
	s.cards = s.cards[1:]
	s.cards[0].Fame = 3
	definition := s.cardDefinitions[20]
	definition.Level = 1
	definition.LevelMax = 1
	definition.FameMax = 100
	s.cardDefinitions[20] = definition
	s.cardDevelopmentRules = map[int]cardDevelopmentRule{20: {DevelopmentType: 2, FameMax: 100, DevelopRadix: 20}}
	s.cardDevelopmentPolicy = gamestate.CardDevelopmentPolicy{TimeEveryFameSeconds: 60, CoinEveryHour: 1}
	s.stive = 1000
	if _, e := s.StartCardFameTraining(2, 4, time.Unix(1000, 0)); e != nil {
		t.Fatal(e)
	}
	rule := s.cardDevelopmentRules[20]
	rule.DevelopmentType = 0
	s.cardDevelopmentRules[20] = rule
	result, e := s.CancelCardFameTraining(2, time.Unix(1120, 0))
	if e != nil || result.Fame != 5 || result.Stive != 960 || result.IsLock != 0 || s.cardFameTraining != nil {
		t.Fatalf("paid training lost fame/refund after disable: %+v %v", result, e)
	}
}
