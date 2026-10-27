package admin

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
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

	"kairisei.local/server/internal/masterdata"
)

const customStampVoiceCPK = "CueSheet_Card_981.cpk"

func stampVoiceID(id int) int { return 902000001 + id - customStampFirstID }

func (a *API) buildCustomStampPatch(d customStampDraft) (result []byte, err error) {
	defer func() {
		if v := recover(); v != nil {
			result = nil
			err = fmt.Errorf("资源布局不支持，未生成更新包：%v", v)
		}
	}()
	root, err := a.collectionRoot()
	if err != nil {
		return nil, err
	}
	files, original := map[string][]byte{}, map[string][]byte{}
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
		var m map[string]any
		e = json.Unmarshal(b, &m)
		return m, e
	}
	master, err := load(customStampMasterPath)
	if err != nil {
		return nil, err
	}
	assets, err := load("asset-map.json")
	if err != nil {
		return nil, err
	}
	manifest, err := load("resource-set.json")
	if err != nil {
		return nil, err
	}
	adminPath := "_local/control/server/cn602-admin-assets/manifest.json"
	admin, err := load(adminPath)
	if err != nil {
		return nil, err
	}
	source := master["source"].(map[string]any)
	rows := map[string][][]string{}
	updates := []any{}
	ids := []int{}
	voices := map[int][]byte{}
	bundles := assets["bundles"].([]any)
	catalog := assets["catalog_assets"].([]any)
	var template []byte
	scrambled := false
	for _, s := range d.Stamps {
		id := strconv.Itoa(s.ID)
		pict, voice := "", ""
		if s.Kind != "text" {
			if template == nil {
				var bundleName string
				for _, v := range catalog {
					entry := v.(map[string]any)
					if entry["container_path"] == stampImagePath(30001) {
						bundleName, _ = entry["bundle"].(string)
					}
				}
				if bundleName == "" {
					return nil, errors.New("缺少表情图片模板")
				}
				found := 0
				for _, v := range bundles {
					b := v.(map[string]any)
					if b["bundle"] == bundleName {
						scrambled = b["scrambled"] == true
						found++
					}
				}
				if found != 1 {
					return nil, errors.New("表情图片模板目录无效")
				}
				template, err = read("resources/patch/" + bundleName)
				if err != nil {
					return nil, err
				}
				delete(original, "resources/patch/"+bundleName) // read-only template
			}
			art, e := decodeCustomArtwork(s.Artwork)
			if e != nil {
				return nil, e
			}
			raw, cab, e := buildStampArtworkBundle(template, scrambled, s.ID, art)
			if e != nil {
				return nil, e
			}
			bundleName := fmt.Sprintf("main_c/image/custom_stamp_%d.dat", s.ID)
			path := "resources/patch/" + bundleName
			if old, e := read(path); e != nil && !os.IsNotExist(e) {
				return nil, e
			} else if e == nil && len(old) > 0 {
				published, _, e := a.stampPublished()
				if e != nil || !published[s.ID] {
					return nil, errors.New("表情图片资源路径已被占用")
				}
			}
			files[path] = raw
			pict = id
			filtered := bundles[:0]
			for _, v := range bundles {
				if v.(map[string]any)["bundle"] != bundleName {
					filtered = append(filtered, v)
				}
			}
			bundles = filtered
			bundles = append(bundles, map[string]any{"bundle": bundleName, "cab_name": cab, "scrambled": false, "delivery_crc32": fmt.Sprintf("%08X", crc32.ChecksumIEEE(raw)), "dependencies": []any{}})
			entries := catalog[:0]
			for _, v := range catalog {
				if v.(map[string]any)["bundle"] != bundleName {
					entries = append(entries, v)
				}
			}
			catalog = entries
			catalog = append(catalog, map[string]any{"container_path": stampImagePath(s.ID), "directory": stampImageDirectory(s.ID), "name": stampImageName(s.ID), "extension": ".png", "object_type": "Texture2D", "bundle": bundleName})
			thumb := fmt.Sprintf("_local/control/server/cn602-admin-assets/stamp/%d.png", s.ID)
			if _, e = read(thumb); e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			files[thumb], err = encodeCustomArtworkPNG(art, 256, 256)
			if err != nil {
				return nil, err
			}
		}
		if s.Kind == "sound" {
			voice = strconv.Itoa(stampVoiceID(s.ID))
			voices[stampVoiceID(s.ID)], err = customVoiceAudioForExport(customCard{AttackVoiceAudio: s.Audio, AttackVoiceVolume: &s.Volume}, 0)
			if err != nil {
				return nil, err
			}
		}
		rows[id] = [][]string{{id, strconv.Itoa(s.Category), s.Name, pict, s.Message, "", voice, ""}}
		updates = append(updates, resourceRow(map[string]any{"stamp_id": s.ID, "category": s.Category, "label": s.Name, "display_text": s.Message, "default_owned": false, "kind": s.Kind}))
		ids = append(ids, s.ID)
	}
	if _, err = mergeResourceRows(master, "stamps", "stamp_id", updates); err != nil {
		return nil, err
	}
	source["admin_custom_stamps"] = ids
	if err = buildVoiceResources(voices, customStampVoiceCPK, "admin_custom_stamp_voice_cpk", root, manifest, source, files, original); err != nil {
		return nil, err
	}
	const bundlePath = "resources/patch/main_c/container.dat"
	raw, err := read(bundlePath)
	if err != nil {
		return nil, err
	}
	b, err := readResourceBundle(raw)
	if err != nil {
		return nil, err
	}
	if len(b.nodes) != 1 {
		return nil, errors.New("客户端container布局不支持")
	}
	var serverCSV string
	b.nodes[0].data, err = replaceResourceTexts(b.nodes[0].data, map[string]func(string) (string, error){"stamp.csv": func(s string) (string, error) { serverCSV, err = rewriteCustomCSV(s, rows); return serverCSV, err }})
	if err != nil {
		return nil, err
	}
	files[bundlePath] = b.save()
	const csvPath = "_local/control/server/cn602-stamp-master/stamp.csv"
	if _, err = read(csvPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	files[csvPath] = []byte(serverCSV)
	source["stamp"] = map[string]any{"path": csvPath, "bytes": len(serverCSV), "sha256": resourceHash([]byte(serverCSV))}
	files[customStampMasterPath], err = resourceJSON(master)
	if err != nil {
		return nil, err
	}
	// Validate the exact generated master through the runtime loader before packaging.
	tmp, err := os.CreateTemp("", "custom-stamps-*.json")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = tmp.Write(files[customStampMasterPath])
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if _, err = masterdata.LoadStampRuntimeMaster(name); err != nil {
		return nil, err
	}
	assets["bundles"], assets["catalog_assets"] = bundles, catalog
	files[adminPath], err = resourceJSON(admin)
	if err != nil {
		return nil, err
	}
	return finishCustomStampPatch(root, d, files, original, assets, manifest, read)
}

func finishCustomStampPatch(root string, d customStampDraft, files, original map[string][]byte, assets, manifest map[string]any, read func(string) ([]byte, error)) ([]byte, error) {
	reportStamps := append([]customStamp(nil), d.Stamps...)
	for i := range reportStamps {
		reportStamps[i].Artwork = nil
		reportStamps[i].Audio = nil
	}
	return finishCustomResourcePatch(files, original, assets, manifest, read, map[string]any{"stamps": reportStamps}, "自制表情资源更新包\n停服并备份资源与数据库。核对configuration.json内base_sha256与当前文件一致（空值表示新文件，目标不应已存在），再将resource-set内容按原路径合并覆盖并重启。使用CDN时同步更新后的资源、清单及语音CPK。\n客户端重新登录并下载资源后生效，无需修改APK。保存草稿和下载ZIP不会自动发布或发放表情。应用后在礼物发放、表情商店、任务或兑换奖励中选择新表情。\n保留玩家存档和部署配置；不同更新包须依次基于已应用资源生成。请在Android核对聊天编成、图片、声音和多人接收。\n")
}

func finishCustomResourcePatch(files, original map[string][]byte, assets, manifest map[string]any, read func(string) ([]byte, error), reportFields map[string]any, instructions string) ([]byte, error) {
	const versionPath = "resources/patch/version.dat"
	encoded, err := read(versionPath)
	if err != nil {
		return nil, err
	}
	key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
	decoded := bytes.Clone(encoded)
	for i := range decoded {
		decoded[i] -= key[i%8]
	}
	r := csv.NewReader(bytes.NewReader(decoded))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	changed := map[string]string{}
	for name, b := range files {
		if strings.HasPrefix(name, "resources/patch/") && strings.HasSuffix(name, ".dat") {
			changed[strings.TrimPrefix(name, "resources/patch/")] = fmt.Sprintf("%08X", crc32.ChecksumIEEE(b))
		}
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if len(row) == 4 && row[0] == "<bundle_ver>" {
			if crc, ok := changed[row[1]]; ok {
				if seen[row[1]] {
					return nil, errors.New("版本表资源路径重复")
				}
				row[3] = crc
				seen[row[1]] = true
			}
		}
	}
	if !seen["main_c/container.dat"] {
		return nil, errors.New("版本表缺少container")
	}
	names := []string{}
	for name := range changed {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		rows = append(rows, []string{"<bundle_ver>", name, "0", changed[name]})
	}
	var out bytes.Buffer
	cw := csv.NewWriter(&out)
	cw.WriteAll(rows)
	if err = cw.Error(); err != nil {
		return nil, err
	}
	version := out.Bytes()
	for i := range version {
		version[i] += key[i%8]
	}
	files[versionPath] = version
	plain, scrambled, edges := 0, 0, 0
	for _, v := range assets["bundles"].([]any) {
		b := v.(map[string]any)
		if crc, ok := changed[b["bundle"].(string)]; ok {
			if b["scrambled"] != false {
				return nil, errors.New("更新资源编码不支持")
			}
			b["delivery_crc32"] = crc
		}
		if b["scrambled"] == true {
			scrambled++
		} else {
			plain++
		}
		if deps, ok := b["dependencies"].([]any); ok {
			edges += len(deps)
		}
	}
	source := assets["source"].(map[string]any)
	changeResourceHash(assets, resourceHash(encoded), resourceHash(version))
	source["version_dat_sha256"] = resourceHash(version)
	if vs, ok := source["version_manifests"].([]any); ok {
		for _, v := range vs {
			m := v.(map[string]any)
			if m["patch_root"] == "resources/patch" {
				m["sha256"] = resourceHash(version)
				m["versioned_bundle_count"] = plain + scrambled
			}
		}
	}
	source["plain_bundle_count"], source["scrambled_bundle_count"], source["parsed_unity_bundle_count"], source["bundle_dependency_edge_count"] = plain, scrambled, plain+scrambled, edges
	source["catalog_asset_count"] = len(assets["catalog_assets"].([]any))
	for _, key := range []string{"versioned_bundle_count", "present_versioned_bundle_count"} {
		source[key] = plain + scrambled
	}
	files["asset-map.json"], err = resourceJSON(assets)
	if err != nil {
		return nil, err
	}
	adminPath := "_local/control/server/cn602-admin-assets/manifest.json"
	var admin map[string]any
	if err = json.Unmarshal(files[adminPath], &admin); err != nil {
		return nil, err
	}
	admin["source"].(map[string]any)["asset_map_sha256"] = resourceHash(files["asset-map.json"])
	files[adminPath], err = resourceJSON(admin)
	if err != nil {
		return nil, err
	}
	inventory := manifest["files"].([]any)
	before := map[string]string{}
	delta, count := 0, 0
	for path, b := range files {
		matches := 0
		for _, v := range inventory {
			entry := v.(map[string]any)
			if entry["path"] != path {
				continue
			}
			matches++
			old, ok := original[path]
			if !ok || entry["sha256"] != resourceHash(old) || int(entry["bytes"].(float64)) != len(old) {
				return nil, fmt.Errorf("资源%s与当前清单不一致，请先同步资源清单", path)
			}
			before[path] = resourceHash(old)
			delta += len(b) - len(old)
			entry["bytes"], entry["sha256"] = len(b), resourceHash(b)
		}
		if matches > 1 {
			return nil, errors.New("资源清单路径重复")
		}
		if matches == 0 {
			if _, ok := original[path]; ok {
				return nil, fmt.Errorf("已有资源%s未登记到清单", path)
			}
			inventory = append(inventory, map[string]any{"path": path, "bytes": len(b), "sha256": resourceHash(b)})
			before[path] = ""
			delta += len(b)
			count++
		}
	}
	manifest["files"] = inventory
	summary := manifest["summary"].(map[string]any)
	for _, key := range []string{"bytes", "bytes_to_copy"} {
		if n, ok := summary[key].(float64); ok {
			summary[key] = int(n) + delta
		}
	}
	for _, key := range []string{"file_count", "files_to_copy"} {
		if n, ok := summary[key].(float64); ok {
			summary[key] = int(n) + count
		}
	}
	files["resource-set.json"], err = resourceJSON(manifest)
	if err != nil {
		return nil, err
	}
	before["resource-set.json"] = resourceHash(original["resource-set.json"])
	names = names[:0]
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	write := func(name string, body []byte) error {
		w, e := z.Create(name)
		if e != nil {
			return e
		}
		_, e = w.Write(body)
		return e
	}
	for _, name := range names {
		if err = write("resource-set/"+name, files[name]); err != nil {
			return nil, err
		}
	}
	reportFields["base_sha256"] = before
	report, err := resourceJSON(reportFields)
	if err != nil {
		return nil, err
	}
	if err = write("configuration.json", report); err != nil {
		return nil, err
	}
	w, err := z.Create("README.txt")
	if err != nil {
		return nil, err
	}
	_, err = io.WriteString(w, instructions)
	if err != nil {
		return nil, err
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
