package admin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func resourceHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func resourceJSON(v any) ([]byte, error) {
	b, e := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), e
}
func rewriteResourceCSV(text string, changes map[string][]string) (string, error) {
	// Preserve every unrelated line verbatim, including historical malformed
	// quoting in official descriptions. Custom records do not contain newlines.
	lines := strings.SplitAfter(text, "\n")
	seen := map[string]bool{}
	encode := func(row []string) (string, error) {
		var b strings.Builder
		w := csv.NewWriter(&b)
		w.UseCRLF = true
		if e := w.Write(row); e != nil {
			return "", e
		}
		w.Flush()
		return b.String(), w.Error()
	}
	for i, line := range lines {
		id, _, _ := strings.Cut(line, ",")
		id = strings.Trim(id, "\"")
		if row, ok := changes[id]; ok {
			if seen[id] {
				return "", fmt.Errorf("客户端记录%s重复", id)
			}
			next, e := encode(row)
			if e != nil {
				return "", e
			}
			lines[i], seen[id] = next, true
		}
	}
	ids := []string{}
	for id := range changes {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	result := strings.Join(lines, "")
	if len(ids) > 0 && result != "" && !strings.HasSuffix(result, "\n") {
		result += "\r\n"
	}
	for _, id := range ids {
		next, e := encode(changes[id])
		if e != nil {
			return "", e
		}
		result += next
	}
	return result, nil
}
func mergeResourceRows(m map[string]any, key, idKey string, updates []any) (int, error) {
	rows, ok := m[key].([]any)
	if !ok {
		return 0, fmt.Errorf("资源表%s不正确", key)
	}
	added := 0
	for _, update := range updates {
		u := update.(map[string]any)
		id := u[idKey]
		found := -1
		for i, row := range rows {
			if row.(map[string]any)[idKey] == id {
				if found >= 0 {
					return 0, errors.New("资源ID重复")
				}
				found = i
			}
		}
		if found < 0 {
			rows = append(rows, u)
			added++
		} else {
			rows[found] = u
		}
	}
	m[key] = rows
	return added, nil
}
func resourceRow(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
func changeResourceHash(v any, old, next string) {
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			if s, ok := value.(string); ok && s == old {
				x[key] = next
			} else {
				changeResourceHash(value, old, next)
			}
		}
	case []any:
		for i, value := range x {
			if s, ok := value.(string); ok && s == old {
				x[i] = next
			} else {
				changeResourceHash(value, old, next)
			}
		}
	}
}

func (a *API) buildCollectionPatch(d collectionDraft) (resultBytes []byte, resultError error) {
	defer func() {
		if failure := recover(); failure != nil {
			resultBytes = nil
			resultError = fmt.Errorf("资源布局不正确，未生成更新包：%v", failure)
		}
	}()
	root, e := a.collectionRoot()
	if e != nil {
		return nil, e
	}
	files := map[string][]byte{}
	original := map[string][]byte{}
	read := func(name string) ([]byte, error) {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if e == nil {
			original[name] = b
		}
		return b, e
	}
	load := func(name string) (map[string]any, error) {
		b, e := read(name)
		if e != nil {
			return nil, e
		}
		var v map[string]any
		e = json.Unmarshal(b, &v)
		return v, e
	}
	put := func(name string, v any) error {
		b, e := resourceJSON(v)
		if e == nil {
			files[name] = b
		}
		return e
	}
	itemPath := "_local/control/server/cn602-item-runtime-master.json"
	honorPath := "_local/control/server/cn602-honor-runtime-master.json"
	adminPath := "_local/control/server/cn602-admin-assets/manifest.json"
	bundlePath := "resources/patch/main_c/container.dat"
	versionPath := "resources/patch/version.dat"
	items, e := load(itemPath)
	if e != nil {
		return nil, e
	}
	honors, e := load(honorPath)
	if e != nil {
		return nil, e
	}
	admin, e := load(adminPath)
	if e != nil {
		return nil, e
	}
	assetMap, e := load("asset-map.json")
	if e != nil {
		return nil, e
	}
	manifest, e := load("resource-set.json")
	if e != nil {
		return nil, e
	}
	itemRows, profiles, honorRows := []any{}, []any{}, []any{}
	clientItems, clientHonors := map[string][]string{}, map[string][]string{}
	for _, h := range d.Honors {
		honorRows = append(honorRows, resourceRow(h))
		slots := []string{"", "", "", ""}
		for i := range slots {
			if h.SlotMask&(1<<i) != 0 {
				slots[i] = "1"
			}
		}
		clientHonors[strconv.Itoa(h.ID)] = []string{strconv.Itoa(h.ID), h.Name, slots[0], slots[1], slots[2], slots[3], "", "0", ""}
	}
	for _, b := range d.Boxes {
		i := b.Item
		itemRows = append(itemRows, resourceRow(i))
		profiles = append(profiles, resourceRow(b.Profile))
		clientItems[strconv.Itoa(i.ItemID)] = []string{strconv.Itoa(i.ItemID), i.Name, strconv.Itoa(i.PictID), i.ItemType, i.Function, strconv.Itoa(i.FunctionValue), strconv.Itoa(i.MaxOwned), "0", i.Description, ""}
		iconName := fmt.Sprintf("_local/control/server/cn602-admin-assets/item/%d.webp", i.ItemID)
		sourceName := fmt.Sprintf("_local/control/server/cn602-admin-assets/item/%d.webp", b.IconSource)
		icon, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(sourceName)))
		if e != nil {
			return nil, e
		}
		if _, e = os.Stat(filepath.Join(root, filepath.FromSlash(iconName))); e == nil {
			if _, e = read(iconName); e != nil {
				return nil, e
			}
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		files[iconName] = icon
	}
	added, e := mergeResourceRows(items, "items", "item_id", itemRows)
	if e != nil {
		return nil, e
	}
	if _, e = mergeResourceRows(items, "item_gacha_profiles", "item_id", profiles); e != nil {
		return nil, e
	}
	if _, e = mergeResourceRows(honors, "honors", "honor_id", honorRows); e != nil {
		return nil, e
	}
	if e = put(itemPath, items); e != nil {
		return nil, e
	}
	if e = put(honorPath, honors); e != nil {
		return nil, e
	}
	coverage := admin["catalog_image_coverage"].(map[string]any)["item"].(map[string]any)
	coverage["entry_count"] = coverage["entry_count"].(float64) + float64(added)
	coverage["resolved_source_count"] = coverage["resolved_source_count"].(float64) + float64(added)
	exported := admin["exported"].(map[string]any)
	exported["item"] = exported["item"].(float64) + float64(added)
	admin["source"].(map[string]any)["item_master_sha256"] = resourceHash(files[itemPath])
	if e = put(adminPath, admin); e != nil {
		return nil, e
	}
	raw, e := read(bundlePath)
	if e != nil {
		return nil, e
	}
	bundle, e := readResourceBundle(raw)
	if e != nil {
		return nil, e
	}
	if len(bundle.nodes) != 1 {
		return nil, errors.New("当前container资源布局不支持编辑")
	}
	updates := map[string]func(string) (string, error){"honor.csv": func(s string) (string, error) { return rewriteResourceCSV(s, clientHonors) }, "item.csv": func(s string) (string, error) { return rewriteResourceCSV(s, clientItems) }}
	bundle.nodes[0].data, e = replaceResourceTexts(bundle.nodes[0].data, updates)
	if e != nil {
		return nil, e
	}
	files[bundlePath] = bundle.save()
	crc := fmt.Sprintf("%08X", crc32.ChecksumIEEE(files[bundlePath]))
	encoded, e := read(versionPath)
	if e != nil {
		return nil, e
	}
	key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
	decoded := bytes.Clone(encoded)
	for i := range decoded {
		decoded[i] -= key[i%8]
	}
	vr := csv.NewReader(bytes.NewReader(decoded))
	vr.FieldsPerRecord = -1
	rows, e := vr.ReadAll()
	if e != nil {
		return nil, e
	}
	count := 0
	for _, row := range rows {
		if len(row) == 4 && row[0] == "<bundle_ver>" && row[1] == "main_c/container.dat" {
			row[3] = crc
			count++
		}
	}
	if count != 1 {
		return nil, errors.New("客户端版本表缺少container记录")
	}
	var vb bytes.Buffer
	vw := csv.NewWriter(&vb)
	vw.WriteAll(rows)
	if e = vw.Error(); e != nil {
		return nil, e
	}
	version := vb.Bytes()
	for i := range version {
		version[i] += key[i%8]
	}
	files[versionPath] = version
	count = 0
	for _, row := range assetMap["bundles"].([]any) {
		b := row.(map[string]any)
		if b["bundle"] == "main_c/container.dat" {
			if b["scrambled"] != false {
				return nil, errors.New("当前container资源编码不支持编辑")
			}
			b["delivery_crc32"] = crc
			count++
		}
	}
	if count != 1 {
		return nil, errors.New("资源目录缺少container记录")
	}
	changeResourceHash(assetMap, resourceHash(encoded), resourceHash(version))
	if e = put("asset-map.json", assetMap); e != nil {
		return nil, e
	}
	inventory := manifest["files"].([]any)
	delta, newFiles := 0, 0
	before := map[string]string{}
	for name, b := range files {
		matches := 0
		for _, row := range inventory {
			entry := row.(map[string]any)
			if entry["path"] != name {
				continue
			}
			matches++
			old, ok := original[name]
			if !ok || entry["sha256"] != resourceHash(old) || int(entry["bytes"].(float64)) != len(old) {
				return nil, fmt.Errorf("资源%s与当前清单不一致，请先同步资源清单", name)
			}
			before[name] = resourceHash(old)
			delta += len(b) - len(old)
			entry["bytes"] = len(b)
			entry["sha256"] = resourceHash(b)
		}
		if matches > 1 {
			return nil, errors.New("资源清单路径重复")
		}
		if matches == 0 {
			if _, exists := original[name]; exists {
				return nil, errors.New("已有资源未登记到清单")
			}
			inventory = append(inventory, map[string]any{"path": name, "bytes": len(b), "sha256": resourceHash(b)})
			delta += len(b)
			newFiles++
		}
	}
	manifest["files"] = inventory
	summary := manifest["summary"].(map[string]any)
	summary["bytes"] = int(summary["bytes"].(float64)) + delta
	summary["file_count"] = int(summary["file_count"].(float64)) + newFiles
	for _, field := range []string{"bytes_to_copy", "files_to_copy"} {
		if value, ok := summary[field].(float64); ok {
			increment := delta
			if field == "files_to_copy" {
				increment = newFiles
			}
			summary[field] = int(value) + increment
		}
	}
	if e = put("resource-set.json", manifest); e != nil {
		return nil, e
	}
	before["resource-set.json"] = resourceHash(original["resource-set.json"])
	var result bytes.Buffer
	z := zip.NewWriter(&result)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, e := z.Create("resource-set/" + name)
		if e != nil {
			return nil, e
		}
		if _, e = entry.Write(files[name]); e != nil {
			return nil, e
		}
	}
	report, e := resourceJSON(map[string]any{"base_sha256": before, "honors": d.Honors, "boxes": d.Boxes})
	if e != nil {
		return nil, e
	}
	entry, _ := z.Create("configuration.json")
	_, _ = entry.Write(report)
	entry, _ = z.Create("README.txt")
	_, _ = io.WriteString(entry, "称号与礼盒资源更新包\n先正常停服并备份资源，再将resource-set按原路径合并覆盖到运行目录，重启服务。\n客户端重新下载资源后生效，无需修改APK。保存草稿/下载更新包不会自动修改运行资源。\nconfiguration.json记录制作时的原文件SHA-256；目标资源已有其他修改时请先比较合并。\n保留数据库、玩家存档、部署文件。称号和礼盒不会自动发给玩家，请在后台另行配置奖励或发放。\n")
	if e = z.Close(); e != nil {
		return nil, e
	}
	return result.Bytes(), nil
}
