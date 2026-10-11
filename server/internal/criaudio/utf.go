package criaudio

import (
	"bytes"
	"encoding/binary"
	"errors"
)

type utfField struct {
	kind     byte
	position int
	value    any
}
type utfTable struct {
	raw  []byte
	name string
	data int
	rows []map[string]utfField
}

func utfWidth(kind byte) int {
	switch kind {
	case 0, 1:
		return 1
	case 2, 3:
		return 2
	case 4, 5, 8, 10:
		return 4
	case 6, 7, 9, 11:
		return 8
	}
	return 0
}

func readUTF(raw []byte) (utfTable, error) {
	var t utfTable
	bad := errors.New("不支持或损坏的CRI音频表")
	if len(raw) < 32 || string(raw[:4]) != "@UTF" {
		return t, bad
	}
	be := binary.BigEndian
	size := uint64(be.Uint32(raw[4:])) + 8
	if size < 32 || size > uint64(len(raw)) {
		return t, bad
	}
	raw = raw[:int(size)]
	version := be.Uint16(raw[8:])
	rows := int(be.Uint16(raw[10:])) + 8
	strings := int(be.Uint32(raw[12:])) + 8
	data := int(be.Uint32(raw[16:])) + 8
	columns := int(be.Uint16(raw[24:]))
	width := int(be.Uint16(raw[26:]))
	count := int(be.Uint32(raw[28:]))
	if version > 1 || columns == 0 || columns > 256 || count > 128000 || rows < 32 || strings < rows || data < strings || data > len(raw) || uint64(width)*uint64(count) > uint64(strings-rows) {
		return t, bad
	}
	str := func(offset uint32) (string, error) {
		if uint64(offset) >= uint64(data-strings) {
			return "", bad
		}
		p := strings + int(offset)
		n := bytes.IndexByte(raw[p:data], 0)
		if n < 0 {
			return "", bad
		}
		return string(raw[p : p+n]), nil
	}
	value := func(kind byte, p int) (any, error) {
		n := utfWidth(kind)
		if n == 0 || p < 0 || p+n > len(raw) {
			return nil, bad
		}
		switch kind {
		case 10:
			return str(be.Uint32(raw[p:]))
		case 11:
			off, length := uint64(be.Uint32(raw[p:])), uint64(be.Uint32(raw[p+4:]))
			if off+length > uint64(len(raw)-data) {
				return nil, bad
			}
			return raw[data+int(off) : data+int(off+length)], nil
		default:
			var v uint64
			for _, b := range raw[p : p+n] {
				v = v<<8 | uint64(b)
			}
			return v, nil
		}
	}
	t = utfTable{raw: raw, data: data}
	var err error
	t.name, err = str(be.Uint32(raw[20:]))
	if err != nil {
		return utfTable{}, err
	}
	type column struct {
		name          string
		kind, storage byte
		p             int
		value         any
	}
	cols := make([]column, 0, columns)
	p := 32
	rowSize := 0
	seen := map[string]bool{}
	for i := 0; i < columns; i++ {
		if p+5 > rows {
			return utfTable{}, bad
		}
		flag := raw[p]
		name, e := str(be.Uint32(raw[p+1:]))
		if e != nil || seen[name] {
			return utfTable{}, bad
		}
		seen[name] = true
		kind, storage := flag&15, flag&240
		p += 5
		n := utfWidth(kind)
		if n == 0 {
			return utfTable{}, bad
		}
		col := column{name: name, kind: kind, storage: storage, p: -1, value: uint64(0)}
		if kind == 10 {
			col.value = ""
		}
		if kind == 11 {
			col.value = []byte{}
		}
		switch storage {
		case 0x10:
		case 0x30:
			if p+n > rows {
				return utfTable{}, bad
			}
			col.p = p
			col.value, err = value(kind, p)
			p += n
		case 0x50:
			rowSize += n
		default:
			return utfTable{}, bad
		}
		if err != nil {
			return utfTable{}, err
		}
		cols = append(cols, col)
	}
	if rowSize > width {
		return utfTable{}, bad
	}
	for i := 0; i < count; i++ {
		p = rows + i*width
		row := map[string]utfField{}
		for _, col := range cols {
			f := utfField{col.kind, col.p, col.value}
			if col.storage == 0x50 {
				f.position = p
				f.value, err = value(col.kind, p)
				p += utfWidth(col.kind)
				if err != nil {
					return utfTable{}, err
				}
			}
			row[col.name] = f
		}
		t.rows = append(t.rows, row)
	}
	return t, nil
}

func (t utfTable) setNumber(row int, key string, v uint64) error {
	if row < 0 || row >= len(t.rows) {
		return errors.New("CRI音频表缺少数据行")
	}
	f, ok := t.rows[row][key]
	if !ok || f.kind >= 8 {
		return errors.New("CRI音频表缺少数值字段：" + key)
	}
	n := utfWidth(f.kind)
	if n < 8 && v > uint64(1)<<(n*8)-1 {
		return errors.New("CRI音频字段溢出")
	}
	if f.position < 0 {
		if v == 0 {
			return nil
		}
		return errors.New("CRI模板字段无法修改：" + key)
	}
	for i := n - 1; i >= 0; i-- {
		t.raw[f.position+i] = byte(v)
		v >>= 8
	}
	return nil
}

// Rebuild a TOC using explicit per-row values, as supported by the native
// UTF reader. The ACB's richer schema and its authored cue graph are retained.
func voiceTOC(entries []cpkEntry) []byte {
	names := []string{"DirName", "FileName", "FileSize", "ExtractSize", "FileOffset", "ID", "UserString"}
	kinds := []byte{10, 10, 4, 4, 6, 4, 10}
	var strings bytes.Buffer
	add := func(s string) uint32 {
		p := strings.Len()
		strings.WriteString(s)
		strings.WriteByte(0)
		return uint32(p)
	}
	name := add("CpkTocInfo")
	empty := add("")
	null := add("<NULL>")
	schema := make([]byte, len(names)*5)
	for i, key := range names {
		schema[i*5] = 0x50 | kinds[i]
		binary.BigEndian.PutUint32(schema[i*5+1:], add(key))
	}
	rows := make([]byte, len(entries)*32)
	for i, e := range entries {
		p := i * 32
		be := binary.BigEndian
		be.PutUint32(rows[p:], empty)
		be.PutUint32(rows[p+4:], add(e.name))
		be.PutUint32(rows[p+8:], uint32(len(e.raw)))
		be.PutUint32(rows[p+12:], uint32(len(e.raw)))
		be.PutUint64(rows[p+16:], uint64(e.offset))
		be.PutUint32(rows[p+24:], uint32(i))
		be.PutUint32(rows[p+28:], null)
	}
	rowStart := 32 + len(schema)
	strStart := rowStart + len(rows)
	end := align(strStart+strings.Len(), 8)
	out := make([]byte, end)
	copy(out, "@UTF")
	be := binary.BigEndian
	be.PutUint32(out[4:], uint32(end-8))
	be.PutUint16(out[10:], uint16(rowStart-8))
	be.PutUint32(out[12:], uint32(strStart-8))
	be.PutUint32(out[16:], uint32(end-8))
	be.PutUint32(out[20:], name)
	be.PutUint16(out[24:], uint16(len(names)))
	be.PutUint16(out[26:], 32)
	be.PutUint32(out[28:], uint32(len(entries)))
	copy(out[32:], schema)
	copy(out[rowStart:], rows)
	copy(out[strStart:], strings.Bytes())
	return out
}

func align(n, unit int) int { return (n + unit - 1) / unit * unit }
