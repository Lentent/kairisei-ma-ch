package admin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/cpk"
)

func customTestVoiceWAV() []byte {
	raw := make([]byte, 244)
	le := binary.LittleEndian
	copy(raw, "RIFF")
	le.PutUint32(raw[4:], 236)
	copy(raw[8:], "WAVEfmt ")
	le.PutUint32(raw[16:], 16)
	le.PutUint16(raw[20:], 1)
	le.PutUint16(raw[22:], 1)
	le.PutUint32(raw[24:], 22050)
	le.PutUint32(raw[28:], 44100)
	le.PutUint16(raw[32:], 2)
	le.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	le.PutUint32(raw[40:], 200)
	return raw
}

func TestCustomCardVoiceSourcesAndUpload(t *testing.T) {
	s := customUnitSources()
	row := append([]string(nil), s.Cards[101]...)
	row = append(row, make([]string, 107-len(row))...)
	row[89], row[90] = "600010080", "600010090"
	s.Cards[101] = row
	other := append([]string(nil), row...)
	other[0], other[89], other[90] = "102", "600020080", "600020090"
	s.Cards[102] = other
	definition := s.Master.CardTemplates[0]
	definition.CardID = 102
	s.Master.CardTemplates = append(s.Master.CardTemplates, definition)
	c, e := s.template(101)
	if e != nil {
		t.Fatal(e)
	}
	c.ID = customCardFirstID
	c.AttackVoice = &customCardVoiceSource{CardID: 102, Kind: "support"}
	c.SupportVoiceAudio = customTestVoiceWAV()
	before := append([]string(nil), row...)
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	var saved customCard
	if e = json.Unmarshal(raw, &saved); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(saved.SupportVoiceAudio, c.SupportVoiceAudio) {
		t.Fatal("audio lost on persistence")
	}
	voices, e := customCardVoices(saved, s, func(id int) bool { return id == 102 })
	if e != nil {
		t.Fatal(e)
	}
	if voices[89] != "600020090" || voices[90] != strconv.Itoa(customVoiceID(c, 1)) {
		t.Fatal("wrong selected voice IDs")
	}
	if !reflect.DeepEqual(row, before) {
		t.Fatal("mutated template")
	}
	if _, e = customCardVoices(saved, s, func(int) bool { return false }); e == nil {
		t.Fatal("accepted unavailable source")
	}
	c.SupportVoice = &customCardVoiceSource{CardID: 101, Kind: "support"}
	if _, e = customCardVoices(c, s, nil); e == nil {
		t.Fatal("accepted two audio choices for one slot")
	}
	c.SupportVoice = nil
	c.SupportVoiceAudio = []byte("not audio")
	if _, e = customCardVoices(c, s, nil); e == nil {
		t.Fatal("accepted invalid upload")
	}
	c.SupportVoiceAudio = nil
	c.AttackVoice.Kind = "home"
	if _, e = customCardVoices(c, s, nil); e == nil {
		t.Fatal("accepted wrong voice kind")
	}
	c.AttackVoice = nil
	if voices, e = customCardVoices(c, s, nil); e != nil || len(voices) != 0 {
		t.Fatal("legacy draft does not inherit")
	}
}

func TestCustomCardFameTrainingOnly(t *testing.T) {
	s := customUnitSources()
	row := append([]string(nil), s.Cards[101]...)
	row = append(row, make([]string, 107-len(row))...)
	row[25], row[95], row[96], row[97] = "100", "2", "30", "20"
	s.Cards[101] = row
	c, e := s.template(101)
	if e != nil {
		t.Fatal(e)
	}
	c.ID = customCardFirstID
	original := append([]string(nil), row...)
	for _, allow := range []bool{false, true} {
		c.AllowFameTraining = &allow
		exported, _, _, card, e := materializeCustomCard(c, s)
		if e != nil {
			t.Fatal(e)
		}
		kind := 0
		if allow {
			kind = 2
		}
		if customRowInt(exported, 95) != kind || card.DevelopmentType != kind || card.DevelopRadix != 20 || exported[25] != "100" || card.FameMax != s.Master.CardTemplates[0].FameMax || exported[2] != strconv.Itoa(c.ID) {
			t.Fatalf("training changed fame or fusion: %+v", card)
		}
	}
	if !reflect.DeepEqual(row, original) {
		t.Fatal("mutated training template")
	}
	row[95], row[97] = "0", "0"
	allow := true
	c.AllowFameTraining = &allow
	exported, _, _, card, e := materializeCustomCard(c, s)
	if e != nil || exported[97] != "20" || card.DevelopRadix != 20 {
		t.Fatalf("non-trainable template default: %v", e)
	}
	row[95] = "1"
	allow = false
	copyRow := append([]string(nil), row...)
	if e = applyCustomCardFame(c, copyRow); e != nil || copyRow[95] != "1" {
		t.Fatal("disabling training changed decomposition")
	}
	c.AllowFameTraining = nil
	if e = applyCustomCardFame(c, copyRow); e != nil || copyRow[95] != "1" {
		t.Fatal("old draft changed policy")
	}
}

func TestCustomVoicePublicationVersionsAndOwnership(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires original voice template")
	}
	template, e := os.ReadFile(filepath.Join(root, "resources/cpk/CueSheet_Card_0.cpk"))
	if e != nil {
		t.Fatal(e)
	}
	staging := t.TempDir()
	directory := filepath.Join(staging, "resources/cpk")
	if e = os.MkdirAll(directory, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(directory, "CueSheet_Card_0.cpk"), template, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(directory, "versions.json"), []byte(`{"schema_version":1,"versions":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	manifest := map[string]any{"entrypoints": map[string]any{"cn-cpk-root": "resources/cpk"}}
	source := map[string]any{}
	d := customCardDraft{Cards: []customCard{{ID: customCardFirstID, AttackVoiceAudio: customTestVoiceWAV()}}}
	apply := func(files map[string][]byte) {
		t.Helper()
		for name, b := range files {
			if e := os.WriteFile(filepath.Join(staging, filepath.FromSlash(name)), b, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	files, original := map[string][]byte{}, map[string][]byte{}
	if e = buildCustomVoiceResources(d, staging, manifest, source, files, original); e != nil {
		t.Fatal(e)
	}
	apply(files)
	unchanged := map[string][]byte{}
	if e = buildCustomVoiceResources(d, staging, manifest, source, unchanged, map[string][]byte{}); e != nil || len(unchanged) != 0 {
		t.Fatalf("unchanged voice changes version: %v", e)
	}
	if e = buildCustomVoiceResources(d, staging, manifest, map[string]any{}, map[string][]byte{}, map[string][]byte{}); e == nil {
		t.Fatal("overwrote unrelated voice resource")
	}
	d.Cards[0].AttackVoiceAudio[44] = 128
	files, original = map[string][]byte{}, map[string][]byte{}
	if e = buildCustomVoiceResources(d, staging, manifest, source, files, original); e != nil {
		t.Fatal(e)
	}
	if len(original["resources/cpk/"+customVoiceCPK]) == 0 {
		t.Fatal("update lost original hash input")
	}
	apply(files)
	delivery, e := cpk.Load(directory, nil)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, file := range delivery {
		if file.Name == customVoiceCPK {
			found = true
			if file.Version != 2 {
				t.Fatal("updated voice not versioned")
			}
		}
	}
	if !found {
		t.Fatal("custom voice missing download delivery")
	}
	if csv := string(cpk.FileList(delivery)); !strings.Contains(csv, customVoiceCPK+",CARD_VOICE,980,") || !strings.Contains(csv, ",ALL,") {
		t.Fatalf("new CPK is not preloaded as card voice: %s", csv)
	}
}
