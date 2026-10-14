package criaudio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCN602ADXGlobalDecrypter(t *testing.T) {
	// First twelve block masks from the installed client's key schedule.
	// A stereo stream advances between channels, not just between frames.
	masks := []uint16{3724, 1923, 2294, 853, 4816, 5719, 666, 265, 2772, 4075, 6654, 7037}
	for _, channels := range []int{1, 2} {
		w, err := ParseWAV(testWAV(channels, 6*32-3))
		if err != nil {
			t.Fatal(err)
		}
		for _, silent := range []bool{false, true} {
			if silent {
				clear(w.Samples)
			}
			plain, game := EncodeADX(w), encodeCN602ADX(w)
			start := int(binary.BigEndian.Uint16(plain[2:])) + 4
			if !bytes.Equal(plain[:start], game[:start]) || !bytes.Equal(plain[len(plain)-18:], game[len(game)-18:]) {
				t.Fatal("encryption changed header or end marker")
			}
			for p, block := start, 0; p < len(game)-18; p, block = p+18, block+1 {
				scale := binary.BigEndian.Uint16(game[p:])
				if scale&0xe000 != 0 {
					t.Fatal("invalid encrypted ADX scale")
				}
				binary.BigEndian.PutUint16(game[p:], scale^masks[block])
			}
			if !bytes.Equal(game, plain) {
				t.Fatal("client decrypter does not recover original ADX blocks")
			}
		}
	}
}
