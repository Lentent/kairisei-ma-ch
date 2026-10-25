package admin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kairisei.local/server/internal/criaudio"
)

const customVoiceCPK = "CueSheet_Card_980.cpk"

func customVoiceID(c customCard, slot int) int {
	return 900000000 + (c.ID-customCardFirstID)*2 + slot
}

func customVoiceVolume(c customCard, slot int) (int, error) {
	v := []*int{c.AttackVoiceVolume, c.SupportVoiceVolume}[slot]
	if v == nil {
		return 100, nil
	}
	if *v < 0 || *v > 100 {
		return 0, fmt.Errorf("自制语音音量须为0–100的整数百分比")
	}
	return *v, nil
}

// Keep the saved WAV intact; only attenuate the exported copy. This also lets
// old designs adjust volume without re-uploading or repeatedly scaling PCM.
func customVoiceAudioForExport(c customCard, slot int) ([]byte, error) {
	raw := [][]byte{c.AttackVoiceAudio, c.SupportVoiceAudio}[slot]
	volume, err := customVoiceVolume(c, slot)
	if err != nil {
		return nil, err
	}
	w, err := criaudio.ParseWAV(raw)
	if err != nil || volume == 100 {
		return raw, err
	}
	out := bytes.Clone(raw)
	for p := 12; p+8 <= len(out); {
		n := int(binary.LittleEndian.Uint32(out[p+4:]))
		if string(out[p:p+4]) == "data" {
			for i, sample := range w.Samples {
				binary.LittleEndian.PutUint16(out[p+8+i*2:], uint16(int(sample)*volume/100))
			}
			return out, nil
		}
		p += 8 + n + n%2
	}
	return nil, fmt.Errorf("自制语音缺少PCM数据")
}

// Only normalized PCM WAV reaches the server. The editor decodes MP3/WAV
// locally; no system encoder, shell command or uploaded executable is used.
func buildCustomVoiceResources(d customCardDraft, root string, manifest map[string]any, source map[string]any, files, original map[string][]byte) error {
	voices := map[int][]byte{}
	for _, c := range d.Cards {
		for slot, raw := range [][]byte{c.AttackVoiceAudio, c.SupportVoiceAudio} {
			if len(raw) > 0 {
				adjusted, err := customVoiceAudioForExport(c, slot)
				if err != nil {
					return err
				}
				voices[customVoiceID(c, slot)] = adjusted
			}
		}
	}
	return buildVoiceResources(voices, customVoiceCPK, "admin_custom_voice_cpk", root, manifest, source, files, original)
}

func buildVoiceResources(voices map[int][]byte, cpkName, sourceKey, root string, manifest, source map[string]any, files, original map[string][]byte) error {
	if len(voices) == 0 {
		return nil
	}
	entrypoints, _ := manifest["entrypoints"].(map[string]any)
	cpkRoot, _ := entrypoints["cn-cpk-root"].(string)
	cpkRoot = strings.ReplaceAll(cpkRoot, "\\", "/")
	if cpkRoot == "" || strings.HasPrefix(cpkRoot, "/") || strings.Contains(cpkRoot, ":") || strings.Contains(cpkRoot, "..") {
		return fmt.Errorf("资源清单缺少有效语音目录")
	}
	cpkPath := cpkRoot + "/" + cpkName
	old, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cpkPath)))
	if err == nil && source[sourceKey] != cpkName {
		return fmt.Errorf("自制语音资源编号已被其他资源使用")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	template, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cpkRoot), "CueSheet_Card_0.cpk"))
	if err != nil {
		return fmt.Errorf("缺少原始卡牌语音模板：%w", err)
	}
	generated, err := criaudio.BuildVoiceCPK(template, voices)
	if err != nil {
		return fmt.Errorf("生成自制语音失败：%w", err)
	}
	source[sourceKey] = cpkName
	if bytes.Equal(old, generated) {
		return nil
	}
	if old != nil {
		original[cpkPath] = old
	}
	files[cpkPath] = generated
	versionPath := cpkRoot + "/versions.json"
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(versionPath)))
	newVersionFile := os.IsNotExist(err)
	if newVersionFile {
		// Legacy sets used the loader's default version 1 for all CPKs.
		// Register only the new file; omitted official entries retain version 1.
		raw = []byte(`{"schema_version":1,"versions":[]}`)
		err = nil
	}
	if err != nil {
		return err
	}
	var versions struct {
		Schema   int              `json:"schema_version"`
		Versions []map[string]any `json:"versions"`
	}
	if err = json.Unmarshal(raw, &versions); err != nil || versions.Schema != 1 {
		return fmt.Errorf("语音资源版本表无效")
	}
	found := false
	for _, item := range versions.Versions {
		if item["name"] != cpkName {
			continue
		}
		if found {
			return fmt.Errorf("自制语音版本记录重复")
		}
		version, ok := item["version"].(float64)
		if !ok || version < 1 || version >= 2147483647 || version != float64(int64(version)) {
			return fmt.Errorf("自制语音版本已超出有效范围")
		}
		item["version"] = int64(version) + 1
		found = true
	}
	if !found {
		version := 1
		if old != nil {
			version = 2
		}
		versions.Versions = append(versions.Versions, map[string]any{"name": cpkName, "version": version})
	}
	files[versionPath], err = resourceJSON(versions)
	if err == nil && !newVersionFile {
		original[versionPath] = raw
	}
	return err
}
