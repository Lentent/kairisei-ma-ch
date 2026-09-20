package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (accounts *Accounts) ListFriendPointAccountRelations(
	userID int,
) ([]game.FriendPointAccountRelation, error) {
	if userID < PrimaryUserID {
		return nil, errors.New("invalid CN local-account relationship owner")
	}
	states, err := accounts.listFriendPointPartnerAccounts(userID)
	if err != nil {
		return nil, err
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(
		context.Background(),
		`SELECT follower_user_id, followed_user_id
		 FROM cn_local_account_follow
		 WHERE follower_user_id = ? OR followed_user_id = ?`,
		userID,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list CN local-account relationships: %w", err)
	}
	defer rows.Close()
	outbound := make(map[int]struct{})
	inbound := make(map[int]struct{})
	for rows.Next() {
		var followerUserID int
		var followedUserID int
		if err := rows.Scan(&followerUserID, &followedUserID); err != nil {
			return nil, fmt.Errorf("scan CN local-account relationship: %w", err)
		}
		if followerUserID == userID {
			outbound[followedUserID] = struct{}{}
		}
		if followedUserID == userID {
			inbound[followerUserID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CN local-account relationships: %w", err)
	}
	result := make([]game.FriendPointAccountRelation, 0, len(states))
	for _, state := range states {
		targetUserID := state.User.UserID
		_, follows := outbound[targetUserID]
		_, followedBy := inbound[targetUserID]
		friendState := int8(0)
		switch {
		case follows && followedBy:
			friendState = 3
		case follows:
			friendState = 5
		case followedBy:
			friendState = 6
		}
		result = append(result, game.FriendPointAccountRelation{
			State:       state,
			FriendState: friendState,
			System:      isSystemPartnerUserID(targetUserID),
		})
	}
	return result, nil
}

// FriendPointAccountStates reads only the compact relationship table. Room
// search and battle settlement already own the relevant account identities;
// decoding every multi-megabyte account snapshot here would make latency grow
// with every unrelated local account.
func (accounts *Accounts) FriendPointAccountStates(
	userID int,
	targetUserIDs []int,
) (map[int]int8, error) {
	if userID < PrimaryUserID {
		return nil, errors.New("invalid CN local-account relationship owner")
	}
	states := make(map[int]int8, len(targetUserIDs))
	targets := make(map[int]struct{}, len(targetUserIDs))
	for _, targetUserID := range targetUserIDs {
		if targetUserID < PrimaryUserID {
			return nil, errors.New("invalid CN local-account relationship target")
		}
		if targetUserID == userID {
			continue
		}
		targets[targetUserID] = struct{}{}
		states[targetUserID] = 0
	}
	if len(targets) == 0 {
		return states, nil
	}

	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(
		context.Background(),
		`SELECT follower_user_id, followed_user_id
		 FROM cn_local_account_follow
		 WHERE follower_user_id = ? OR followed_user_id = ?`,
		userID,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list targeted CN local-account relationships: %w", err)
	}
	defer rows.Close()
	outbound := make(map[int]struct{}, len(targets))
	inbound := make(map[int]struct{}, len(targets))
	for rows.Next() {
		var followerUserID int
		var followedUserID int
		if err := rows.Scan(&followerUserID, &followedUserID); err != nil {
			return nil, fmt.Errorf("scan targeted CN local-account relationship: %w", err)
		}
		if followerUserID == userID {
			if _, wanted := targets[followedUserID]; wanted {
				outbound[followedUserID] = struct{}{}
			}
		}
		if followedUserID == userID {
			if _, wanted := targets[followerUserID]; wanted {
				inbound[followerUserID] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate targeted CN local-account relationships: %w", err)
	}
	for targetUserID := range targets {
		_, follows := outbound[targetUserID]
		_, followedBy := inbound[targetUserID]
		switch {
		case follows && followedBy:
			states[targetUserID] = 3
		case follows:
			states[targetUserID] = 5
		case followedBy:
			states[targetUserID] = 6
		}
	}
	return states, nil
}

func (accounts *Accounts) FollowFriendPointAccounts(
	userID int,
	targetUserIDs []int,
	followMaximum int,
	friendMaximum int,
) (game.FollowAddResult, error) {
	if userID < PrimaryUserID || followMaximum <= 0 || friendMaximum <= 0 {
		return game.FollowAddResult{}, errors.New("invalid CN local-account follow selection")
	}
	// The result/search UI can submit an empty selection. It is a no-op.
	if len(targetUserIDs) == 0 {
		return game.FollowAddResult{RequestUserIDs: []int{}}, nil
	}
	available, err := accounts.listFriendPointPartnerAccounts(userID)
	if err != nil {
		return game.FollowAddResult{}, err
	}
	validTargets := make(map[int]gamestate.State, len(available))
	for _, state := range available {
		if strings.TrimSpace(state.User.Name) != "" {
			validTargets[state.User.UserID] = state
		}
	}
	seen := make(map[int]struct{}, len(targetUserIDs))
	selected := make([]int, 0, len(targetUserIDs))
	for _, targetUserID := range targetUserIDs {
		if targetUserID == userID {
			return game.FollowAddResult{}, &game.FollowAddError{ResultCode: -600, ResultString: "不能关注自己。"}
		}
		if _, exists := validTargets[targetUserID]; !exists {
			return game.FollowAddResult{}, &game.FollowAddError{ResultCode: -601, ResultString: "该玩家暂不可关注，请刷新列表后重试。"}
		}
		if _, duplicate := seen[targetUserID]; duplicate {
			continue
		}
		seen[targetUserID] = struct{}{}
		selected = append(selected, targetUserID)
	}
	targetUserIDs = selected
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	database, err := accounts.storage.Open()
	if err != nil {
		return game.FollowAddResult{}, err
	}
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return game.FollowAddResult{}, fmt.Errorf("begin CN local-account follow: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	var existingCount int
	if err := transaction.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM cn_local_account_follow WHERE follower_user_id = ?`,
		userID,
	).Scan(&existingCount); err != nil {
		return game.FollowAddResult{}, fmt.Errorf("count CN local-account follows: %w", err)
	}
	if existingCount+len(targetUserIDs) > followMaximum {
		return game.FollowAddResult{}, &game.FollowAddError{
			ResultCode:   -3410,
			ResultString: "关注人数已达上限。",
		}
	}
	mutualCount, err := mutualFriendCount(transaction, userID)
	if err != nil {
		return game.FollowAddResult{}, err
	}
	newMutualCount := mutualCount
	for _, targetUserID := range targetUserIDs {
		var alreadyFollows int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM cn_local_account_follow
			 WHERE follower_user_id = ? AND followed_user_id = ?`,
			userID,
			targetUserID,
		).Scan(&alreadyFollows); err != nil {
			return game.FollowAddResult{}, fmt.Errorf("check CN local-account follow: %w", err)
		}
		if alreadyFollows != 0 {
			return game.FollowAddResult{}, &game.FollowAddError{
				ResultCode:   -3411,
				ResultString: "已经关注该玩家。",
			}
		}
		var followerCount int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM cn_local_account_follow WHERE followed_user_id = ?`,
			targetUserID,
		).Scan(&followerCount); err != nil {
			return game.FollowAddResult{}, fmt.Errorf("count CN local-account followers: %w", err)
		}
		// The CN client exposes a 50-entry received-follow list. The local
		// profile uses the same configured follow maximum in both directions.
		if followerCount >= followMaximum {
			return game.FollowAddResult{}, &game.FollowAddError{
				ResultCode:   -3412,
				ResultString: "对方的粉丝人数已达上限。",
			}
		}
		var reverseFollow int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM cn_local_account_follow
			 WHERE follower_user_id = ? AND followed_user_id = ?`,
			targetUserID,
			userID,
		).Scan(&reverseFollow); err != nil {
			return game.FollowAddResult{}, fmt.Errorf("check reverse CN local-account follow: %w", err)
		}
		if reverseFollow == 0 {
			continue
		}
		if newMutualCount >= friendMaximum {
			return game.FollowAddResult{}, &game.FollowAddError{
				// FollowAdd's native dialog closes for -3410, whereas the
				// old friend-request code -3400 sends the player to title.
				ResultCode:   -3410,
				ResultString: "好友人数已达上限。",
			}
		}
		targetMutualCount, err := mutualFriendCount(transaction, targetUserID)
		if err != nil {
			return game.FollowAddResult{}, err
		}
		if targetMutualCount >= validTargets[targetUserID].User.FriendMax {
			return game.FollowAddResult{}, &game.FollowAddError{
				ResultCode:   -3412,
				ResultString: "对方的好友人数已达上限。",
			}
		}
		newMutualCount++
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, targetUserID := range targetUserIDs {
		result, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_local_account_follow
			 (follower_user_id, followed_user_id, created_utc) VALUES (?, ?, ?)
			 ON CONFLICT(follower_user_id, followed_user_id) DO NOTHING`,
			userID,
			targetUserID,
			now,
		)
		if err != nil {
			return game.FollowAddResult{}, fmt.Errorf("record CN local-account follow: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return game.FollowAddResult{}, errors.New("CN local account follow insert did not affect exactly one row")
		}
	}
	if err := transaction.Commit(); err != nil {
		return game.FollowAddResult{}, fmt.Errorf("commit CN local-account follow: %w", err)
	}
	committed = true
	return game.FollowAddResult{
		RequestUserIDs: append([]int(nil), targetUserIDs...),
		IsFriendFull:   newMutualCount >= friendMaximum,
	}, nil
}

func mutualFriendCount(transaction *sql.Tx, userID int) (int, error) {
	var count int
	err := transaction.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*)
		 FROM cn_local_account_follow AS outbound
		 WHERE outbound.follower_user_id = ?
		   AND EXISTS (
		     SELECT 1 FROM cn_local_account_follow AS inbound
		     WHERE inbound.follower_user_id = outbound.followed_user_id
		       AND inbound.followed_user_id = outbound.follower_user_id
		   )`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count CN local-account mutual friends: %w", err)
	}
	return count, nil
}

func (accounts *Accounts) UnfollowFriendPointAccount(userID int, targetUserID int) error {
	if userID < PrimaryUserID || targetUserID < PrimaryUserID || userID == targetUserID {
		return &game.FollowAddError{ResultCode: -601, ResultString: "无效的取消关注对象，请刷新列表。"}
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	result, err := database.ExecContext(
		context.Background(),
		`DELETE FROM cn_local_account_follow
		 WHERE follower_user_id = ? AND followed_user_id = ?`,
		userID,
		targetUserID,
	)
	if err != nil {
		return fmt.Errorf("delete CN local-account follow: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read CN unfollow affected rows: %w", err)
	}
	if changed > 1 {
		return errors.New("CN unfollow changed multiple relationships")
	}
	// Repeating a successful cancellation already has the requested state.
	return nil
}

func (accounts *Accounts) ListFriendPointRentalEvents(
	userID int,
	afterEventID int64,
) ([]game.FriendPointRentalEvent, error) {
	if userID < PrimaryUserID || afterEventID < 0 {
		return nil, errors.New("invalid CN friend-point inbox cursor")
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(
		context.Background(),
		`SELECT event_id, renter_user_id, friend_point
		 FROM cn_friend_point_rental
		 WHERE owner_user_id = ? AND event_id > ?
		 ORDER BY event_id`,
		userID,
		afterEventID,
	)
	if err != nil {
		return nil, fmt.Errorf("list CN friend-point rental events: %w", err)
	}
	defer rows.Close()
	events := make([]game.FriendPointRentalEvent, 0)
	for rows.Next() {
		var event game.FriendPointRentalEvent
		if err := rows.Scan(&event.EventID, &event.RenterUserID, &event.FriendPoint); err != nil {
			return nil, fmt.Errorf("scan CN friend-point rental event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CN friend-point rental events: %w", err)
	}
	return events, nil
}

func (accounts *Accounts) RecordFriendPointRentals(
	eventKey string,
	renterUserID int,
	credits []game.FriendPointRentalCredit,
	bossID int,
) error {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" || len(eventKey) > 256 || renterUserID < PrimaryUserID ||
		bossID <= 0 || len(credits) == 0 || len(credits) > 3 {
		return errors.New("invalid CN friend-point rental event")
	}
	seen := make(map[int]struct{}, len(credits))
	for _, credit := range credits {
		if credit.OwnerUserID < PrimaryUserID || credit.OwnerUserID == renterUserID ||
			credit.FriendPoint <= 0 {
			return errors.New("invalid CN friend-point rental owner")
		}
		if _, duplicate := seen[credit.OwnerUserID]; duplicate {
			return errors.New("duplicate CN friend-point rental owner")
		}
		seen[credit.OwnerUserID] = struct{}{}
	}

	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin CN friend-point rental event: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, credit := range credits {
		var exists int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT 1 FROM cn_local_account WHERE user_id = ?`,
			credit.OwnerUserID,
		).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("CN friend-point rental owner %d does not exist", credit.OwnerUserID)
			}
			return fmt.Errorf("check CN friend-point rental owner: %w", err)
		}
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_friend_point_rental
			 (event_key, owner_user_id, renter_user_id, boss_id, friend_point, created_utc)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(event_key, owner_user_id) DO NOTHING`,
			eventKey,
			credit.OwnerUserID,
			renterUserID,
			bossID,
			credit.FriendPoint,
			now,
		); err != nil {
			return fmt.Errorf("record CN friend-point rental event: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit CN friend-point rental event: %w", err)
	}
	committed = true
	return nil
}
