package multiplayer

import (
	"errors"
	"fmt"
	"math/bits"
)

type BattleEngine struct {
	catalog          *CombatCatalog
	seed             uint32
	rng              xorShift128
	turn             int
	elapsedWaveTurns int // 5d08e's total turn survives Start; 5d077's wave turn does not.
	costInitial      int
	costTurnOffset   int
	holdMax          int
	players          [4]battlePlayer
	enemies          [4]battleEnemy
	enemyCount       int
	phase            battlePhase
	selectedPlays    map[int]cardPlaySubmission
	enemyUses        map[int]int
	// Native AI trigger predicates can retain the matched battle member for a
	// later TRIGGER_TARGET action. It is captured per candidate in
	// selectEnemyAction so evaluating another condition cannot overwrite it.
	enemyTriggerTarget int
	turnStats          battleTurnStats
	damageHistory      [29][]battleDamageEvent // Relative ages 1..29; current turn lives in turnStats.
	turnActions        []battleAction
	forceEndCheck      bool
	endType            int
	continueAllowed    bool
	continuePending    bool
	continueCount      int
	nativeSkillSerial  int // 5d982: completed 7adb0 calls; card buffs share their action serial.
	waveRecovery       []BattleResult
	turnDrawn          [4][5]bool          // 5dd72 prepares hands in TurnPhase; UserPhase emits DEAL.
	resumeSide         int                 // Native 5d0a5; retained after a terminal action.
	skillTargets       *battleSkillTargets // Scoped to one 7adb0 invocation; nested death/CALL skills restore it.
	openingDraw        *[4][10]bool        // Pending first-battle shuffle, after Start applies all passives.
}

type battlePhase uint8

const (
	battlePhaseCreated battlePhase = iota
	battlePhaseStarted
	battlePhaseTurn
	battlePhaseUser
	battlePhaseUserAttack
	battlePhaseChaliceUser
	battlePhaseEnemy
	battlePhaseChaliceEnemy
	battlePhaseEnded
)

type battlePlayer struct {
	MemberType        int
	ExecutedBuffKinds [69]uint32 // d0889 reads this caster's successful good-status executions.
	ArthurType        int
	HP                int
	MaxHP             int
	BaseMaxHP         int
	Attack            int
	BaseAttack        int
	Magic             int
	BaseMagic         int
	Recovery          int
	BaseRecovery      int
	Defense           int
	BaseDefense       int
	MDefense          int
	BaseMDefense      int
	LimitAttack       int
	LimitMagic        int
	LimitRecovery     int
	Cost              int // Remaining selectable cost, not the native COST wire base.
	Burst             int
	BurstState        int
	BurstBreak        int
	BurstRecharge     [3]int
	CardCostDown      [11]int
	CardBurstSkills   [11][]int
	DamageTaken       int
	TurnDamage        int
	BaseAttribute     string
	Attribute         string
	Deck              [10]BattleCard
	SupportDeck       [10]BattleCard
	Spheres           [3]battleSphere
	Buddies           [5]BattleBuddy
	DeckOrder         [10]int // Native slots: zero-based Deck index, -1 for a null slot.
	DrawIndex         int     // Scan cursor; drawn slots before it are no longer in the pool.
	DrawCount         int     // Slot extent, not the number of non-null cards.
	ContinueDraw      bool    // Native +59ab0: refill hand once after a paid continuation.
	Discard           []int
	Hand              [5]int
	// CardDisplay is the native per-CARD_TYPE cache compared by FUN_0003fa8a
	// before it projects ResultCmd314. It is presentation state owned by the
	// battle engine, not a reconstruction from previously emitted CSV rows.
	CardDisplay     [11]battleDisplayPower
	Effects         []battleEffect
	BlessHolds      []battleBlessHold
	CardHolds       []battleCardHold
	GameOver        bool
	ReservedChalice int
	// Native GameMasterTeamBattle stores eleven signed 64-bit hate buckets per
	// user. Bucket zero is the current turn; TurnPhase shifts older values one
	// slot and FUN_000576ec weights them 100,90,...,0 percent for HATE targets.
	HateHistory [11]int64
}

type battleSphere struct {
	Slot            int
	SphereID        int
	Level           int
	Type            string
	Count           int
	Maximum         int
	Playable        bool
	Remaining       int
	ChalicePlayable bool // cf186 post-selection gate, distinct from 309's turn condition.
	// Display is the FUN_00041a42 comparison cache; 315 sends power and marker.
	Display battleDisplayPower
}

type battleDisplayPower struct {
	Power  int
	Marker int
	Known  bool
}

type battleEnemy struct {
	Trance               battleEnemyTrance
	StatusCooldown       [15]int
	MemberType           int
	EnemyID              int
	Parent               int
	HP                   int
	MaxHP                int
	BaseMaxHP            int
	Attack               int
	BaseAttack           int
	Magic                int
	BaseMagic            int
	Recovery             int
	BaseRecovery         int
	Defense              int
	BaseDefense          int
	MDefense             int
	BaseMDefense         int
	DamageReduction      int
	AttributeFixed       [5]int
	LimitAttack          int
	LimitMagic           int
	LimitRecovery        int
	DamageTaken          int
	TurnDamage           int
	TurnPhysical         int
	TurnMagic            int
	AITurn               battleEnemyAITurnStats
	AIFlags              uint32   // 4cee1+13b24; preserved by TurnPhase's 56dea reset.
	AIVariables          [5]int32 // 4cee1+13b28; actor-local, preserved across turns.
	ExecutedBuffKinds    [69]uint32
	DiedTurn             int
	DeathCount           int
	BaseAttribute        string
	Attribute            string
	Level                CombatEnemyLevel
	Awake                int
	Broken               bool
	PendingBreak         bool // 59b54 is consumed after all roles, even if a later heal restored HP.
	ActionConsumed       int
	ChargedActions       [5]enemyActionCandidate
	ChargedActionCount   int
	DeathActionTriggered bool
	Drops                []BattleDrop
	DropResolved         bool
	DropReleased         bool
	Effects              []battleEffect
}

// battleEffect is the durable state shared by the rule families in the
// official CN skill-role tables. Values remain typed here; ResultCmd rows are
// only a transport projection and are never used as the state itself.
type battleEffect struct {
	Function      string
	ListType      int
	Parameter     string
	Attribute     string
	DamageKind    string
	CardType      int
	CostMin       int
	CostMax       int
	HateRate      int
	HateLimit     int
	Mask          int
	Value         int
	Delta         int
	Kind          int
	Rate          int
	Uses          int
	Parameters    [4]int
	Remaining     int
	AppliedTurn   int
	Source        int
	SourceSkillID int // Original SkillData identity; not caster or role-function ID.
	RoleIndex     int
	BurstPassive  bool
}

type battleTurnStats struct {
	Damage              int
	Physical            int
	Magic               int
	Enchant             int
	DamageHits          int
	DamageHitsByPhysics [3]int
	Heal                int
	MaxChain            int
	DamageByUser        [4]int
	PlayedByUser        [4]int
	KindsByUser         [4]map[string]int
	DamageEvents        []battleDamageEvent
}

// GameMaster enemy AI block, not the whole party's damage totals or the
// member's HP-loss history. Parent propagation and DOT use other counters.
type battleEnemyAITurnStats struct {
	Damage          [3]int64 // PHYSICS, MAGIC, ALL (enchant/reflection)
	Hits            [3]int
	BadStatus       uint32
	DebuffKinds     uint32
	PlayerBuffKinds [69]uint32
}

type battleDamageEvent struct {
	Target    int
	Source    int
	Value     int
	Attribute string
	Physics   string
	DOT       string
	Enchant   bool
	Special   bool // Trap/self-HP-cut/destruct/reflection: independent of attribute/physics/DOT filters.
}

// xorShift128 is the RNG family confirmed in libbattle5 FUN_000a8e57.
type xorShift128 struct {
	x uint32
	y uint32
	z uint32
	w uint32
}

func newXorShift128(seed uint32) xorShift128 {
	// libbattle5 FUN_000a8f54 seeds the four standard xor128 words from the
	// caller value, three rotations and the canonical Marsaglia constants.
	// Keep uint32 rotations explicit so seed zero retains the native constants.
	return xorShift128{
		x: seed ^ 0x075bcd15,
		y: bits.RotateLeft32(seed, 8) ^ 0x159a55e5,
		z: bits.RotateLeft32(seed, 16) ^ 0x1f123bb5,
		w: bits.RotateLeft32(seed, 24) ^ 0x05491333,
	}
}

func (random *xorShift128) next() uint32 {
	temporary := random.x ^ (random.x << 11)
	random.x, random.y, random.z = random.y, random.z, random.w
	random.w = random.w ^ (random.w >> 19) ^ temporary ^ (temporary >> 8)
	return random.w
}

func newBattleEngine(catalog *CombatCatalog, spec RoomSpec, members []Member) (*BattleEngine, error) {
	if catalog == nil {
		return nil, errors.New("combat catalog is unavailable")
	}
	if len(members) != maxRoomMembers {
		return nil, errors.New("combat party is incomplete")
	}
	party, exists := catalog.EnemyParties[spec.EnemyPartyID]
	if !exists {
		return nil, fmt.Errorf("enemy party %d is unavailable", spec.EnemyPartyID)
	}
	engine := &BattleEngine{
		catalog: catalog, seed: uint32(spec.Seed), rng: newXorShift128(uint32(spec.Seed)),
		costInitial: spec.CostInitial, holdMax: spec.HoldMax, continueAllowed: spec.ContinueAllowed,
		phase: battlePhaseCreated, selectedPlays: make(map[int]cardPlaySubmission, maxRoomMembers),
		enemyUses: make(map[int]int), openingDraw: new([4][10]bool),
	}
	seenMemberTypes := make(map[int]struct{}, maxRoomMembers)
	for _, member := range members {
		if len(member.DeckCards) != 10 {
			return nil, errors.New("combat member deck must contain ten cards")
		}
		if member.MemberType < 1 || member.MemberType > maxRoomMembers {
			return nil, errors.New("combat member type is invalid")
		}
		if _, duplicate := seenMemberTypes[member.MemberType]; duplicate {
			return nil, errors.New("combat member type is duplicated")
		}
		seenMemberTypes[member.MemberType] = struct{}{}
		player := &engine.players[member.MemberType-1]
		player.MemberType = member.MemberType
		player.ArthurType = member.ArthurType
		player.HP = member.HP
		player.MaxHP = member.HP
		player.BaseMaxHP = member.HP
		player.Attack = member.Attack
		player.BaseAttack = member.Attack
		player.Magic = member.Magic
		player.BaseMagic = member.Magic
		player.Recovery = member.Mind
		player.BaseRecovery = member.Mind
		player.BaseDefense = player.Defense
		player.BaseMDefense = player.MDefense
		// battle5_api_user_set initializes member+0x44 to ATTR.NEUTRAL=9.
		player.BaseAttribute = "NEUTRAL"
		player.Attribute = player.BaseAttribute
		player.LimitAttack = 99999
		player.LimitMagic = 99999
		player.LimitRecovery = 99999
		player.Cost = spec.CostInitial
		player.Burst = minInt(catalog.BurstGauge.Maximum, maxInt(0, spec.BurstGaugeInitial))
		var guaranteed [10]bool
		for cardIndex, card := range member.DeckCards {
			definition, exists := catalog.Cards[card.CardID]
			if !exists {
				return nil, fmt.Errorf("combat card %d is unavailable", card.CardID)
			}
			var err error
			guaranteed[cardIndex], err = catalog.cardBeginningDraw(definition)
			if err != nil {
				return nil, err
			}
			player.Deck[cardIndex] = card
			player.DeckOrder[cardIndex] = cardIndex
		}
		if err := validateSupportCards(member.SupportCards); err != nil {
			return nil, err
		}
		for _, card := range member.SupportCards {
			if _, _, err := catalog.CardSupportSkill(card); err != nil {
				return nil, err
			}
			player.SupportDeck[card.CardType-11] = card
		}
		for _, sphere := range member.DeckSpheres {
			if sphere.SphereType < 1 || sphere.SphereType > len(player.Spheres) || sphere.SphereID <= 0 || sphere.Level <= 0 {
				return nil, errors.New("combat sphere slot is invalid")
			}
			if player.Spheres[sphere.SphereType-1].SphereID != 0 {
				return nil, errors.New("combat sphere slot is duplicated")
			}
			definition, exists := catalog.Spheres[sphere.SphereID]
			if !exists {
				return nil, fmt.Errorf("combat sphere %d is unavailable", sphere.SphereID)
			}
			if sphere.Level > definition.MaxLevel {
				return nil, fmt.Errorf("combat sphere %d level %d exceeds max level %d", sphere.SphereID, sphere.Level, definition.MaxLevel)
			}
			player.Spheres[sphere.SphereType-1] = battleSphere{
				Slot: sphere.SphereType, SphereID: sphere.SphereID, Level: sphere.Level,
				Type: definition.Type, Count: definition.Count, Maximum: definition.Count,
			}
		}
		for _, buddy := range member.DeckBuddies {
			if buddy.BuddyType < 1 || buddy.BuddyType > len(player.Buddies) || buddy.BuddyID <= 0 || buddy.Level <= 0 {
				return nil, errors.New("combat buddy slot is invalid")
			}
			if player.Buddies[buddy.BuddyType-1].BuddyID != 0 {
				return nil, errors.New("combat buddy slot is duplicated")
			}
			definition, exists := catalog.Buddies[buddy.BuddyID]
			if !exists {
				return nil, fmt.Errorf("combat buddy %d is unavailable", buddy.BuddyID)
			}
			if buddy.Level > definition.MaxLevel {
				return nil, fmt.Errorf("combat buddy %d level %d exceeds max level %d", buddy.BuddyID, buddy.Level, definition.MaxLevel)
			}
			player.Buddies[buddy.BuddyType-1] = buddy
		}
		if player.Buddies[0].BuddyID != 0 {
			player.BurstState = burstGaugeNormal
		} else {
			player.Burst = 0
		}
		player.DrawCount = len(player.DeckOrder)
		engine.openingDraw[member.MemberType-1] = guaranteed
	}
	if err := engine.loadEnemyParty(party, spec.Drops); err != nil {
		return nil, err
	}
	return engine, nil
}

func (engine *BattleEngine) loadEnemyParty(party CombatEnemyParty, drops []BattleDrop) error {
	engine.enemies = [4]battleEnemy{}
	engine.enemyCount = 0
	for index, partySlot := range party.Slots {
		if partySlot.EnemyID == 0 {
			continue
		}
		definition, exists := engine.catalog.Enemies[partySlot.EnemyID]
		if !exists {
			return fmt.Errorf("enemy %d is unavailable", partySlot.EnemyID)
		}
		level, exists := engine.catalog.EnemyLevels[partySlot.EnemyID]
		if !exists {
			return fmt.Errorf("enemy level %d is unavailable", partySlot.EnemyID)
		}
		hp := definition.HP * partySlot.HPRate
		enemy := &engine.enemies[index]
		*enemy = battleEnemy{
			Trance:     newEnemyTrance(definition),
			MemberType: 5 + index, EnemyID: partySlot.EnemyID, Parent: partySlot.ParentIndex,
			HP: hp, MaxHP: hp, BaseMaxHP: hp,
			Attack: definition.Attack, BaseAttack: definition.Attack,
			Magic: definition.Magic, BaseMagic: definition.Magic,
			Recovery: definition.Recovery, BaseRecovery: definition.Recovery,
			Defense: definition.Defense, BaseDefense: definition.Defense,
			MDefense: definition.MagicDefense, BaseMDefense: definition.MagicDefense,
			DamageReduction: definition.DamageReduction, AttributeFixed: definition.AttributeFixed,
			LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
			BaseAttribute: definition.Attribute, Attribute: definition.Attribute, Level: level, Awake: level.InitialAwake,
		}
		engine.enemyCount = index + 1
	}
	if engine.enemyCount == 0 {
		return errors.New("combat enemy party is empty")
	}
	for _, drop := range drops {
		if drop.EnemyIndex < 0 || drop.EnemyIndex >= engine.enemyCount || engine.enemies[drop.EnemyIndex].EnemyID == 0 {
			return fmt.Errorf("combat drop enemy slot %d is unavailable", drop.EnemyIndex)
		}
		if !ValidBattleDrop(drop) {
			return errors.New("combat drop reward is invalid")
		}
		enemy := &engine.enemies[drop.EnemyIndex]
		enemy.Drops = append(enemy.Drops, drop)
	}
	return nil
}

func (engine *BattleEngine) shuffleDeck(player *battlePlayer) {
	// CardCollector<10>::shuffle (49ed8) always swaps ten native slots,
	// including null entries. Compacting the pool first changes both the card
	// order and the nine RNG words consumed by every ordinary reshuffle.
	for index := player.DrawCount; index < len(player.DeckOrder); index++ {
		player.DeckOrder[index] = -1
	}
	player.DrawCount, player.DrawIndex = len(player.DeckOrder), 0
	for index := len(player.DeckOrder) - 1; index > 0; index-- {
		swapIndex := int(engine.rng.next() % uint32(index+1))
		player.DeckOrder[index], player.DeckOrder[swapIndex] = player.DeckOrder[swapIndex], player.DeckOrder[index]
	}
}

func (player *battlePlayer) remainingDeckCount() int {
	count := 0
	for _, slot := range player.DeckOrder[player.DrawIndex:player.DrawCount] {
		if slot >= 0 {
			count++
		}
	}
	return count
}

func (engine *BattleEngine) recycleDrawPool(player *battlePlayer) {
	// FUN_00049830 pops trash in order into the FIRST empty native slot.
	// Unconsumed cards retain their positions, including across waves.
	for index := range player.DeckOrder {
		if index < player.DrawIndex || index >= player.DrawCount {
			player.DeckOrder[index] = -1
		}
	}
	index := 0
	for _, slot := range player.Discard {
		for index < len(player.DeckOrder) && player.DeckOrder[index] >= 0 {
			index++
		}
		if index < len(player.DeckOrder) {
			player.DeckOrder[index] = slot
		}
	}
	player.Discard = nil
	player.DrawCount = len(player.DeckOrder)
	engine.shuffleDeck(player)
}

func (engine *BattleEngine) drawCard(player *battlePlayer) int {
	if player.remainingDeckCount() == 0 && len(player.Discard) > 0 {
		engine.recycleDrawPool(player)
	}
	// FUN_00048d00 pops the first non-null slot without compacting others.
	for player.DrawIndex < player.DrawCount && player.DeckOrder[player.DrawIndex] < 0 {
		player.DrawIndex++
	}
	if player.DrawIndex == player.DrawCount {
		return 0
	}
	deckSlot := player.DeckOrder[player.DrawIndex] + 1
	player.DeckOrder[player.DrawIndex] = -1
	player.DrawIndex++
	return deckSlot
}
