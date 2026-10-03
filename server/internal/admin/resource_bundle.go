package admin

// Preserve object IDs, type trees and external references when replacing
// supported TextAssets or Texture2D payloads.
import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

type resourceCursor struct {
	b     []byte
	p     int
	order binary.ByteOrder
}

func (c *resourceCursor) take(n int) []byte {
	if n < 0 || n > len(c.b)-c.p {
		panic("资源文件长度不正确")
	}
	b := c.b[c.p : c.p+n]
	c.p += n
	return b
}
func (c *resourceCursor) u16() uint16 { return c.order.Uint16(c.take(2)) }
func (c *resourceCursor) u32() uint32 { return c.order.Uint32(c.take(4)) }
func (c *resourceCursor) u64() uint64 { return c.order.Uint64(c.take(8)) }
func (c *resourceCursor) text() string {
	start := c.p
	for c.take(1)[0] != 0 {
	}
	return string(c.b[start : c.p-1])
}
func (c *resourceCursor) align(n int) { c.take((n - c.p%n) % n) }

type resourceNode struct {
	name  string
	flags uint32
	data  []byte
}
type resourceBundle struct {
	player, engine string
	nodes          []resourceNode
}

func resourceLZ4(data []byte, size int) ([]byte, error) {
	if size < 0 || size > 128<<20 {
		return nil, errors.New("资源解压大小超限")
	}
	out := make([]byte, 0, size)
	p := 0
	length := func(n int) (int, error) {
		if n == 15 {
			for {
				if p >= len(data) {
					return 0, errors.New("LZ4长度不完整")
				}
				v := int(data[p])
				p++
				n += v
				if n > size {
					return 0, errors.New("LZ4长度超限")
				}
				if v != 255 {
					break
				}
			}
		}
		return n, nil
	}
	for p < len(data) {
		token := data[p]
		p++
		n, e := length(int(token >> 4))
		if e != nil {
			return nil, e
		}
		if n > len(data)-p || n > size-len(out) {
			return nil, errors.New("LZ4数据长度不正确")
		}
		out = append(out, data[p:p+n]...)
		p += n
		if p == len(data) {
			break
		}
		if p+2 > len(data) {
			return nil, errors.New("LZ4偏移不完整")
		}
		offset := int(binary.LittleEndian.Uint16(data[p:]))
		p += 2
		if offset == 0 || offset > len(out) {
			return nil, errors.New("LZ4偏移不正确")
		}
		n, e = length(int(token & 15))
		n += 4
		if e != nil || n > size-len(out) {
			return nil, errors.New("LZ4匹配长度不正确")
		}
		for j := 0; j < n; j++ {
			out = append(out, out[len(out)-offset])
		}
	}
	if len(out) != size {
		return nil, errors.New("LZ4解压大小不一致")
	}
	return out, nil
}
func packResourceLZ4(data []byte) []byte {
	table := make([]int, 1<<16)
	for i := range table {
		table[i] = -1
	}
	out := make([]byte, 0, len(data))
	anchor, i := 0, 0
	extra := func(n int) {
		for n >= 255 {
			out = append(out, 255)
			n -= 255
		}
		out = append(out, byte(n))
	}
	for i+12 < len(data) {
		v := binary.LittleEndian.Uint32(data[i:])
		h := int((v * 2654435761) >> 16)
		previous := table[h]
		table[h] = i
		if previous < 0 || i-previous > 65535 || !bytes.Equal(data[previous:previous+4], data[i:i+4]) {
			i++
			continue
		}
		end := i + 4
		for end < len(data)-5 && data[previous+end-i] == data[end] {
			end++
		}
		literal, match := i-anchor, end-i-4
		token := len(out)
		out = append(out, 0)
		out[token] = byte(min(literal, 15)<<4 | min(match, 15))
		if literal >= 15 {
			extra(literal - 15)
		}
		out = append(out, data[anchor:i]...)
		out = append(out, byte(i-previous), byte((i-previous)>>8))
		if match >= 15 {
			extra(match - 15)
		}
		i = end
		anchor = i
	}
	n := len(data) - anchor
	out = append(out, byte(min(n, 15)<<4))
	if n >= 15 {
		extra(n - 15)
	}
	return append(out, data[anchor:]...)
}
func decodeResourceBlock(data []byte, size int, flags uint32) ([]byte, error) {
	switch flags & 63 {
	case 0:
		if len(data) != size {
			return nil, errors.New("资源块大小不一致")
		}
		return data, nil
	case 2, 3:
		return resourceLZ4(data, size)
	default:
		return nil, fmt.Errorf("此资源压缩格式暂不支持：%d", flags&63)
	}
}
func readResourceBundle(data []byte) (result resourceBundle, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("读取客户端资源：%v", r)
		}
	}()
	c := resourceCursor{b: data, order: binary.BigEndian}
	if c.text() != "UnityFS" || c.u32() != 6 {
		return result, errors.New("需要UnityFS 6资源")
	}
	result.player, result.engine = c.text(), c.text()
	if c.u64() != uint64(len(data)) {
		return result, errors.New("Unity资源长度不一致")
	}
	compressed, raw, flags := int(c.u32()), int(c.u32()), c.u32()
	if flags&0x200 != 0 {
		c.align(16)
	}
	var info []byte
	if flags&0x80 != 0 {
		if compressed > len(data)-c.p {
			return result, errors.New("资源目录过大")
		}
		info = data[len(data)-compressed:]
	} else {
		info = c.take(compressed)
	}
	info, err = decodeResourceBlock(info, raw, flags)
	if err != nil {
		return result, err
	}
	metadata := resourceCursor{b: info, order: binary.BigEndian}
	metadata.take(16)
	blocks := int(metadata.u32())
	if blocks < 1 || blocks > 4096 {
		return result, errors.New("资源块数量不正确")
	}
	type block struct {
		raw, compressed int
		flags           uint32
	}
	list := make([]block, blocks)
	for i := range list {
		list[i] = block{int(metadata.u32()), int(metadata.u32()), uint32(metadata.u16())}
	}
	count := int(metadata.u32())
	if count < 1 || count > 32 {
		return result, errors.New("资源文件数量不正确")
	}
	type node struct {
		offset, size int
		flags        uint32
		name         string
	}
	nodes := make([]node, count)
	for i := range nodes {
		nodes[i] = node{int(metadata.u64()), int(metadata.u64()), metadata.u32(), metadata.text()}
	}
	if flags&0x200 != 0 {
		c.align(16)
	}
	var unpacked []byte
	for _, b := range list {
		if b.raw > 128<<20-len(unpacked) {
			return result, errors.New("资源总大小超限")
		}
		chunk, e := decodeResourceBlock(c.take(b.compressed), b.raw, b.flags)
		if e != nil {
			return result, e
		}
		unpacked = append(unpacked, chunk...)
	}
	for _, n := range nodes {
		if n.offset < 0 || n.size < 0 || n.offset > len(unpacked)-n.size {
			return result, errors.New("资源文件偏移不正确")
		}
		result.nodes = append(result.nodes, resourceNode{n.name, n.flags, unpacked[n.offset : n.offset+n.size]})
	}
	return result, nil
}
func (b resourceBundle) save() []byte {
	var content, info, header bytes.Buffer
	info.Write(make([]byte, 16))
	write := func(buf *bytes.Buffer, v any) { _ = binary.Write(buf, binary.BigEndian, v) }
	for _, n := range b.nodes {
		content.Write(n.data)
	}
	compressed := packResourceLZ4(content.Bytes())
	write(&info, uint32(1))
	write(&info, uint32(content.Len()))
	write(&info, uint32(len(compressed)))
	write(&info, uint16(2))
	write(&info, uint32(len(b.nodes)))
	offset := 0
	for _, n := range b.nodes {
		write(&info, uint64(offset))
		write(&info, uint64(len(n.data)))
		write(&info, n.flags)
		info.WriteString(n.name)
		info.WriteByte(0)
		offset += len(n.data)
	}
	header.WriteString("UnityFS\x00")
	write(&header, uint32(6))
	header.WriteString(b.player + "\x00" + b.engine + "\x00")
	sizePos := header.Len()
	write(&header, uint64(0))
	write(&header, uint32(info.Len()))
	write(&header, uint32(info.Len()))
	write(&header, uint32(64))
	binary.BigEndian.PutUint64(header.Bytes()[sizePos:], uint64(header.Len()+info.Len()+len(compressed)))
	header.Write(info.Bytes())
	header.Write(compressed)
	return header.Bytes()
}

type resourceObject struct{ table, start, size, class int }

func replaceResourceTexts(asset []byte, updates map[string]func(string) (string, error)) (result []byte, err error) {
	found := map[string]bool{}
	result, err = rewriteResourceObjects(asset, func(class int, raw []byte) ([]byte, error) {
		if class != 49 {
			return raw, nil
		}
		r := resourceCursor{b: raw, order: binary.LittleEndian}
		n := int(r.u32())
		name := string(r.take(n))
		r.align(4)
		scriptPos := r.p
		length := int(r.u32())
		script := string(r.take(length))
		r.align(4)
		update, ok := updates[name]
		if !ok {
			return raw, nil
		}
		if found[name] {
			return nil, fmt.Errorf("表%s重复", name)
		}
		next, e := update(script)
		if e != nil {
			return nil, e
		}
		var replacement bytes.Buffer
		replacement.Write(raw[:scriptPos])
		_ = binary.Write(&replacement, binary.LittleEndian, uint32(len(next)))
		replacement.WriteString(next)
		for replacement.Len()%4 != 0 {
			replacement.WriteByte(0)
		}
		replacement.Write(raw[r.p:])
		found[name] = true
		return replacement.Bytes(), nil
	})
	if err != nil {
		return nil, err
	}
	for name := range updates {
		if !found[name] {
			return nil, fmt.Errorf("客户端缺少%s", name)
		}
	}
	return result, nil
}

func rewriteResourceObjects(asset []byte, update func(int, []byte) ([]byte, error)) (result []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("读取客户端序列化资源：%v", r)
		}
	}()
	if len(asset) < 20 {
		return nil, errors.New("客户端表文件过短")
	}
	version := int(binary.BigEndian.Uint32(asset[8:]))
	if version < 14 || version > 17 || asset[16] != 0 {
		return nil, errors.New("客户端表序列化版本不支持")
	}
	offset := int(binary.BigEndian.Uint32(asset[12:]))
	if offset < 20 || offset > len(asset) {
		return nil, errors.New("表数据偏移不正确")
	}
	c := resourceCursor{b: asset, p: 20, order: binary.LittleEndian}
	c.text()
	c.u32()
	tree := c.take(1)[0] != 0
	types := int(c.u32())
	if types < 1 || types > 1000 {
		return nil, errors.New("表类型数量不正确")
	}
	classes := make([]int, types)
	for i := range classes {
		class := int(int32(c.u32()))
		classes[i] = class
		if version >= 16 {
			c.take(1)
		}
		if version >= 17 {
			c.take(2)
		}
		if version < 16 && class < 0 || version >= 16 && class == 114 {
			c.take(16)
		}
		c.take(16)
		if tree {
			nodes, strings := int(c.u32()), int(c.u32())
			if nodes > 100000 || strings > 8<<20 {
				return nil, errors.New("表类型树过大")
			}
			c.take(nodes * 24)
			c.take(strings)
		}
	}
	count := int(c.u32())
	if count < 1 || count > 100000 {
		return nil, errors.New("表对象数量不正确")
	}
	objects := make([]resourceObject, count)
	for i := range objects {
		c.align(4)
		c.u64()
		table := c.p
		start, size, typeID := int(c.u32()), int(c.u32()), int(c.u32())
		class := typeID
		if version < 16 {
			class = int(c.u16())
			c.take(2)
		} else {
			if typeID >= len(classes) {
				return nil, errors.New("对象类型不存在")
			}
			class = classes[typeID]
		}
		if version == 15 || version == 16 {
			c.take(1)
		}
		if start < 0 || size < 0 || start > len(asset)-offset-size {
			return nil, errors.New("对象偏移不正确")
		}
		objects[i] = resourceObject{table, start, size, class}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].start < objects[j].start })
	metadata := bytes.Clone(asset[:offset])
	var content bytes.Buffer
	previous := 0
	for _, o := range objects {
		if o.start < previous {
			return nil, errors.New("对象数据重叠")
		}
		content.Write(asset[offset+previous : offset+o.start])
		for content.Len()%8 != o.start%8 {
			content.WriteByte(0)
		}
		start := content.Len()
		raw := asset[offset+o.start : offset+o.start+o.size]
		var e error
		raw, e = update(o.class, raw)
		if e != nil {
			return nil, e
		}
		content.Write(raw)
		binary.LittleEndian.PutUint32(metadata[o.table:], uint32(start))
		binary.LittleEndian.PutUint32(metadata[o.table+4:], uint32(len(raw)))
		previous = o.start + o.size
	}
	content.Write(asset[offset+previous:])
	result = append(metadata, content.Bytes()...)
	binary.BigEndian.PutUint32(result[4:], uint32(len(result)))
	return result, nil
}
