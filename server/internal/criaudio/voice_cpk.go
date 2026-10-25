package criaudio

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

type cpkEntry struct {
	name   string
	raw    []byte
	offset int
}

func cpkTables(raw []byte) (utfTable, utfTable, error) {
	bad := errors.New("语音模板不是支持的原始CPK")
	if len(raw) < 32 || string(raw[:4]) != "CPK " {
		return utfTable{}, utfTable{}, bad
	}
	head, e := readUTF(raw[16:])
	if e != nil || len(head.rows) != 1 {
		return utfTable{}, utfTable{}, bad
	}
	off, ok := head.rows[0]["TocOffset"].value.(uint64)
	if !ok || off+32 > uint64(len(raw)) || string(raw[off:off+4]) != "TOC " {
		return utfTable{}, utfTable{}, bad
	}
	toc, e := readUTF(raw[off+16:])
	return head, toc, e
}

func ReadCPKEntry(raw []byte, name string) ([]byte, error) {
	head, toc, e := cpkTables(raw)
	if e != nil {
		return nil, e
	}
	content, cok := head.rows[0]["ContentOffset"].value.(uint64)
	toff, tok := head.rows[0]["TocOffset"].value.(uint64)
	if !cok || !tok {
		return nil, errors.New("语音CPK缺少目录偏移")
	}
	base := min(content, toff)
	for _, row := range toc.rows {
		if row["FileName"].value != name {
			continue
		}
		off, ok := row["FileOffset"].value.(uint64)
		size, sok := row["FileSize"].value.(uint64)
		unpacked, uok := row["ExtractSize"].value.(uint64)
		if !ok || !sok || !uok || size != unpacked || base > uint64(len(raw)) || off > uint64(len(raw))-base || size > uint64(len(raw))-base-off {
			return nil, errors.New("语音CPK文件压缩或目录无效")
		}
		return raw[base+off : base+off+size], nil
	}
	return nil, fmt.Errorf("语音模板CPK缺少%s", name)
}

func voiceAWB(adx []byte) []byte {
	out := make([]byte, 32+len(adx))
	copy(out, "AFS2")
	out[4], out[5], out[6] = 1, 4, 2
	le := binary.LittleEndian
	le.PutUint32(out[8:], 1)
	le.PutUint32(out[12:], 32)
	// Waveform ID zero, followed by the start and end file offsets.
	le.PutUint32(out[18:], 32)
	le.PutUint32(out[22:], uint32(len(out)))
	copy(out[32:], adx)
	return out
}

func voiceACB(template []byte, id int, w WAV) ([]byte, error) {
	root, e := readUTF(bytes.Clone(template))
	if e != nil || root.name != "Header" || len(root.rows) != 1 {
		return nil, errors.New("语音ACB模板布局不支持")
	}
	row := root.rows[0]
	adx := encodeCN602ADX(w)
	for key, values := range map[string]map[string]uint64{
		"CueTable":      {"CueId": uint64(id), "Length": uint64(w.Frames * 1000 / w.Rate)},
		"WaveformTable": {"EncodeType": 0, "Streaming": 0, "NumChannels": uint64(w.Channels), "LoopFlag": 0, "SamplingRate": uint64(w.Rate), "NumSamples": uint64(w.Frames)},
	} {
		raw, ok := row[key].value.([]byte)
		if !ok {
			return nil, errors.New("语音ACB缺少" + key)
		}
		table, e := readUTF(raw)
		if e != nil || len(table.rows) != 1 {
			return nil, errors.New("语音ACB须为单个内存语音")
		}
		for key, v := range values {
			if e = table.setNumber(0, key, v); e != nil {
				return nil, e
			}
		}
	}
	name := strconv.Itoa(id)
	f := row["Name"]
	oldName, ok := f.value.(string)
	if !ok || len(name) != len(oldName) || f.position < 0 {
		return nil, errors.New("自制语音编号不符合模板名称长度")
	}
	strStart := int(binary.BigEndian.Uint32(root.raw[12:])) + 8
	p := strStart + int(binary.BigEndian.Uint32(root.raw[f.position:]))
	copy(root.raw[p:p+len(name)], name)
	if guid, ok := row["AcbGuid"].value.([]byte); ok && len(guid) == 16 {
		hash := sha256.Sum256(append([]byte(name), adx...))
		copy(guid, hash[:16])
	}
	f = row["AwbFile"]
	awbOld, ok := f.value.([]byte)
	if !ok || f.position < 0 || len(awbOld) < 16 || string(awbOld[:4]) != "AFS2" {
		return nil, errors.New("语音ACB缺少内存波形库")
	}
	start := root.data + int(binary.BigEndian.Uint32(root.raw[f.position:]))
	end := len(root.raw)
	for _, other := range row {
		if other.kind != 11 || other.position < 0 {
			continue
		}
		off := root.data + int(binary.BigEndian.Uint32(root.raw[other.position:]))
		if off > start && off < end {
			end = off
		}
	}
	if start%32 != 0 || (end-start)%32 != 0 || end < start+len(awbOld) {
		return nil, errors.New("语音ACB波形对齐不支持")
	}
	awb := voiceAWB(adx)
	slot := align(len(awb), 32)
	delta := slot - (end - start)
	for key, other := range row {
		if other.kind != 11 || other.position < 0 {
			continue
		}
		off := int(binary.BigEndian.Uint32(root.raw[other.position:]))
		if root.data+off >= end {
			binary.BigEndian.PutUint32(root.raw[other.position:], uint32(off+delta))
		}
		if key == "AwbFile" {
			binary.BigEndian.PutUint32(root.raw[other.position+4:], uint32(len(awb)))
		}
	}
	out := make([]byte, len(root.raw)+delta)
	copy(out, root.raw[:start])
	copy(out[start:], awb)
	copy(out[start+slot:], root.raw[end:])
	binary.BigEndian.PutUint32(out[4:], uint32(len(out)-8))
	if _, e = readUTF(out); e != nil {
		return nil, e
	}
	return out, nil
}

// BuildVoiceCPK retains the original client's ACB format, ACF category and
// single-cue graph. Only the cue identity, waveform metadata and audio change.
// Each generated ACB is addressable as <voice-id>.acb through the native binder.
func BuildVoiceCPK(templateCPK []byte, voices map[int][]byte) ([]byte, error) {
	if len(voices) == 0 || len(voices) > 400 {
		return nil, errors.New("自制语音数量须在1–400段内")
	}
	base, e := ReadCPKEntry(templateCPK, "600010010.acb")
	if e != nil {
		return nil, e
	}
	head, _, e := cpkTables(templateCPK)
	if e != nil {
		return nil, e
	}
	head, e = readUTF(bytes.Clone(head.raw))
	if e != nil {
		return nil, e
	}
	ids := make([]int, 0, len(voices))
	for id := range voices {
		// Card voices occupy 900000000–902000000; custom stamps use the
		// following 9,999 IDs in a separate CPK.
		if id < 900000000 || id > 902009999 {
			return nil, errors.New("自制语音编号不在保留范围内")
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	entries := make([]cpkEntry, 0, len(ids))
	payload := 0
	for _, id := range ids {
		w, e := ParseWAV(voices[id])
		if e != nil {
			return nil, e
		}
		raw, e := voiceACB(base, id, w)
		if e != nil {
			return nil, e
		}
		entries = append(entries, cpkEntry{strconv.Itoa(id) + ".acb", raw, 0})
		payload += len(raw)
	}
	toc := voiceTOC(entries)
	content := align(2048+16+len(toc), 2048)
	position := content
	for i := range entries {
		entries[i].offset = position - 2048
		position += align(len(entries[i].raw), 2048)
	}
	toc = voiceTOC(entries)
	// Filename lookup needs a TOC; this new archive has no original groups.
	for key, v := range map[string]uint64{"CpkMode": 1, "ContentOffset": uint64(content), "ContentSize": uint64(position - content), "TocOffset": 2048, "TocSize": uint64(16 + len(toc)), "EtocOffset": 0, "EtocSize": 0, "GtocOffset": 0, "GtocSize": 0, "Files": uint64(len(entries)), "Groups": 0, "Attrs": 0, "EnabledPackedSize": uint64(payload), "EnabledDataSize": uint64(payload)} {
		if e = head.setNumber(0, key, v); e != nil {
			return nil, e
		}
	}
	if len(head.raw)+16 > 2048 {
		return nil, errors.New("自制CPK头超过对齐空间")
	}
	out := make([]byte, position)
	copy(out, templateCPK[:16])
	copy(out[16:], head.raw)
	copy(out[2048:], "TOC ")
	binary.LittleEndian.PutUint32(out[2052:], 255)
	binary.LittleEndian.PutUint64(out[2056:], uint64(len(toc)))
	copy(out[2064:], toc)
	for _, entry := range entries {
		copy(out[2048+entry.offset:], entry.raw)
	}
	for _, entry := range entries {
		check, e := ReadCPKEntry(out, entry.name)
		if e != nil || !bytes.Equal(check, entry.raw) {
			return nil, errors.New("自制语音CPK目录校验失败")
		}
	}
	return out, nil
}
