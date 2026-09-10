package httpapi

import (
	"errors"
	"math"
	"sort"

	"kairisei.local/server/internal/release"
)

// FriendPointRentalEvent is a durable local-account event. EventID is the
// account inbox order; the account snapshot persists the last applied ID so a
// crash cannot make HomeShow grant the same event twice.
type FriendPointRentalEvent struct {
	EventID      int64
	RenterUserID int
	FriendPoint  int
}

// FriendPointRentalCredit preserves the amount shown for one selected
// account-backed partner. The amount can differ for a mutual friend, so the
// settlement must not collapse credits into a single per-partner value.
type FriendPointRentalCredit struct {
	OwnerUserID int
	FriendPoint int
}

type FriendPointAccountRelation struct {
	State       release.State
	FriendState int8
	System      bool
}

// FollowAddResult is the protocol-visible outcome of one atomic follow batch.
// IsFriendFull is raised after a successful batch that fills the requester's
// mutual-friend capacity; capacity failures are returned as FollowAddError.
type FollowAddResult struct {
	RequestUserIDs []int
	IsFriendFull   bool
}

// FollowAddError preserves the original-client result code needed for its
// native friend-capacity dialog. It is intentionally a domain error rather
// than an HTTP status: the client parses these errors from a normal protocol
// response and keeps the current scene open.
type FollowAddError struct {
	ResultCode   int
	ResultString string
}

func (err *FollowAddError) Error() string {
	return err.ResultString
}

// FriendPointAccountRepository keeps cross-account partner discovery and the
// rental inbox outside an individual account handler. Static partner decks
// remain in account snapshots; only successful rental events enter the inbox.
type FriendPointAccountRepository interface {
	ListFriendPointAccountRelations(userID int) ([]FriendPointAccountRelation, error)
	FriendPointAccountStates(userID int, targetUserIDs []int) (map[int]int8, error)
	FollowFriendPointAccounts(
		userID int,
		targetUserIDs []int,
		followMaximum int,
		friendMaximum int,
	) (FollowAddResult, error)
	UnfollowFriendPointAccount(userID int, targetUserID int) error
	ListFriendPointRentalEvents(userID int, afterEventID int64) ([]FriendPointRentalEvent, error)
	RecordFriendPointRentals(eventKey string, renterUserID int, credits []FriendPointRentalCredit, bossID int) error
}

type friendPointHomeReward struct {
	FromUserNum int
	FriendPoint int
}

func (s *store) friendPointRentalCursor() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.friendPointInboxCursor
}

func (s *store) claimFriendPointRentalEvents(events []FriendPointRentalEvent) (*friendPointHomeReward, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(events) == 0 {
		return nil, nil
	}
	ordered := append([]FriendPointRentalEvent(nil), events...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].EventID < ordered[right].EventID
	})
	fromUsers := make(map[int]struct{}, len(ordered))
	total := 0
	lastEventID := s.friendPointInboxCursor
	for _, event := range ordered {
		if event.EventID <= lastEventID || event.RenterUserID <= 0 || event.FriendPoint <= 0 {
			return nil, errors.New("friend-point rental inbox contains an invalid event")
		}
		if event.FriendPoint > math.MaxInt-total {
			return nil, errors.New("friend-point rental inbox overflows")
		}
		total += event.FriendPoint
		fromUsers[event.RenterUserID] = struct{}{}
		lastEventID = event.EventID
	}
	reward := release.Reward{Type: 9, Num: total, CardSkillLevels: []int16{}}
	if err := s.validateRewardBatchCapacityLocked([]release.Reward{reward}); err != nil {
		return nil, err
	}
	if err := s.applyRewardLocked(reward, &presentReceiveResult{}); err != nil {
		return nil, err
	}
	s.friendPointInboxCursor = lastEventID
	return &friendPointHomeReward{FromUserNum: len(fromUsers), FriendPoint: total}, nil
}
