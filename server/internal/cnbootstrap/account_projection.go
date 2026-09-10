package cnbootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/internal/release"
)

const (
	cnAccountProjectionSchemaVersion = 2
	maxCNAccountProjectionBytes      = 512 * 1024
)

// cnAccountProjection is the small cross-account view used by partner and PVP
// discovery. The authoritative account snapshot remains the only mutable state;
// this row is replaced in the same transaction whenever that snapshot changes.
// Keeping the projection explicit prevents list endpoints from decoding every
// account's multi-megabyte quest and battle catalogs.
type cnAccountProjection struct {
	SchemaVersion int                          `json:"schema_version"`
	User          release.User                 `json:"user"`
	PVP           release.PVPPlayerState       `json:"pvp"`
	Spheres       []release.Sphere             `json:"spheres"`
	Cards         []cnProjectionCard           `json:"cards"`
	Decks         []release.Deck               `json:"decks"`
	Avatars       []release.Avatar             `json:"avatars"`
	Buddy         release.Buddy                `json:"buddy"`
	Buddies       []release.Buddy              `json:"buddies"`
	Honors        release.HonorCollectionState `json:"honors"`
	SupportDeck   release.SupportDeckState     `json:"support_deck"`
}

func accountProjectionFromState(state release.State) cnAccountProjection {
	projection := cnAccountProjection{
		SchemaVersion: cnAccountProjectionSchemaVersion,
		User:          state.User,
		PVP:           cloneReleasePVPState(release.PVPPlayerState{DefenseDecks: state.PVP.DefenseDecks}),
		Spheres:       []release.Sphere{},
		Cards:         []cnProjectionCard{},
		Decks:         append([]release.Deck(nil), state.Decks...),
		Avatars:       append([]release.Avatar(nil), state.Avatars...),
		Buddy:         state.Buddy,
		Buddies:       []release.Buddy{},
		Honors:        release.HonorCollectionState{DeckHonorIDs: append([]int(nil), state.Honors.DeckHonorIDs...)},
		SupportDeck:   release.SupportDeckState{UnlockSlotNums: append([]int8(nil), state.SupportDeck.UnlockSlotNums...)},
	}
	cardIDs, sphereIDs, buddyIDs := map[int64]bool{}, map[int64]bool{}, map[int64]bool{}
	collect := func(cards, support, spheres, buddies []int64) {
		for _, id := range cards {
			cardIDs[id] = true
		}
		for _, id := range support {
			cardIDs[id] = true
		}
		for _, id := range spheres {
			sphereIDs[id] = true
		}
		for _, id := range buddies {
			buddyIDs[id] = true
		}
	}
	cardIDs[state.User.LeaderCardUniqueID] = true
	for _, deck := range state.Decks {
		collect(deck.CardUniqueIDs, deck.SupportCardUniqueIDs, deck.SphereUniqueIDs, deck.BuddyUniqueIDs)
	}
	for _, deck := range state.PVP.DefenseDecks {
		collect(deck.CardUniqueIDs, deck.SupportCardUniqueIDs, deck.SphereUniqueIDs, deck.BuddyUniqueIDs)
	}
	for _, card := range state.Cards {
		if cardIDs[card.UniqueID] {
			projection.Cards = append(projection.Cards, projectCNCard(card))
		}
	}
	for _, sphere := range state.Spheres {
		if sphereIDs[sphere.UniqueID] {
			projection.Spheres = append(projection.Spheres, sphere)
		}
	}
	for _, buddy := range state.Buddies {
		if buddyIDs[buddy.UniqueID] {
			projection.Buddies = append(projection.Buddies, buddy)
		}
	}
	return projection
}

func (projection cnAccountProjection) state() release.State {
	return release.State{
		User:        projection.User,
		PVP:         cloneReleasePVPState(projection.PVP),
		Spheres:     append([]release.Sphere{}, projection.Spheres...),
		Cards:       restoreCNProjectionCards(projection.Cards),
		Decks:       append([]release.Deck(nil), projection.Decks...),
		Avatars:     append([]release.Avatar(nil), projection.Avatars...),
		Buddy:       projection.Buddy,
		Buddies:     append([]release.Buddy{}, projection.Buddies...),
		Honors:      projection.Honors,
		SupportDeck: cloneSupportDeckState(projection.SupportDeck),
	}
}

func encodeCNAccountProjection(state release.State) ([]byte, string, error) {
	if state.User.UserID < cnPrimaryUserID {
		return nil, "", errors.New("CN account projection user ID is invalid")
	}
	content, err := json.Marshal(accountProjectionFromState(state))
	if err != nil {
		return nil, "", fmt.Errorf("encode CN account projection: %w", err)
	}
	if len(content) == 0 || len(content) > maxCNAccountProjectionBytes {
		return nil, "", errors.New("CN account projection is empty or exceeds the size limit")
	}
	digest := sha256.Sum256(content)
	return content, hex.EncodeToString(digest[:]), nil
}

func decodeCNAccountProjection(content []byte, expectedDigest string) (release.State, error) {
	if len(content) == 0 || len(content) > maxCNAccountProjectionBytes {
		return release.State{}, errors.New("CN account projection is empty or exceeds the size limit")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return release.State{}, errors.New("CN account projection digest mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var projection cnAccountProjection
	if err := decoder.Decode(&projection); err != nil {
		return release.State{}, fmt.Errorf("decode CN account projection: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return release.State{}, err
	}
	if projection.SchemaVersion != cnAccountProjectionSchemaVersion ||
		projection.User.UserID < cnPrimaryUserID {
		return release.State{}, errors.New("CN account projection identity is invalid")
	}
	return projection.state(), nil
}

func upsertCNAccountProjection(
	transaction *sql.Tx,
	userID int,
	snapshotRevision int,
	updatedUTC string,
	state release.State,
) error {
	if transaction == nil || userID < cnPrimaryUserID || state.User.UserID != userID ||
		snapshotRevision <= 0 || updatedUTC == "" {
		return errors.New("invalid CN account projection update")
	}
	content, digest, err := encodeCNAccountProjection(state)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(
		context.Background(),
		`INSERT INTO cn_account_projection
		 (user_id, schema_version, snapshot_revision, updated_utc, payload_json, payload_sha256)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		  schema_version = excluded.schema_version,
		  snapshot_revision = excluded.snapshot_revision,
		  updated_utc = excluded.updated_utc,
		  payload_json = excluded.payload_json,
		  payload_sha256 = excluded.payload_sha256`,
		userID,
		cnAccountProjectionSchemaVersion,
		snapshotRevision,
		updatedUTC,
		content,
		digest,
	); err != nil {
		return fmt.Errorf("write CN account projection: %w", err)
	}
	return nil
}

func (storage *cnSaveDatabase) syncCNAccountProjections(transaction *sql.Tx) error {
	if transaction == nil {
		return errors.New("CN account projection transaction is required")
	}
	rows, err := transaction.QueryContext(
		context.Background(),
		`SELECT account.user_id
		 FROM cn_local_account AS account
		 LEFT JOIN cn_save_snapshot AS primary_snapshot
		   ON account.user_id = ? AND primary_snapshot.singleton = 1
		 LEFT JOIN cn_account_snapshot AS account_snapshot
		   ON account_snapshot.user_id = account.user_id
		 LEFT JOIN cn_account_projection AS projection
		   ON projection.user_id = account.user_id
		 WHERE projection.user_id IS NULL OR projection.schema_version <> ? OR
		       projection.snapshot_revision <> CASE
		        WHEN account.user_id = ? THEN primary_snapshot.revision
		        ELSE account_snapshot.revision END
		 ORDER BY account.user_id`,
		cnPrimaryUserID,
		cnAccountProjectionSchemaVersion,
		cnPrimaryUserID,
	)
	if err != nil {
		return fmt.Errorf("list stale CN account projections: %w", err)
	}
	var userIDs []int
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan stale CN account projection: %w", err)
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate stale CN account projections: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close stale CN account projection rows: %w", err)
	}

	for _, userID := range userIDs {
		query := `SELECT revision, updated_utc, payload_json, payload_sha256
			FROM cn_account_snapshot WHERE user_id = ?`
		arguments := []any{userID}
		if userID == cnPrimaryUserID {
			query = `SELECT revision, updated_utc, payload_json, payload_sha256
				FROM cn_save_snapshot WHERE singleton = 1`
			arguments = nil
		}
		var revision int
		var updatedUTC string
		var content []byte
		var expectedDigest string
		if err := transaction.QueryRowContext(
			context.Background(),
			query,
			arguments...,
		).Scan(&revision, &updatedUTC, &content, &expectedDigest); err != nil {
			return fmt.Errorf("read CN account snapshot %d for projection: %w", userID, err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != expectedDigest {
			return fmt.Errorf("CN account snapshot %d digest mismatch", userID)
		}
		state, err := storage.decodeAccount(transaction, content)
		if err != nil {
			return fmt.Errorf("decode CN account snapshot %d for projection: %w", userID, err)
		}
		if state.User.UserID != userID {
			return fmt.Errorf("CN account snapshot %d has a mismatched user ID", userID)
		}
		if err := upsertCNAccountProjection(transaction, userID, revision, updatedUTC, state); err != nil {
			return fmt.Errorf("refresh CN account projection %d: %w", userID, err)
		}
	}
	return nil
}
