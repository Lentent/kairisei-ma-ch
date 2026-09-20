package cnbootstrap

import (
	"crypto/sha256"
	"encoding/json"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

// Master application can mutate slices in place. capture a digest beforehand,
// streaming inventory one record at a time instead of cloning/encoding one
// full account document. This is only used when constructing a handler.
func fingerprintCNAccountState(state gamestate.State) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	metadata, err := accountstore.EncodeAccountMetadata(state)
	if err != nil {
		return result, err
	}
	digest := sha256.New()
	digest.Write(metadata)
	encoder := json.NewEncoder(digest)
	u := state.User
	if err := encoder.Encode([5]int{u.Gold, u.Coin, u.CoinFree, u.FriendPoint, u.PVPPoint}); err != nil {
		return result, err
	}
	for _, cards := range [][]gamestate.Card{state.Cards, state.ContainerCards} {
		if err := fingerprintCNRecords(encoder, cards); err != nil {
			return result, err
		}
	}
	if err := fingerprintCNRecords(encoder, state.Items); err != nil {
		return result, err
	}
	if err := fingerprintCNRecords(encoder, state.StackCards); err != nil {
		return result, err
	}
	for _, presents := range [][]gamestate.Present{state.Engagement.Presents, state.Engagement.Histories} {
		if err := fingerprintCNRecords(encoder, presents); err != nil {
			return result, err
		}
	}
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func fingerprintCNRecords[T any](encoder *json.Encoder, records []T) error {
	if err := encoder.Encode(len(records)); err != nil {
		return err
	}
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return nil
}
