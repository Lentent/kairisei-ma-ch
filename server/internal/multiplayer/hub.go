package multiplayer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"kairisei.local/server/internal/gamestate"
)

const (
	maxRoomMembers      = 4
	deckSphereSlots     = 3
	deckBuddySlotCount  = 5
	pendingLifetime     = 2 * time.Minute
	reservationLifetime = time.Minute
	comebackLifetime    = 2 * time.Minute
	completedLifetime   = 30 * time.Minute
	firstLocalRoomID    = int64(6020000001)
	defaultMemberCount  = 2
	MaxBattleWaves      = 64 // Matches the bounded CN RoomCountdownFinish receiver patch.
)

var ErrCompletedBattleUnavailable = errors.New("completed room is unavailable")
var ErrCompletedBattlePending = errors.New("room battle has not completed")
var ErrCompletedBattleIneligible = errors.New("completed room result is unavailable for this account")

// Availability can change between listing, reservation and entry. Keep these
// business rejections distinct from invalid profiles and infrastructure errors.
var ErrRoomUnavailable = errors.New("room is unavailable")
var ErrRoomArthurUnavailable = errors.New("room Arthur type is unavailable")

type RoomState string

const (
	RoomStateOpen      RoomState = "open"
	RoomStateCountdown RoomState = "countdown"
	RoomStateBattle    RoomState = "battle"
	RoomStateClosed    RoomState = "closed"
)

type Member struct {
	MemberType    int
	UserID        int
	Level         int
	ArthurType    int
	JobType       int
	HP            int
	Attack        int
	Magic         int
	Mind          int
	Name          string
	LeaderCardID  int
	LeaderFame    int
	LeaderLevel   int
	DeckRank      int
	DeckName      string
	CostumeID     int
	PartsIDs      []int
	DeckHonorIDs  []int
	DeckCards     []BattleCard
	SupportCards  []BattleCard
	DeckSpheres   []BattleSphere
	DeckBuddies   []BattleBuddy
	IsBurst       int
	IsRoomLoading int
	IsFirstMatch  int
	RookieType    int
	IsBuddy       int
}

// BattleCard is the persisted deck identity required by the official client
// to build CardBattleData for remote room members. CardType is the one-based
// deck slot used by BattleSv result commands.
type BattleCard struct {
	CardType int
	CardID   int
	Level    int
	Love     int
}

// BattleSphere is one of the original client's three equipped sphere slots.
// SphereType is one-based and remains distinct from the sphere master ID.
type BattleSphere struct {
	SphereType int
	SphereID   int
	Level      int
}

// BattleBuddy is the persisted buddy identity equipped in one of the five
// official deck buddy slots. BuddyType is one-based and remains distinct from
// the buddy master ID.
type BattleBuddy struct {
	BuddyType int
	BuddyID   int
	Level     int
}

// BattleDrop is one deterministic reward assigned to an enemy slot by the
// local HTTP publication profile. EnemyIndex is the zero-based slot consumed
// by TeamBattleSoloDropInfo; the battle engine converts it to member 5..8 only
// when it projects ResultCmd83/84/108.
type BattleDrop struct {
	EnemyIndex   int
	RewardType   int
	Num          int
	RewardTypeID int
}

type RoomSpec struct {
	FameRewardsSet     bool
	FameRewards        []gamestate.Reward
	Battles            []gamestate.TeamBattleReplayBattle
	DropLedgerVersion  int
	DropPlan           []gamestate.TeamBattleEnemyDrop
	BattlePointUse     int
	ContinueAllowed    bool
	BossID             int
	BossGroupID        int
	EnemyPartyID       int
	EnemyType          int
	RoomType           int
	Password           string
	NeedDeckRank       int
	NeedHP             int
	NeedFame           int
	Comment            string
	Flag               int
	AllowToLeave       int
	GameStartMemberNum int
	AutoStart          bool
	CostInitial        int
	HoldMax            int
	BurstGaugeInitial  int
	Seed               int
	Drops              []BattleDrop
	BossGroup          any
	Owner              Member
	OwnerFallbackParty []Member
}

type RoomSnapshot struct {
	RoomID             int64
	BossID             int
	BossGroupID        int
	RoomType           int
	HasPassword        bool
	NeedDeckRank       int
	NeedHP             int
	NeedFame           int
	Comment            string
	Flag               int
	AllowToLeave       int
	GameStartMemberNum int
	OwnerMemberType    int
	GameSpeed          int
	State              RoomState
	BossGroup          any
	Members            []Member
}

// CompletedBattle is the immutable room projection retained between the
// BattleSv GameClose frame and each eligible account's TeamBattleResult HTTP
// request. Eligible accounts were either connected at completion or still in
// their authenticated comeback window. Mutable account rewards remain in the
// per-account SQLite store.
type CompletedBattle struct {
	FameRewardsSet     bool
	FameRewards        []gamestate.Reward
	Turns              int
	BattleIndex        int
	Progress           int
	DropLedgerVersion  int
	DestroyedEnemyBits int
	ReleasedDrops      []gamestate.TeamBattleEnemyDrop
	HostCostPaid       bool
	RoomID             int64
	BossID             int
	BossGroupID        int
	OwnerMemberType    int
	Members            []Member
	OnlineUserIDs      []int
	CompletedAtUnix    int64
}

// CompletionRepository keeps the immutable room outcome separate from each
// account's reward receipt. It lets the HTTP settlement survive a server
// restart without moving mutable character state out of the account snapshot.
type CompletionRepository interface {
	// NextRoomID atomically reserves an ID, even if the room never completes.
	NextRoomID(minimum int64) (int64, error)
	SaveCompleted(CompletedBattle, time.Time) error
	LoadCompleted(roomID int64, now time.Time) (CompletedBattle, time.Time, error)
}

type Credential struct {
	AuthToken string
	Signature string
}

type Endpoint struct {
	Host string
	Port uint16
}

type pendingKind int

const (
	pendingCreate pendingKind = iota + 1
	pendingEnter
)

type pendingRequest struct {
	Kind      pendingKind
	RoomID    int64
	Spec      RoomSpec
	Member    Member
	Signature string
	ExpiresAt time.Time
}

type cardPlaySubmission struct {
	CardTypes    [5]int
	Targets      [5]int
	SphereSlot   int
	SphereTarget int
	TimedOut     bool
	// Set only by the server's automatic player; not accepted from the wire.
	Automatic bool
}

type room struct {
	RoomSnapshot
	battles                []gamestate.TeamBattleReplayBattle
	battleIndex            int
	progress               int
	nextBattlePending      bool
	nextBattleIndex        int
	gameNextFinished       map[int]bool
	waveDropsRecorded      map[int]bool
	releasedDrops          []gamestate.TeamBattleEnemyDrop
	destroyedEnemyBits     int
	dropLedgerVersion      int
	dropPlan               []gamestate.TeamBattleEnemyDrop
	fameRewardsSet         bool
	fameRewards            []gamestate.Reward
	battlePointUse         int
	continueAllowed        bool
	continuation           *roomContinuation
	continueSequence       int
	continueDone           chan struct{}
	startCommitting        bool
	startDone              chan struct{}
	hostCostPaid           bool
	connections            map[int]*clientConn
	reservations           map[int]roomReservation
	comebackTokens         map[int]string
	disconnectedUntil      map[int]time.Time
	ownerFallbackParty     []Member
	enemyPartyID           int
	enemyType              int
	costInitial            int
	holdMax                int
	burstGaugeInitial      int
	seed                   int
	drops                  []BattleDrop
	countdownSyncs         int
	countdownDeadline      time.Time
	countdownFinishPending bool
	battleLoading          map[int]bool
	gameStarted            bool
	gameStartFinished      map[int]bool
	turnPhaseStarted       bool
	turnPhaseFinished      map[int]bool
	turnNumber             int
	userPhaseStarted       bool
	cardPlayPlans          map[int]cardPlaySubmission
	cardPlaySubmissions    map[int]cardPlaySubmission
	userAttackStarted      bool
	userAttackFinished     map[int]bool
	chaliceUserStarted     bool
	chaliceUserFinished    map[int]bool
	enemyPhaseStarted      bool
	enemyPhaseFinished     map[int]bool
	chaliceEnemyStarted    bool
	chaliceEnemyFinished   map[int]bool
	engineBattleEnd        int
	engine                 *BattleEngine
}

type roomReservation struct {
	UserID     int
	MemberType int
	ExpiresAt  time.Time
}

type Hub struct {
	mu                 sync.RWMutex
	nextRoomID         int64
	rooms              map[int64]*room
	pending            map[string]pendingRequest
	completed          map[int64]*completedBattle
	repository         CompletionRepository
	combat             *CombatCatalog
	startAuthorizer    func(BattleStart) error
	continueAuthorizer func(BattleContinue) (ContinueBalance, error)
	gameSpeed          func() int
}

func ValidGameSpeed(speed int) bool {
	return speed == 100 || speed == 150 || speed == 200
}

// AttachGameSpeed binds the in-memory operator setting before room activity.
// The callback is read-only; each room retains its creation-time speed.
func (h *Hub) AttachGameSpeed(current func() int) error {
	if current == nil || !ValidGameSpeed(current()) {
		return errors.New("multiplayer speed must be 100, 150 or 200 percent")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gameSpeed != nil || len(h.rooms) != 0 || len(h.pending) != 0 {
		return errors.New("attach multiplayer speed before room activity")
	}
	h.gameSpeed = current
	return nil
}

type BattleStart struct {
	RoomID      int64
	BossID      int
	OwnerUserID int
	BPUse       int
}

// AttachStartAuthorizer binds the account transaction boundary at startup.
// The callback is always called WITHOUT hub.mu: HTTP owns account->Hub order.
func (h *Hub) AttachStartAuthorizer(authorize func(BattleStart) error) error {
	if authorize == nil {
		return errors.New("multiplayer start authorizer is required")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.startAuthorizer != nil || len(h.rooms) != 0 || len(h.pending) != 0 {
		return errors.New("attach multiplayer start authorizer before room activity")
	}
	h.startAuthorizer = authorize
	return nil
}

// AttachCombatCatalog installs the immutable official CN combat definitions
// before room activity. Mutable battle state remains owned by each room.
func (h *Hub) AttachCombatCatalog(catalog *CombatCatalog) error {
	if catalog == nil {
		return errors.New("multiplayer combat catalog is required")
	}
	if err := catalog.Validate(); err != nil {
		return fmt.Errorf("validate multiplayer combat catalog: %w", err)
	}
	if err := catalog.ValidatePlayerFunctionCoverage(); err != nil {
		return fmt.Errorf("validate multiplayer combat capability: %w", err)
	}
	if err := catalog.ValidateBurstFunctionCoverage(); err != nil {
		return fmt.Errorf("validate multiplayer burst combat capability: %w", err)
	}
	if err := catalog.ValidateSphereSupportFunctionCoverage(); err != nil {
		return fmt.Errorf("validate multiplayer sphere support capability: %w", err)
	}
	if err := catalog.ValidateEnemyFunctionCoverage(); err != nil {
		return fmt.Errorf("validate multiplayer enemy combat capability: %w", err)
	}
	if err := catalog.ValidateRoleParameterContracts(); err != nil {
		return fmt.Errorf("validate multiplayer combat parameter contracts: %w", err)
	}
	// Full deterministic battle simulations belong to kairi-battle-audit and
	// release verification. Startup keeps the structural and function-coverage
	// gates above, which are sufficient to prevent invalid runtime dispatch.
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) != 0 || len(h.pending) != 0 || h.combat != nil {
		return errors.New("multiplayer combat catalog must be attached before room activity")
	}
	h.combat = catalog
	return nil
}

// AttachCompletionRepository is called once during local server startup,
// before any room credentials can be issued.
func (h *Hub) AttachCompletionRepository(repository CompletionRepository) error {
	if repository == nil {
		return errors.New("multiplayer completion repository is required")
	}
	nextRoomID, err := repository.NextRoomID(firstLocalRoomID)
	if err != nil {
		return fmt.Errorf("load multiplayer room sequence: %w", err)
	}
	if nextRoomID < firstLocalRoomID {
		return errors.New("persisted multiplayer room sequence is invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) != 0 || len(h.pending) != 0 || h.repository != nil {
		return errors.New("multiplayer completion repository must be attached before room activity")
	}
	h.repository = repository
	h.nextRoomID = nextRoomID
	return nil
}

type completedBattle struct {
	CompletedBattle
	claimed               map[int]bool
	expiresAt             time.Time
	comebackTokens        map[int]string
	disconnectedUntil     map[int]time.Time
	comebackConnections   map[int]*clientConn
	terminalEngine        *BattleEngine
	terminalBattleEndType int
}

func NewHub() *Hub {
	return &Hub{
		nextRoomID: firstLocalRoomID,
		rooms:      make(map[int64]*room),
		pending:    make(map[string]pendingRequest),
		completed:  make(map[int64]*completedBattle),
	}
}

// SettlementFor returns a completed room only to an account that still has an
// unclaimed result. CPU fallback character IDs are never eligible claimants.
func (h *Hub) SettlementFor(roomID int64, userID int) (CompletedBattle, error) {
	if roomID <= 0 || userID <= 0 {
		return CompletedBattle{}, errors.New("completed room identity is invalid")
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneCompletedLocked(now)
	completed, exists := h.completed[roomID]
	if !exists && h.rooms[roomID] != nil {
		return CompletedBattle{}, ErrCompletedBattlePending
	}
	if !exists && h.repository != nil {
		persisted, expiresAt, err := h.repository.LoadCompleted(roomID, now)
		if err != nil {
			if errors.Is(err, ErrCompletedBattleUnavailable) {
				return CompletedBattle{}, ErrCompletedBattleUnavailable
			}
			return CompletedBattle{}, fmt.Errorf("load completed room: %w", err)
		}
		claimed := make(map[int]bool, len(persisted.OnlineUserIDs))
		for _, eligibleUserID := range persisted.OnlineUserIDs {
			if eligibleUserID <= 0 {
				return CompletedBattle{}, errors.New("persisted completed room claimant is invalid")
			}
			if _, duplicate := claimed[eligibleUserID]; duplicate {
				return CompletedBattle{}, errors.New("persisted completed room repeats a claimant")
			}
			claimed[eligibleUserID] = false
		}
		completed = &completedBattle{
			CompletedBattle: cloneCompletedBattle(persisted),
			claimed:         claimed,
			expiresAt:       expiresAt,
		}
		h.completed[roomID] = completed
		exists = true
	}
	if !exists {
		return CompletedBattle{}, ErrCompletedBattleUnavailable
	}
	claimed, eligible := completed.claimed[userID]
	if !eligible || claimed {
		return CompletedBattle{}, ErrCompletedBattleIneligible
	}
	return cloneCompletedBattle(completed.CompletedBattle), nil
}

// MarkSettlementClaimed is advisory in memory. The immutable completion stays
// available until its bounded expiry so another eligible account can settle;
// account-owned SQLite receipts are the authoritative duplicate guard.
func (h *Hub) MarkSettlementClaimed(roomID int64, userID int) error {
	if roomID <= 0 || userID <= 0 {
		return errors.New("completed room identity is invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneCompletedLocked(time.Now())
	completed, exists := h.completed[roomID]
	if !exists {
		return errors.New("completed room is unavailable")
	}
	if _, eligible := completed.claimed[userID]; !eligible {
		return errors.New("completed room result is unavailable for this account")
	}
	completed.claimed[userID] = true
	return nil
}

func (h *Hub) IssueCreate(spec RoomSpec) (Credential, error) {
	if err := validateRoomSpec(spec); err != nil {
		return Credential{}, err
	}
	credential, err := newCredential()
	if err != nil {
		return Credential{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.combat != nil {
		for index, wave := range roomSpecBattles(spec) {
			party, exists := h.combat.EnemyParties[wave.EnemyPartyID]
			if !exists {
				return Credential{}, errors.New("room enemy party is unavailable")
			}
			for _, drop := range roomSpecWaveDrops(spec, index) {
				if party.Slots[drop.EnemyIndex].EnemyID == 0 {
					return Credential{}, errors.New("room drop references an empty enemy slot")
				}
			}
		}
	}
	h.prunePendingLocked(time.Now())
	h.pending[credential.AuthToken] = pendingRequest{
		Kind:      pendingCreate,
		Spec:      cloneRoomSpec(spec),
		Member:    cloneMember(spec.Owner),
		Signature: credential.Signature,
		ExpiresAt: time.Now().Add(pendingLifetime),
	}
	return credential, nil
}

func (h *Hub) IssueEnter(roomID int64, member Member) (Credential, error) {
	if roomID <= 0 {
		return Credential{}, errors.New("room id is invalid")
	}
	if err := validateMember(member); err != nil {
		return Credential{}, err
	}
	credential, err := newCredential()
	if err != nil {
		return Credential{}, err
	}
	now := time.Now()
	h.expireReservations(now)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prunePendingLocked(now)
	current, exists := h.rooms[roomID]
	if !exists || current.State != RoomStateOpen {
		return Credential{}, ErrRoomUnavailable
	}
	if len(current.Members) >= maxRoomMembers {
		return Credential{}, fmt.Errorf("%w: room is full", ErrRoomArthurUnavailable)
	}
	if err := validateRoomRequirements(current.RoomSnapshot, member); err != nil {
		return Credential{}, err
	}
	for _, existing := range current.Members {
		if existing.UserID == member.UserID {
			return Credential{}, fmt.Errorf("%w: member is already present", ErrRoomUnavailable)
		}
		if existing.ArthurType == member.ArthurType {
			return Credential{}, ErrRoomArthurUnavailable
		}
	}
	reservation, exists := current.reservations[member.ArthurType]
	if !exists || reservation.UserID != member.UserID || !reservation.ExpiresAt.After(now) {
		return Credential{}, fmt.Errorf("%w: reservation expired or missing", ErrRoomUnavailable)
	}
	h.pending[credential.AuthToken] = pendingRequest{
		Kind:      pendingEnter,
		RoomID:    roomID,
		Member:    cloneMember(member),
		Signature: credential.Signature,
		ExpiresAt: time.Now().Add(pendingLifetime),
	}
	return credential, nil
}

func (h *Hub) Reserve(roomID int64, userID int, arthurType int) (int64, error) {
	if roomID <= 0 || userID <= 0 || arthurType < 1 || arthurType > 4 {
		return 0, errors.New("room reservation is invalid")
	}
	now := time.Now()
	h.expireReservations(now)
	h.mu.Lock()
	h.prunePendingLocked(now)
	current := h.rooms[roomID]
	memberType, err := roomReservationSlot(current, userID, arthurType, now)
	if err != nil {
		h.mu.Unlock()
		return 0, err
	}
	cancellations := make([][]frameDelivery, 0, 1)
	for _, candidate := range h.rooms {
		for role, reservation := range candidate.reservations {
			if reservation.UserID != userID || candidate == current && role == arthurType {
				continue
			}
			delete(candidate.reservations, role)
			cancellations = append(cancellations,
				reserveMemberReservationLocked(roomConnections(candidate), reservation.MemberType, false))
		}
	}
	limit := now.Add(reservationLifetime)
	current.reservations[arthurType] = roomReservation{UserID: userID, MemberType: memberType, ExpiresAt: limit}
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveMemberReservationLocked(connections, memberType, true)
	h.mu.Unlock()
	for _, cancellation := range cancellations {
		sendRoomFrames(cancellation)
	}
	sendRoomFrames(deliveries)
	h.scheduleReservationExpiry(roomID, userID, arthurType, memberType, limit)
	return limit.Unix(), nil
}

func (h *Hub) CancelReservation(roomID int64, userID int, arthurType int) error {
	if roomID <= 0 || userID <= 0 || arthurType < 1 || arthurType > 4 {
		return errors.New("room reservation cancellation is invalid")
	}
	h.expireReservations(time.Now())
	h.mu.Lock()
	current, exists := h.rooms[roomID]
	if !exists {
		h.mu.Unlock()
		// Closing a room already removes its reservations. Cancellation is done.
		return nil
	}
	reservation, exists := current.reservations[arthurType]
	if !exists {
		h.mu.Unlock()
		return nil
	}
	if reservation.UserID != userID {
		h.mu.Unlock()
		return errors.New("room reservation belongs to another user")
	}
	delete(current.reservations, arthurType)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveMemberReservationLocked(connections, reservation.MemberType, false)
	h.mu.Unlock()
	sendRoomFrames(deliveries)
	return nil
}

// Search and reservation use the same live slot check. Listing is read-only;
// expired reservations can await their timer without hiding a usable room.
func roomReservationSlot(current *room, userID, arthurType int, now time.Time) (int, error) {
	if current == nil || current.State != RoomStateOpen {
		return 0, ErrRoomUnavailable
	}
	if len(current.Members) >= maxRoomMembers {
		return 0, fmt.Errorf("%w: room is full", ErrRoomArthurUnavailable)
	}
	for _, member := range current.Members {
		if member.UserID == userID {
			return 0, fmt.Errorf("%w: member is already present", ErrRoomUnavailable)
		}
		if member.ArthurType == arthurType {
			return 0, ErrRoomArthurUnavailable
		}
	}
	if reservation, exists := current.reservations[arthurType]; exists && reservation.ExpiresAt.After(now) {
		if reservation.UserID != userID {
			return 0, fmt.Errorf("%w: reserved", ErrRoomArthurUnavailable)
		}
		return reservation.MemberType, nil
	}
	occupied := make(map[int]bool, maxRoomMembers)
	for _, member := range current.Members {
		occupied[member.MemberType] = true
	}
	for _, reservation := range current.reservations {
		if reservation.ExpiresAt.After(now) && reservation.MemberType >= 1 && reservation.MemberType <= maxRoomMembers {
			occupied[reservation.MemberType] = true
		}
	}
	for memberType, profession := range roomProfessionSlots(current) {
		if memberType > 0 && profession == arthurType && !occupied[memberType] {
			return memberType, nil
		}
	}
	return 0, fmt.Errorf("%w: member slots reserved", ErrRoomArthurUnavailable)
}

func reserveMemberReservationLocked(connections []*clientConn, memberType int, reserved bool) []frameDelivery {
	if memberType < 1 || memberType > maxRoomMembers {
		return nil
	}
	value := "0"
	if reserved {
		value = "1"
	}
	payload := joinCSV(strconv.Itoa(memberType), value)
	return reserveRoomFramesLocked(connections, battleFrame{"RoomMemberReserve", payload})
}

// Match TeamRoom's pending-entry gate, using the same live reservations that
// are sent to newly joined clients. Stable slot order keeps snapshots coherent.
func roomReservationFrames(current *room, now time.Time) []battleFrame {
	var frames []battleFrame
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		for _, reservation := range current.reservations {
			if reservation.MemberType == memberType && reservation.ExpiresAt.After(now) {
				frames = append(frames, battleFrame{"RoomMemberReserve", joinCSV(strconv.Itoa(memberType), "1")})
				break
			}
		}
	}
	return frames
}

func (h *Hub) scheduleReservationExpiry(roomID int64, userID int, arthurType int, memberType int, expiresAt time.Time) {
	delay := time.Until(expiresAt)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		h.mu.Lock()
		current, exists := h.rooms[roomID]
		if !exists {
			h.mu.Unlock()
			return
		}
		reservation, exists := current.reservations[arthurType]
		if !exists || reservation.UserID != userID || reservation.MemberType != memberType ||
			!reservation.ExpiresAt.Equal(expiresAt) || reservation.ExpiresAt.After(time.Now()) {
			h.mu.Unlock()
			return
		}
		delete(current.reservations, arthurType)
		connections := append([]*clientConn(nil), roomConnections(current)...)
		deliveries := reserveMemberReservationLocked(connections, memberType, false)
		h.mu.Unlock()
		sendRoomFrames(deliveries)
	})
}

func (h *Hub) ReservationFor(userID int) (int64, int64, bool) {
	if userID <= 0 {
		return 0, 0, false
	}
	h.expireReservations(time.Now())
	h.mu.Lock()
	defer h.mu.Unlock()
	for roomID, current := range h.rooms {
		for _, reservation := range current.reservations {
			if reservation.UserID == userID {
				return roomID, reservation.ExpiresAt.Unix(), true
			}
		}
	}
	return 0, 0, false
}

type RoomSearch struct {
	BossID      int
	BossGroupID int
	Password    string
	UserID      int
	ArthurType  int
}

func (h *Hub) List(query RoomSearch) []RoomSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	now := time.Now()
	result := make([]RoomSnapshot, 0, len(h.rooms))
	for _, current := range h.rooms {
		if current.State != RoomStateOpen || len(current.Members) >= maxRoomMembers {
			continue
		}
		if query.BossID > 0 && current.BossID != query.BossID {
			continue
		}
		if query.BossGroupID > 0 && current.BossGroupID != query.BossGroupID {
			continue
		}
		// The client reuses the search password for entry without prompting.
		if current.password() != query.Password {
			continue
		}
		if query.ArthurType != 0 {
			if _, err := roomReservationSlot(current, query.UserID, query.ArthurType, now); err != nil {
				continue
			}
		}
		result = append(result, cloneRoomSnapshot(current.RoomSnapshot))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RoomID < result[j].RoomID })
	return result
}

func (h *Hub) Snapshot(roomID int64) (RoomSnapshot, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	current, exists := h.rooms[roomID]
	if !exists {
		return RoomSnapshot{}, false
	}
	return cloneRoomSnapshot(current.RoomSnapshot), true
}

func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}

func (r *room) password() string {
	if request, ok := r.BossGroup.(roomPrivate); ok {
		return request.password
	}
	return ""
}

// roomPrivate keeps a password out of snapshots returned to the HTTP adapter.
type roomPrivate struct {
	value    any
	password string
}

// validateRoomRequirements is shared by HTTP credential issuance and actual
// BattleSv admission. Listing/filtering in the client is not authorization.
func validateRoomRequirements(snapshot RoomSnapshot, member Member) error {
	if member.DeckRank < snapshot.NeedDeckRank {
		return errors.New("room deck rank requirement is not met")
	}
	if member.HP < snapshot.NeedHP {
		return errors.New("room HP requirement is not met")
	}
	if member.LeaderFame < snapshot.NeedFame {
		return errors.New("room leader fame requirement is not met")
	}
	return nil
}

func validateRoomSpec(spec RoomSpec) error {
	if spec.BattlePointUse < 0 {
		return errors.New("room battle point cost is invalid")
	}
	if spec.BossID <= 0 || spec.BossGroupID < 0 || spec.EnemyPartyID <= 0 || spec.EnemyType < 0 || spec.EnemyType > 4 || spec.Seed < 0 {
		return errors.New("room boss is invalid")
	}
	waves := roomSpecBattles(spec)
	if len(waves) > MaxBattleWaves || waves[0].EnemyPartyID != spec.EnemyPartyID || int(waves[0].EnemyType) != spec.EnemyType || waves[0].EnemyType == 4 {
		return errors.New("room battle waves are invalid")
	}
	for _, wave := range waves {
		if wave.EnemyPartyID <= 0 || wave.EnemyType < 0 || wave.EnemyType > 4 {
			return errors.New("room battle wave is invalid")
		}
	}
	if spec.RoomType < 0 || spec.RoomType > 1 {
		return errors.New("room type is invalid")
	}
	if spec.NeedDeckRank < 0 || spec.NeedHP < 0 || spec.NeedFame < 0 {
		return errors.New("room condition is invalid")
	}
	if spec.GameStartMemberNum == 0 {
		spec.GameStartMemberNum = defaultMemberCount
	}
	if spec.GameStartMemberNum < 1 || spec.GameStartMemberNum > maxRoomMembers {
		return errors.New("room start member count is invalid")
	}
	if spec.CostInitial < 0 || spec.HoldMax <= 0 || spec.BurstGaugeInitial < 0 {
		return errors.New("room battle start configuration is invalid")
	}
	for _, drop := range spec.Drops {
		if drop.EnemyIndex >= maxRoomMembers || !ValidBattleDrop(drop) {
			return errors.New("room battle drop configuration is invalid")
		}
	}
	if spec.DropLedgerVersion < 0 || spec.DropLedgerVersion > 1 || len(spec.DropPlan) > 512 ||
		(spec.DropLedgerVersion == 0 && (len(spec.DropPlan) != 0 || len(waves) > 1)) {
		return errors.New("room drop ledger is invalid")
	}
	firstWaveDrops := make([]BattleDrop, 0)
	for _, drop := range spec.DropPlan {
		wire := BattleDrop{EnemyIndex: drop.EnemyIndex, RewardType: drop.Reward.Type, RewardTypeID: drop.Reward.RewardTypeID, Num: drop.Reward.Num}
		if drop.BattleIndex < 0 || drop.BattleIndex >= len(waves) || drop.ChancePerMillion != nil || wire.EnemyIndex >= maxRoomMembers || !ValidBattleDrop(wire) {
			return errors.New("room drop ledger differs from the engine plan")
		}
		if drop.BattleIndex == 0 {
			firstWaveDrops = append(firstWaveDrops, wire)
		}
	}
	if spec.DropLedgerVersion == 1 {
		if len(firstWaveDrops) != len(spec.Drops) {
			return errors.New("room first wave drops differ from ledger")
		}
		for index := range firstWaveDrops {
			if firstWaveDrops[index] != spec.Drops[index] {
				return errors.New("room first wave drops differ from ledger")
			}
		}
	}
	if len(spec.OwnerFallbackParty) > maxRoomMembers-1 || (spec.AutoStart && len(spec.OwnerFallbackParty) != maxRoomMembers-1) {
		return errors.New("room owner fallback party is incomplete")
	}
	seenArthurTypes := map[int]struct{}{spec.Owner.ArthurType: {}}
	for _, member := range spec.OwnerFallbackParty {
		if err := validateMember(member); err != nil {
			return err
		}
		if member.UserID == spec.Owner.UserID {
			return errors.New("room fallback member must have a distinct character ID")
		}
		if _, exists := seenArthurTypes[member.ArthurType]; exists {
			return errors.New("room owner fallback party repeats an Arthur type")
		}
		seenArthurTypes[member.ArthurType] = struct{}{}
	}
	return validateMember(spec.Owner)
}

func validateMember(member Member) error {
	if member.ArthurType < 1 || member.ArthurType > 4 || member.Level <= 0 || member.DeckRank < 0 {
		return errors.New("room member profile is invalid")
	}
	if member.HP <= 0 || member.LeaderCardID <= 0 || member.LeaderLevel <= 0 || member.LeaderFame <= 0 {
		return errors.New("room member deck is invalid")
	}
	if len(member.PartsIDs) != 7 || len(member.DeckHonorIDs) != 4 {
		return errors.New("room member appearance is invalid")
	}
	if len(member.DeckCards) != 10 {
		return errors.New("room member battle deck is invalid")
	}
	if err := validateSupportCards(member.SupportCards); err != nil {
		return err
	}
	if len(member.DeckSpheres) > deckSphereSlots {
		return errors.New("room member battle sphere deck is invalid")
	}
	seenSphereTypes := make(map[int]struct{}, len(member.DeckSpheres))
	for _, sphere := range member.DeckSpheres {
		if sphere.SphereType < 1 || sphere.SphereType > deckSphereSlots || sphere.SphereID <= 0 || sphere.Level <= 0 {
			return errors.New("room member battle sphere is invalid")
		}
		if _, duplicate := seenSphereTypes[sphere.SphereType]; duplicate {
			return errors.New("room member battle sphere slot is duplicated")
		}
		seenSphereTypes[sphere.SphereType] = struct{}{}
	}
	for index, card := range member.DeckCards {
		if card.CardType != index+1 || card.CardID <= 0 || card.Level <= 0 {
			return errors.New("room member battle card is invalid")
		}
	}
	if len(member.DeckBuddies) > deckBuddySlotCount {
		return errors.New("room member battle buddy deck is invalid")
	}
	seenBuddyTypes := make(map[int]struct{}, len(member.DeckBuddies))
	for _, buddy := range member.DeckBuddies {
		if buddy.BuddyType < 1 || buddy.BuddyType > deckBuddySlotCount || buddy.BuddyID <= 0 || buddy.Level <= 0 {
			return errors.New("room member battle buddy is invalid")
		}
		if _, duplicate := seenBuddyTypes[buddy.BuddyType]; duplicate {
			return errors.New("room member battle buddy slot is duplicated")
		}
		seenBuddyTypes[buddy.BuddyType] = struct{}{}
	}
	return nil
}

func newCredential() (Credential, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Credential{}, fmt.Errorf("generate room credential: %w", err)
	}
	return Credential{
		AuthToken: hex.EncodeToString(bytes[:16]),
		Signature: hex.EncodeToString(bytes[16:]),
	}, nil
}

func (h *Hub) prunePendingLocked(now time.Time) {
	for token, request := range h.pending {
		if !request.ExpiresAt.After(now) {
			delete(h.pending, token)
		}
	}
}

func (h *Hub) expireReservations(now time.Time) {
	expired := make([][]frameDelivery, 0)
	h.mu.Lock()
	for _, current := range h.rooms {
		for arthurType, reservation := range current.reservations {
			if reservation.ExpiresAt.After(now) {
				continue
			}
			delete(current.reservations, arthurType)
			expired = append(expired,
				reserveMemberReservationLocked(roomConnections(current), reservation.MemberType, false))
		}
	}
	h.mu.Unlock()
	for _, expiration := range expired {
		sendRoomFrames(expiration)
	}
}

func (h *Hub) pruneCompletedLocked(now time.Time) {
	for roomID, completed := range h.completed {
		if !completed.expiresAt.After(now) {
			delete(h.completed, roomID)
		}
	}
}

func cloneMember(member Member) Member {
	member.PartsIDs = append([]int(nil), member.PartsIDs...)
	member.DeckHonorIDs = append([]int(nil), member.DeckHonorIDs...)
	member.DeckCards = append([]BattleCard(nil), member.DeckCards...)
	member.SupportCards = append([]BattleCard(nil), member.SupportCards...)
	member.DeckSpheres = append([]BattleSphere(nil), member.DeckSpheres...)
	member.DeckBuddies = append([]BattleBuddy(nil), member.DeckBuddies...)
	return member
}

func cloneRoomSpec(spec RoomSpec) RoomSpec {
	spec.FameRewards = cloneFameRewards(spec.FameRewards)
	spec.Battles = append([]gamestate.TeamBattleReplayBattle(nil), spec.Battles...)
	spec.DropPlan = cloneDropPlan(spec.DropPlan)
	spec.Owner = cloneMember(spec.Owner)
	spec.Drops = append([]BattleDrop(nil), spec.Drops...)
	spec.OwnerFallbackParty = append([]Member(nil), spec.OwnerFallbackParty...)
	for index := range spec.OwnerFallbackParty {
		spec.OwnerFallbackParty[index] = cloneMember(spec.OwnerFallbackParty[index])
	}
	return spec
}

func cloneRoomSnapshot(snapshot RoomSnapshot) RoomSnapshot {
	snapshot.Members = append([]Member(nil), snapshot.Members...)
	for index := range snapshot.Members {
		snapshot.Members[index] = cloneMember(snapshot.Members[index])
	}
	if private, ok := snapshot.BossGroup.(roomPrivate); ok {
		snapshot.BossGroup = private.value
	}
	return snapshot
}

func cloneCompletedBattle(completed CompletedBattle) CompletedBattle {
	completed.FameRewards = cloneFameRewards(completed.FameRewards)
	completed.ReleasedDrops = cloneDropPlan(completed.ReleasedDrops)
	completed.Members = append([]Member(nil), completed.Members...)
	for index := range completed.Members {
		completed.Members[index] = cloneMember(completed.Members[index])
	}
	completed.OnlineUserIDs = append([]int(nil), completed.OnlineUserIDs...)
	return completed
}

func cloneFameRewards(source []gamestate.Reward) []gamestate.Reward {
	result := slices.Clone(source)
	for i := range result {
		result[i].CardSkillLevels = slices.Clone(result[i].CardSkillLevels)
	}
	return result
}

func cloneDropPlan(source []gamestate.TeamBattleEnemyDrop) []gamestate.TeamBattleEnemyDrop {
	result := append([]gamestate.TeamBattleEnemyDrop(nil), source...)
	for index := range result {
		result[index].Reward.CardSkillLevels = slices.Clone(result[index].Reward.CardSkillLevels)
	}
	return result
}
