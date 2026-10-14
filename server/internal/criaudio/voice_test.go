package criaudio

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func testWAV(channels, frames int) []byte {
	raw := make([]byte, 44+frames*channels*2)
	le := binary.LittleEndian
	copy(raw, "RIFF")
	le.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	le.PutUint32(raw[16:], 16)
	le.PutUint16(raw[20:], 1)
	le.PutUint16(raw[22:], uint16(channels))
	le.PutUint32(raw[24:], 22050)
	le.PutUint32(raw[28:], uint32(22050*channels*2))
	le.PutUint16(raw[32:], uint16(channels*2))
	le.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	le.PutUint32(raw[40:], uint32(frames*channels*2))
	for frame := 0; frame < frames; frame++ {
		for ch := 0; ch < channels; ch++ {
			x := int16(math.Sin(float64(frame)*2*math.Pi*float64(440+ch*220)/22050) * 18000)
			le.PutUint16(raw[44+(frame*channels+ch)*2:], uint16(x))
		}
	}
	return raw
}

func TestWAVBoundsAndInvalidTables(t *testing.T) {
	raw := testWAV(1, 22050)
	if w, e := ParseWAV(raw); e != nil || w.Frames != 22050 || w.Rate != 22050 || w.Channels != 1 {
		t.Fatalf("valid WAV: %+v %v", w, e)
	}
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint32(b[4:], 4) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[20:], 3) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[24:], 192000) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[22:], 3) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[40:], uint32(len(b))) },
	} {
		b := bytes.Clone(raw)
		mutate(b)
		if _, e := ParseWAV(b); e == nil {
			t.Fatal("accepted invalid WAV")
		}
	}
	if _, e := ParseWAV(testWAV(1, 22050*30+1)); e == nil {
		t.Fatal("accepted >30s")
	}
	if _, e := ParseWAV(testWAV(2, 22050*31)); e == nil {
		t.Fatal("accepted oversized audio")
	}
	for n := 0; n < 32; n++ {
		b := make([]byte, 32)
		copy(b, "@UTF")
		binary.BigEndian.PutUint32(b[4:], uint32(n))
		if _, e := readUTF(b); e == nil {
			t.Fatal("accepted bad table")
		}
	}
}

// Decode the emitted ADX as an independent consumer, checking channel order,
// frame count, signed nibbles and bounded quantization loss.
func TestADXRoundTrip(t *testing.T) {
	for _, channels := range []int{1, 2} {
		w, e := ParseWAV(testWAV(channels, 22053))
		if e != nil {
			t.Fatal(e)
		}
		adx := EncodeADX(w)
		be := binary.BigEndian
		headerSize := int(be.Uint16(adx[2:])) + 4
		if be.Uint16(adx) != 0x8000 || int(be.Uint32(adx[12:])) != w.Frames || headerSize != 64 || adx[18] != 4 || string(adx[headerSize-6:headerSize]) != "(c)CRI" || adx[7] != byte(channels) {
			t.Fatal("invalid ADX header")
		}
		a := math.Sqrt2 - math.Cos(2*math.Pi*float64(be.Uint16(adx[16:]))/float64(be.Uint32(adx[8:])))
		b := math.Sqrt2 - 1
		c := (a - math.Sqrt(a*a-b*b)) / b
		coef1, coef2 := int(c*8192), int(-c*c*4096)
		history := make([][2]int, channels)
		energy, errorEnergy := 0.0, 0.0
		for block := 0; block < (w.Frames+31)/32; block++ {
			for ch := 0; ch < channels; ch++ {
				p := headerSize + (block*channels+ch)*18
				scale := int(be.Uint16(adx[p:])) + 1
				h1, h2 := history[ch][0], history[ch][1]
				for i := 0; i < 32; i++ {
					q := int(adx[p+2+i/2])
					if i%2 == 0 {
						q >>= 4
					}
					q &= 15
					if q >= 8 {
						q -= 16
					}
					v := max(-32768, min(32767, q*scale+((coef1*h1+coef2*h2)>>12)))
					h2, h1 = h1, v
					if f := block*32 + i; f < w.Frames {
						x := float64(w.Samples[f*channels+ch])
						d := float64(v) - x
						energy += x * x
						errorEnergy += d * d
					}
				}
				history[ch] = [2]int{h1, h2}
			}
		}
		if snr := 10 * math.Log10(energy/errorEnergy); snr < 24 {
			t.Fatalf("ADX signal loss: %.1f dB", snr)
		}
	}
}

// Optional external validation: vgmstream follows CRI's stored-scale + 1,
// truncated coefficients and v4 history convention, unlike FFmpeg's decoder.
func TestADXExternalDecoder(t *testing.T) {
	decoder := os.Getenv("CN602_VGMSTREAM")
	if decoder == "" {
		t.Skip("requires vgmstream-cli")
	}
	inputs := map[string][]byte{"mono": testWAV(1, 22053), "stereo": testWAV(2, 22053), "quiet": testWAV(1, 22053)}
	for i := 44; i < len(inputs["quiet"]); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(inputs["quiet"][i:])) / 256
		binary.LittleEndian.PutUint16(inputs["quiet"][i:], uint16(sample))
	}
	if path := os.Getenv("CN602_VOICE_SAMPLE"); path != "" {
		var err error
		inputs["uploaded"], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, raw := range inputs {
		t.Run(name, func(t *testing.T) {
			w, err := ParseWAV(raw)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			input, output := filepath.Join(dir, "voice.adx"), filepath.Join(dir, "decoded.wav")
			if err := os.WriteFile(input, EncodeADX(w), 0600); err != nil {
				t.Fatal(err)
			}
			if log, err := exec.Command(decoder, "-o", output, input).CombinedOutput(); err != nil {
				t.Fatalf("external decoder: %v\n%s", err, log)
			}
			decoded, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseWAV(decoded)
			if err != nil || got.Frames != w.Frames || got.Channels != w.Channels || got.Rate != w.Rate {
				t.Fatal("external waveform layout mismatch", err)
			}
			energy, loss, decodedEnergy := 0.0, 0.0, 0.0
			for i, sample := range w.Samples {
				x, y := float64(sample), float64(got.Samples[i])
				energy += x * x
				decodedEnergy += y * y
				loss += (x - y) * (x - y)
			}
			snr, gain := 10*math.Log10(energy/loss), 10*math.Log10(decodedEnergy/energy)
			t.Logf("external decode SNR %.2f dB, gain %.3f dB", snr, gain)
			if snr < 24 || math.Abs(gain) > 0.2 {
				t.Fatal("external decode distorted or amplified audio")
			}
		})
	}
}

func TestVoiceCPKWithOriginalTemplate(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires original voice template")
	}
	raw, e := os.ReadFile(filepath.Join(root, "resources/cpk/CueSheet_Card_0.cpk"))
	if e != nil {
		t.Fatal(e)
	}
	input := map[int][]byte{900000000: testWAV(1, 22050), 900000001: testWAV(2, 22053)}
	if path := os.Getenv("CN602_VOICE_SAMPLE"); path != "" {
		input[900000002], e = os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
	}
	cpk, e := BuildVoiceCPK(raw, input)
	if e != nil {
		t.Fatal(e)
	}
	for id, wav := range input {
		acb, e := ReadCPKEntry(cpk, strconv.Itoa(id)+".acb")
		if e != nil {
			t.Fatal(e)
		}
		table, e := readUTF(acb)
		if e != nil {
			t.Fatal(e)
		}
		cue, e := readUTF(table.rows[0]["CueTable"].value.([]byte))
		if e != nil || cue.rows[0]["CueId"].value != uint64(id) {
			t.Fatal("wrong cue ID")
		}
		wave, e := readUTF(table.rows[0]["WaveformTable"].value.([]byte))
		if e != nil {
			t.Fatal(e)
		}
		w, _ := ParseWAV(wav)
		if wave.rows[0]["EncodeType"].value != uint64(0) || wave.rows[0]["NumSamples"].value != uint64(w.Frames) || wave.rows[0]["NumChannels"].value != uint64(w.Channels) {
			t.Fatal("waveform metadata mismatch")
		}
		awb := table.rows[0]["AwbFile"].value.([]byte)
		start := binary.LittleEndian.Uint32(awb[18:])
		end := binary.LittleEndian.Uint32(awb[22:])
		if end != uint32(len(awb)) || !bytes.Equal(awb[start:end], encodeCN602ADX(w)) {
			t.Fatal("CPK/ACB/AWB audio mismatch")
		}
		if dir := os.Getenv("CN602_CUSTOM_VOICE_VERIFY_DIR"); dir != "" {
			if e := os.MkdirAll(dir, 0700); e != nil {
				t.Fatal(e)
			}
			for ext, b := range map[string][]byte{".acb": acb, ".adx": awb[start:end], ".wav": wav} {
				if e := os.WriteFile(filepath.Join(dir, strconv.Itoa(id)+ext), b, 0600); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	second, e := BuildVoiceCPK(raw, input)
	if e != nil || !bytes.Equal(cpk, second) {
		t.Fatal("nondeterministic voice output")
	}
}
