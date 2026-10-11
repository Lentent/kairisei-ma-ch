// Package criaudio builds short, memory-resident voice cues for the existing
// CN client. It accepts bounded PCM WAV data and needs no external encoder.
package criaudio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

type WAV struct {
	Rate, Channels, Frames int
	Samples                []int16
}

func ParseWAV(raw []byte) (WAV, error) {
	var w WAV
	bad := errors.New("自制语音须为30秒以内、8–48kHz、单／双声道的16位PCM WAV，单段最多4MB")
	if len(raw) < 44 || len(raw) > 4<<20 || !bytes.Equal(raw[:4], []byte("RIFF")) || string(raw[8:12]) != "WAVE" || int(binary.LittleEndian.Uint32(raw[4:8])) != len(raw)-8 {
		return w, bad
	}
	var format, data []byte
	for p := 12; p < len(raw); {
		if p+8 > len(raw) {
			return w, bad
		}
		n := int(binary.LittleEndian.Uint32(raw[p+4 : p+8]))
		end := p + 8 + n
		if end+n%2 > len(raw) {
			return w, bad
		}
		switch string(raw[p : p+4]) {
		case "fmt ":
			if format != nil {
				return w, bad
			}
			format = raw[p+8 : end]
		case "data":
			if data != nil {
				return w, bad
			}
			data = raw[p+8 : end]
		}
		p = end + n%2
	}
	if len(format) < 16 || len(data) == 0 || binary.LittleEndian.Uint16(format) != 1 || binary.LittleEndian.Uint16(format[14:]) != 16 {
		return w, bad
	}
	w.Channels = int(binary.LittleEndian.Uint16(format[2:]))
	w.Rate = int(binary.LittleEndian.Uint32(format[4:]))
	if w.Channels < 1 || w.Channels > 2 || w.Rate < 8000 || w.Rate > 48000 || len(data)%(w.Channels*2) != 0 || int(binary.LittleEndian.Uint16(format[12:])) != w.Channels*2 || int(binary.LittleEndian.Uint32(format[8:])) != w.Rate*w.Channels*2 {
		return WAV{}, bad
	}
	w.Frames = len(data) / (w.Channels * 2)
	if w.Frames == 0 || w.Frames > w.Rate*30 {
		return WAV{}, bad
	}
	w.Samples = make([]int16, len(data)/2)
	for i := range w.Samples {
		w.Samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return w, nil
}

// Standard ADX: 32 samples per 18-byte block, signed 4-bit residuals,
// a 500 Hz predictor and a version-3 non-looping header. Quantization is
// performed against reconstructed history so errors do not accumulate.
func EncodeADX(w WAV) []byte {
	blocks := (w.Frames + 31) / 32
	out := make([]byte, 36+blocks*w.Channels*18+18)
	be := binary.BigEndian
	be.PutUint16(out, 0x8000)
	be.PutUint16(out[2:], 32)
	out[4], out[5], out[6], out[7] = 3, 18, 4, byte(w.Channels)
	be.PutUint32(out[8:], uint32(w.Rate))
	be.PutUint32(out[12:], uint32(w.Frames))
	be.PutUint16(out[16:], 500)
	out[18] = 3
	copy(out[30:], "(c)CRI")
	a := math.Sqrt2 - math.Cos(2*math.Pi*500/float64(w.Rate))
	b := math.Sqrt2 - 1
	c := (a - math.Sqrt(a*a-b*b)) / b
	c1, c2 := int(math.Round(c*2*4096)), int(math.Round(-c*c*4096))
	history := make([][2]int, w.Channels)
	for block := 0; block < blocks; block++ {
		for channel := 0; channel < w.Channels; channel++ {
			var samples [32]int
			for i := range samples {
				frame := block*32 + i
				if frame < w.Frames {
					samples[i] = int(w.Samples[frame*w.Channels+channel])
				}
			}
			h1, h2 := history[channel][0], history[channel][1]
			lo, hi := 0, 0
			for _, sample := range samples {
				residual := sample - ((c1*h1 + c2*h2) >> 12)
				lo = min(lo, residual)
				hi = max(hi, residual)
				h2, h1 = h1, sample
			}
			scale := max(1, (hi+6)/7, (-lo+7)/8)
			p := 36 + (block*w.Channels+channel)*18
			be.PutUint16(out[p:], uint16(scale))
			h1, h2 = history[channel][0], history[channel][1]
			for i, sample := range samples {
				prediction := (c1*h1 + c2*h2) >> 12
				q := max(-8, min(7, int(math.Round(float64(sample-prediction)/float64(scale)))))
				if i%2 == 0 {
					out[p+2+i/2] = byte(q&15) << 4
				} else {
					out[p+2+i/2] |= byte(q & 15)
				}
				h2, h1 = h1, max(-32768, min(32767, prediction+q*scale))
			}
			history[channel] = [2]int{h1, h2}
		}
	}
	p := len(out) - 18
	be.PutUint16(out[p:], 0x8001)
	be.PutUint16(out[p+2:], 14)
	return out
}
