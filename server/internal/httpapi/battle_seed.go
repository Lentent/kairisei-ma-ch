package httpapi

import "math/rand/v2"

// CN battle seeds are positive Int32 values. Allocate once per accepted solo
// run or room credential, then retain that value for retries and every peer.
// Replay fixtures and scripted tutorials keep their reproducible seed.
func newBattleSeed() int {
	return rand.IntN(2147483647) + 1
}
