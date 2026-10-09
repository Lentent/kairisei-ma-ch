package admin

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/protocol"
)

// Unlike item.csv, skills legitimately have several rows with the same ID.
func rewriteCustomCSV(text string, changes map[string][][]string) (string, error) {
	encode := func(rows [][]string) (string, error) {
		var b strings.Builder
		for _, row := range rows {
			// The client retains quote characters and parses one physical line
			// at a time. RFC 4180 escaping would change inherited fields.
			line := strings.Join(row, ",")
			parsed := protocol.SplitCSVLine(line)
			if strings.ContainsAny(line, "\r\n\x00") || len(parsed) != len(row) {
				return "", errors.New("卡牌文本不支持换行或未配对的引号、英文逗号，请使用中文标点")
			}
			for i, field := range parsed {
				if field != row[i] {
					return "", errors.New("卡牌文本不符合客户端CSV格式，请使用中文标点")
				}
			}
			b.WriteString(line)
			b.WriteString("\r\n")
		}
		return b.String(), nil
	}
	lines := strings.SplitAfter(text, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		id, _, _ := strings.Cut(line, ",")
		id = strings.Trim(id, "\"")
		if rows, ok := changes[id]; ok {
			lines[i] = ""
			if !seen[id] {
				var e error
				lines[i], e = encode(rows)
				if e != nil {
					return "", e
				}
				seen[id] = true
			}
		}
	}
	out := strings.Join(lines, "")
	ids := []string{}
	for id := range changes {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > 0 && out != "" && !strings.HasSuffix(out, "\n") {
		out += "\r\n"
	}
	for _, id := range ids {
		v, e := encode(changes[id])
		if e != nil {
			return "", e
		}
		out += v
	}
	return out, nil
}

func materializeCustomCard(c customCard, s customCardSources) ([]string, [][]string, [][]string, gamestate.Card, error) {
	if e := validateCustomClientSkillCapacity(c); e != nil {
		return nil, nil, nil, gamestate.Card{}, e
	}
	row := append([]string(nil), s.Cards[c.TemplateID]...)
	id := strconv.Itoa(c.ID)
	row[0], row[1], row[2], row[3] = id, id, id, id
	row[4], row[5], row[9] = c.Prefix, c.Name, strconv.Itoa(c.Cost)
	initial, maximum, bonus := customParameterValues(c.Initial), customParameterValues(c.Maximum), customParameterValues(c.LoveBonus)
	for i := range initial {
		row[10+i*3] = strconv.Itoa(initial[i])
		row[11+i*3] = strconv.Itoa(maximum[i])
		row[12+i*3] = strconv.Itoa(bonus[i])
	}
	ids := customSkillIDs(c)
	for _, i := range []int{26, 27} {
		old := customRowInt(row, i)
		if old != 0 {
			row[i] = strconv.Itoa(ids[old])
		}
	}
	if len(c.Artwork) > 0 {
		row[36], row[38] = id, id
	}
	skills, roles := cloneCustomRows(c.Skills), cloneCustomRows(c.Roles)
	job := []string{"COMMON", "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER"}[c.ArthurType]
	for _, r := range skills {
		source := customRowInt(r, 0)
		r[0] = strconv.Itoa(ids[source])
		fn := customRowInt(r, 49)
		if fn == 0 {
			fn = source
		}
		r[49] = strconv.Itoa(ids[fn])
		r[11], r[12], r[14] = c.Attribute, job, strconv.Itoa(c.Cost)
	}
	base, e := s.template(c.TemplateID)
	if e != nil {
		return nil, nil, nil, gamestate.Card{}, e
	}
	cutin, directionRows, e := customCardPresentation(c, s, nil)
	if e != nil {
		return nil, nil, nil, gamestate.Card{}, e
	}
	row[35] = cutin
	// The client reads a function's direction from its first role row only.
	// Reordering/removing effects must never replace the selected direction
	// with an empty continuation row or an unrelated effect's references.
	directions := map[int][]string{}
	for fn, r := range directionRows {
		directions[fn] = r[1:8]
	}
	for _, r := range roles {
		fn := customRowInt(r, 0)
		copy(r[1:8], directions[fn])
		r[0] = strconv.Itoa(ids[fn])
		for i, kind := range s.Rules[r[8]] {
			if kind == "ATTR" && r[20+i] == base.Attribute {
				r[20+i] = c.Attribute
			}
		}
	}
	var card gamestate.Card
	for _, v := range s.Master.CardTemplates {
		if v.CardID == c.TemplateID {
			card = v
			break
		}
	}
	card.CardID, card.SameCardID, card.SameSupportCardID = c.ID, c.ID, c.ID
	card.Name = c.Name
	card.ParameterInitial, card.ParameterMaximum, card.ParameterLoveMaximumBonus = c.Initial, c.Maximum, c.LoveBonus
	card.FusionAttributes = map[string]uint8{"FIRE": 1, "ICE": 2, "WIND": 4, "LIGHT": 8, "DARK": 16}[c.Attribute]
	card.AcquisitionText = "自制卡牌／扭蛋／运营赠礼"
	p, e := s.Master.CardProgressionPolicy.ParametersAt(card, card.Level, card.Love, card.Fame)
	if e != nil {
		return nil, nil, nil, card, e
	}
	card.HP, card.Attack, card.Magic, card.Mind = p.HP, p.Attack, p.Magic, p.Mind
	return row, skills, roles, card, nil
}

func (a *API) buildCustomCardPatch(d customCardDraft, s customCardSources) (result []byte, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("资源布局不支持，未生成更新包：%v", v)
		}
	}()
	root := a.collectionResourceRoot
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
	put := func(name string, v any) error {
		b, e := resourceJSON(v)
		if e == nil {
			files[name] = b
		}
		return e
	}
	manifest, e := load("resource-set.json")
	if e != nil {
		return nil, e
	}
	assets, e := load("asset-map.json")
	if e != nil {
		return nil, e
	}
	adminPath := "_local/control/server/cn602-admin-assets/manifest.json"
	admin, e := load(adminPath)
	if e != nil {
		return nil, e
	}
	if _, e = read(customCardMasterPath); e != nil {
		return nil, e
	}
	master := s.Master
	cards, skills, roles := map[string][][]string{}, map[string][][]string{}, map[string][][]string{}
	receipts := []customCardReceipt{}
	added, resolved, gaps := 0, 0, []any{}
	for _, c := range d.Cards {
		row, skillRows, roleRows, card, e := materializeCustomCard(c, s)
		if e != nil {
			return nil, e
		}
		cards[row[0]] = [][]string{row}
		for _, r := range skillRows {
			skills[r[0]] = append(skills[r[0]], r)
		}
		for _, r := range roleRows {
			roles[r[0]] = append(roles[r[0]], r)
		}
		found := false
		for i, v := range master.CardTemplates {
			if v.CardID == c.ID {
				master.CardTemplates[i] = card
				found = true
				break
			}
		}
		if !found {
			master.CardTemplates = append(master.CardTemplates, card)
			added++
		}
		rule := master.DeckRankPolicy.Cards[c.TemplateID]
		oldJob := rule.ArthurType
		rule.ArthurType = c.ArthurType
		rule.MaximumParameters = customParameterValues(c.Maximum)
		points, bonus := rule.SkillPoints[0], 0
		if oldJob > 0 {
			bonus = rule.SkillPoints[oldJob] - points
		}
		for i := range rule.SkillPoints {
			rule.SkillPoints[i] = points
		}
		if c.ArthurType > 0 {
			rule.SkillPoints[c.ArthurType] += bonus
		}
		master.DeckRankPolicy.Cards[c.ID] = rule
		inCollection := false
		for _, page := range master.CardCollectionPages {
			for _, id := range page {
				inCollection = inCollection || id == c.ID
			}
		}
		if !inCollection {
			inserted := false
			for _, page := range master.CardCollectionPages {
				for i, id := range page {
					if id == 0 {
						page[i] = c.ID
						inserted = true
						break
					}
				}
				if inserted {
					break
				}
			}
			if !inserted {
				page := make([]int, 10)
				page[0] = c.ID
				master.CardCollectionPages = append(master.CardCollectionPages, page)
			}
		}
		if len(c.Artwork) > 0 {
			if e = buildCustomCardImages(c, row, s, assets, manifest, files, original, root); e != nil {
				return nil, e
			}
		} else {
			entry := a.catalogByKey[adminCatalogKey(6, c.TemplateID)]
			if entry.ImageURL == "" {
				if !found {
					gaps = append(gaps, c.ID)
				}
			} else {
				source := strings.TrimPrefix(entry.ImageURL, "/assets/")
				name := "_local/control/server/cn602-admin-assets/" + source
				b, e := read(name)
				if e != nil {
					return nil, e
				}
				dest := fmt.Sprintf("_local/control/server/cn602-admin-assets/card/%d.webp", c.ID)
				if _, e = os.Stat(filepath.Join(root, filepath.FromSlash(dest))); e == nil {
					if _, e = read(dest); e != nil {
						return nil, e
					}
				}
				files[dest] = b
			}
		}
		if !found && (len(c.Artwork) > 0 || a.catalogByKey[adminCatalogKey(6, c.TemplateID)].ImageURL != "") {
			resolved++
		}
		receipts = append(receipts, customCardReceipt{ID: c.ID, TemplateID: c.TemplateID, Artwork: len(c.Artwork) > 0})
	}
	for path, updates := range map[string]map[string][][]string{customCardCSVPath: cards, customSkillCSVPath: skills, customRoleCSVPath: roles} {
		raw, e := read(path)
		if e != nil {
			return nil, e
		}
		next, e := rewriteCustomCSV(string(raw), updates)
		if e != nil {
			return nil, e
		}
		files[path] = []byte(next)
	}
	// Advancing the progression contract lets existing owned copies be normalized
	// to edited custom stats without resetting level, fame, loyalty or ownership.
	master.CardProgressionConfigVersion++
	master.CardProgressionPolicy.ConfigVersion = master.CardProgressionConfigVersion
	var source map[string]any
	if e = json.Unmarshal(master.Source, &source); e != nil {
		return nil, e
	}
	source["admin_custom_cards"] = receipts
	for _, v := range source["files"].([]any) {
		r := v.(map[string]any)
		for path, b := range files {
			if filepath.Base(fmt.Sprint(r["path"])) == filepath.Base(path) {
				r["bytes"], r["sha256"] = len(b), resourceHash(b)
			}
		}
	}
	master.Source, e = json.Marshal(source)
	if e != nil {
		return nil, e
	}
	if e = put(customCardMasterPath, master); e != nil {
		return nil, e
	}
	containerPath := "resources/patch/main_c/container.dat"
	raw, e := read(containerPath)
	if e != nil {
		return nil, e
	}
	bundle, e := readResourceBundle(raw)
	if e != nil {
		return nil, e
	}
	if len(bundle.nodes) != 1 {
		return nil, errors.New("客户端container布局不支持")
	}
	updates := map[string]func(string) (string, error){}
	for name, rows := range map[string]map[string][][]string{"card.csv": cards, "skill_player.csv": skills, "skill_role_player.csv": roles} {
		updates[name] = func(text string) (string, error) { return rewriteCustomCSV(text, rows) }
	}
	bundle.nodes[0].data, e = replaceResourceTexts(bundle.nodes[0].data, updates)
	if e != nil {
		return nil, e
	}
	files[containerPath] = bundle.save()
	if e = validateGeneratedCustomCardMaster(files[customCardMasterPath]); e != nil {
		return nil, e
	}
	if e = validateGeneratedCustomCombat(root, files); e != nil {
		return nil, fmt.Errorf("技能组合校验失败：%w", e)
	}
	coverage := admin["catalog_image_coverage"].(map[string]any)["card"].(map[string]any)
	coverage["entry_count"] = int(coverage["entry_count"].(float64)) + added
	coverage["resolved_source_count"] = int(coverage["resolved_source_count"].(float64)) + resolved
	coverage["source_gap_count"] = int(coverage["source_gap_count"].(float64)) + len(gaps)
	gapIDs, _ := coverage["source_gap_ids"].([]any)
	coverage["source_gap_ids"] = append(gapIDs, gaps...)
	admin["exported"].(map[string]any)["card"] = int(admin["exported"].(map[string]any)["card"].(float64)) + resolved
	adminSource := admin["source"].(map[string]any)
	adminSource["card_master_sha256"] = resourceHash(files[customCardMasterPath])
	adminSource["card_source_sha256"] = resourceHash(files[customCardCSVPath])
	if e = put(adminPath, admin); e != nil {
		return nil, e
	}
	return packageCustomCardResources(d, root, files, original, manifest, assets)
}

func validateGeneratedCustomCardMaster(raw []byte) error {
	dir, e := os.MkdirTemp("", "kairi-custom-card-check-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "cards.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		return e
	}
	_, e = masterdata.LoadCardRuntimeMaster(path)
	return e
}

func validateGeneratedCustomCombat(root string, files map[string][]byte) error {
	dir, e := os.MkdirTemp("", "kairi-custom-combat-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	battle := filepath.Join(root, "_local/control/server/cn602-battle-master")
	entries, e := os.ReadDir(battle)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		name := "_local/control/server/cn602-battle-master/" + entry.Name()
		raw, ok := files[name]
		if !ok {
			raw, e = os.ReadFile(filepath.Join(battle, entry.Name()))
			if e != nil {
				return e
			}
		}
		if e = os.WriteFile(filepath.Join(dir, entry.Name()), raw, 0600); e != nil {
			return e
		}
	}
	cardPath := filepath.Join(dir, "card.csv")
	if e = os.WriteFile(cardPath, files[customCardCSVPath], 0600); e != nil {
		return e
	}
	catalog, e := multiplayer.LoadCombatCatalog(cardPath, dir)
	if e != nil {
		return e
	}
	if e = catalog.ValidateRoleParameterContracts(); e != nil {
		return e
	}
	return catalog.ValidatePlayerFunctionCoverage()
}

func buildCustomCardImages(c customCard, row []string, s customCardSources, assets, manifest map[string]any, files, original map[string][]byte, root string) error {
	im, e := decodeCustomArtwork(c.Artwork)
	if e != nil {
		return e
	}
	oldPict := customRowInt(s.Cards[c.TemplateID], 36)
	oldID := fmt.Sprintf("%08d", oldPict)
	groups := map[string][]map[string]any{}
	for _, v := range assets["catalog_assets"].([]any) {
		entry := v.(map[string]any)
		if strings.HasSuffix(fmt.Sprint(entry["name"]), "_"+oldID) {
			groups[entry["bundle"].(string)] = append(groups[entry["bundle"].(string)], entry)
		}
	}
	if len(groups) == 0 || len(groups) > 8 {
		return errors.New("无法定位模板卡面资源")
	}
	for sourceBundle, entries := range groups {
		var info map[string]any
		for _, v := range assets["bundles"].([]any) {
			b := v.(map[string]any)
			if b["bundle"] == sourceBundle {
				info = b
				break
			}
		}
		if info == nil {
			return errors.New("模板卡面资源清单缺失")
		}
		raw, e := os.ReadFile(filepath.Join(root, "resources/patch", filepath.FromSlash(sourceBundle)))
		if e != nil {
			return e
		}
		name := fmt.Sprintf("main_c/image/custom_card_%d_%s.dat", c.ID, resourceHash([]byte(sourceBundle))[:8])
		path := "resources/patch/" + name
		containerPaths := map[string]string{}
		for _, entry := range entries {
			if oldPath, ok := entry["container_path"].(string); ok && oldPath != "" {
				containerPaths[oldPath] = customArtworkResourcePath(oldPath, oldPict, c.ID)
			}
		}
		next, cab, e := buildCustomArtworkBundle(raw, info["scrambled"].(bool), oldPict, c.ID, im, containerPaths)
		if e != nil {
			return e
		}
		if old, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); e == nil {
			original[path] = old
		}
		files[path] = next
		resourceBytes := 0
		for _, data := range files {
			resourceBytes += len(data)
		}
		if resourceBytes > 512<<20 {
			return errors.New("卡面资源超过512MB，请减少自制卡面或分批制作")
		}
		newInfo := map[string]any{"bundle": name, "cab_name": cab, "scrambled": false, "delivery_crc32": fmt.Sprintf("%08X", crc32.ChecksumIEEE(next)), "dependencies": info["dependencies"]}
		bundles := assets["bundles"].([]any)
		replaced := false
		for i, v := range bundles {
			if v.(map[string]any)["bundle"] == name {
				bundles[i] = newInfo
				replaced = true
			}
		}
		if !replaced {
			bundles = append(bundles, newInfo)
		}
		assets["bundles"] = bundles
		catalog := assets["catalog_assets"].([]any)
		for _, entry := range entries {
			copy := map[string]any{}
			for k, v := range entry {
				copy[k] = v
			}
			copy["bundle"] = name
			for _, field := range []string{"name", "directory", "container_path"} {
				if value, ok := entry[field].(string); ok {
					copy[field] = customArtworkResourcePath(value, oldPict, c.ID)
				}
			}
			found := false
			nextCatalog := catalog[:0]
			for _, v := range catalog {
				r := v.(map[string]any)
				// Replace older exports by their owned bundle/name as well, so
				// regenerating an applied card repairs its stale template bucket.
				if r["bundle"] == name && r["name"] == copy["name"] {
					if !found {
						nextCatalog = append(nextCatalog, copy)
					}
					found = true
				} else {
					nextCatalog = append(nextCatalog, v)
				}
			}
			if !found {
				nextCatalog = append(nextCatalog, copy)
			}
			catalog = nextCatalog
		}
		assets["catalog_assets"] = catalog
	}
	png, e := encodeCustomArtworkPNG(im, 160, 160)
	if e != nil {
		return e
	}
	path := fmt.Sprintf("_local/control/server/cn602-admin-assets/card/%d.png", c.ID)
	if b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); e == nil {
		original[path] = b
	}
	files[path] = png
	// The enlarged-image service is optional in older resource sets. Where it
	// exists, include its separate manifest and PNG along with bundle textures.
	return addCustomEnlargedImage(c.ID, im, manifest, files, original, root)
}

func addCustomEnlargedImage(cardID int, im image.Image, manifest map[string]any, files, original map[string][]byte, root string) error {
	if imageRoot, ok := manifest["entrypoints"].(map[string]any)["cn-image-root"].(string); ok {
		path := strings.TrimSuffix(imageRoot, "/") + "/manifest.json"
		raw, edited := files[path]
		if !edited {
			var e error
			raw, e = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if e != nil {
				return e
			}
			original[path] = raw
		}
		var m map[string]any
		if e := json.Unmarshal(raw, &m); e != nil {
			return e
		}
		file := fmt.Sprintf("chr51/chr51_%08d.png", cardID)
		png, e := encodeCustomArtworkPNG(im, 1024, 1024)
		if e != nil {
			return e
		}
		dest := strings.TrimSuffix(imageRoot, "/") + "/" + file
		if b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(dest))); e == nil {
			original[dest] = b
		}
		files[dest] = png
		row := map[string]any{"path": file, "bytes": len(png), "sha256": resourceHash(png)}
		if _, e = mergeResourceRows(m, "files", "path", []any{row}); e != nil {
			return e
		}
		files[path], e = resourceJSON(m)
		if e != nil {
			return e
		}
	}
	return nil
}

func packageCustomCardResources(d customCardDraft, root string, files, original map[string][]byte, manifest, assets map[string]any) ([]byte, error) {
	versionPath := "resources/patch/version.dat"
	encoded, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(versionPath)))
	if e != nil {
		return nil, e
	}
	original[versionPath] = encoded
	decoded := bytes.Clone(encoded)
	key := []byte{1, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}
	for i := range decoded {
		decoded[i] -= key[i%8]
	}
	r := csv.NewReader(bytes.NewReader(decoded))
	r.FieldsPerRecord = -1
	rows, e := r.ReadAll()
	if e != nil {
		return nil, e
	}
	changed := map[string]string{}
	for path, b := range files {
		if strings.HasPrefix(path, "resources/patch/") && strings.HasSuffix(path, ".dat") {
			changed[strings.TrimPrefix(path, "resources/patch/")] = fmt.Sprintf("%08X", crc32.ChecksumIEEE(b))
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
	var vb bytes.Buffer
	w := csv.NewWriter(&vb)
	w.WriteAll(rows)
	if e = w.Error(); e != nil {
		return nil, e
	}
	version := vb.Bytes()
	for i := range version {
		version[i] += key[i%8]
	}
	files[versionPath] = version
	plain, scrambled, edges := 0, 0, 0
	for _, v := range assets["bundles"].([]any) {
		b := v.(map[string]any)
		if crc, ok := changed[b["bundle"].(string)]; ok {
			if b["scrambled"] != false {
				return nil, errors.New("更新资源使用不支持的编码")
			}
			b["delivery_crc32"] = crc
		}
		if b["scrambled"] == true {
			scrambled++
		} else {
			plain++
		}
		if dependencies, ok := b["dependencies"].([]any); ok {
			edges += len(dependencies)
		}
	}
	changeResourceHash(assets, resourceHash(encoded), resourceHash(version))
	source := assets["source"].(map[string]any)
	source["plain_bundle_count"], source["scrambled_bundle_count"], source["bundle_dependency_edge_count"] = plain, scrambled, edges
	source["parsed_unity_bundle_count"] = plain + scrambled
	files["asset-map.json"], e = resourceJSON(assets)
	if e != nil {
		return nil, e
	}
	if admin, ok := files["_local/control/server/cn602-admin-assets/manifest.json"]; ok {
		var m map[string]any
		if e = json.Unmarshal(admin, &m); e != nil {
			return nil, e
		}
		m["source"].(map[string]any)["asset_map_sha256"] = resourceHash(files["asset-map.json"])
		files["_local/control/server/cn602-admin-assets/manifest.json"], e = resourceJSON(m)
		if e != nil {
			return nil, e
		}
	}
	before := map[string]string{}
	inventory := manifest["files"].([]any)
	delta, count := 0, 0
	for name, b := range files {
		matches := 0
		for _, v := range inventory {
			row := v.(map[string]any)
			if row["path"] != name {
				continue
			}
			matches++
			old, ok := original[name]
			if !ok || row["sha256"] != resourceHash(old) || int(row["bytes"].(float64)) != len(old) {
				return nil, fmt.Errorf("资源%s与当前清单不一致", name)
			}
			before[name] = resourceHash(old)
			delta += len(b) - len(old)
			row["bytes"], row["sha256"] = len(b), resourceHash(b)
		}
		if matches > 1 {
			return nil, errors.New("资源清单路径重复")
		}
		if matches == 0 {
			if _, ok := original[name]; ok {
				return nil, fmt.Errorf("已有资源%s未登记", name)
			}
			inventory = append(inventory, map[string]any{"path": name, "bytes": len(b), "sha256": resourceHash(b)})
			delta += len(b)
			count++
		}
	}
	manifest["files"] = inventory
	summary := manifest["summary"].(map[string]any)
	for _, field := range []string{"bytes", "bytes_to_copy"} {
		if n, ok := summary[field].(float64); ok {
			summary[field] = int(n) + delta
		}
	}
	for _, field := range []string{"file_count", "files_to_copy"} {
		if n, ok := summary[field].(float64); ok {
			summary[field] = int(n) + count
		}
	}
	files["resource-set.json"], e = resourceJSON(manifest)
	if e != nil {
		return nil, e
	}
	before["resource-set.json"] = resourceHash(original["resource-set.json"])
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	names = names[:0]
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
	for i := range d.Cards {
		d.Cards[i].Artwork = nil
	}
	report, e := resourceJSON(map[string]any{"base_sha256": before, "cards": d.Cards})
	if e != nil {
		return nil, e
	}
	entry, e := z.Create("configuration.json")
	if e != nil {
		return nil, e
	}
	if _, e = entry.Write(report); e != nil {
		return nil, e
	}
	entry, e = z.Create("README.txt")
	if e != nil {
		return nil, e
	}
	_, e = io.WriteString(entry, "自制卡牌资源更新包\n停服并备份资源与数据库，核对configuration.json原文件SHA-256，将resource-set按原路径合并覆盖后重启。使用CDN时同步更新的资源；客户端重新下载后生效。\n保存草稿、下载ZIP不会自动发布，也不会发卡。应用后从礼物发放、卡池或兑换所提供新卡。\n卡面沿用模板尺寸并保持比例；技能条件、成长、稀有度及进化规则沿用模板，新卡不自动加入原进化链。出牌特写样式及各效果组的动作可选择现有卡牌来源，未选择时沿用模板；2D整段技能演出随动作来源切换。组合效果后须核对各分支技能说明。\n保留玩家存档和部署配置，不需要修改APK。请在Android验证卡面、编组、出牌和多人战斗。\n")
	if e != nil {
		return nil, e
	}
	if e = z.Close(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
