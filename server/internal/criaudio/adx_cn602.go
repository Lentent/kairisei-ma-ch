package criaudio

import "encoding/binary"

// encodeCN602ADX matches the global Atom decrypter configured by the CN602
// client's CriWareInitializer. The legacy ADX path applies that key even when
// the ADX revision byte is zero. Plain ADX therefore becomes loud noise in the
// game despite decoding correctly in standalone tools and the WAV preview.
//
// These are audio-format parameters from the shipped client, not account keys.
// Keep the revision at zero: revisions 8 and 9 select different key slots in
// this CRI runtime. The header, residual nibbles and end marker stay unchanged.
func encodeCN602ADX(w WAV) []byte {
	adx := EncodeADX(w)
	const clientAudioKey uint64 = 1782351729464341796
	key := clientAudioKey - 1
	state := uint16((key >> 27) & 0x7fff)
	multiplier := uint16(((key>>14)&0x1fff)*4 + 1)
	increment := uint16((key&0x3fff)*2 + 1)
	start := int(binary.BigEndian.Uint16(adx[2:])) + 4
	// Advance once per channel block, including silent and padded blocks.
	for p := start; p < len(adx)-18; p += 18 {
		scale := binary.BigEndian.Uint16(adx[p:])
		binary.BigEndian.PutUint16(adx[p:], scale^(state&0x1fff))
		state = (state*multiplier + increment) & 0x7fff
	}
	return adx
}
