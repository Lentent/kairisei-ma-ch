package admin

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

const customBossAppearancePath = "_local/control/server/cn602-custom-boss-appearances.json"

type customBossAppearance struct {
	CoverPictureID int    `json:"cover_picture_id,omitempty"`
	SourceBossID   int    `json:"source_boss_id"`
	Fingerprint    string `json:"fingerprint"`
	Parties        []int  `json:"parties"`
}

func customBossCoverID(boss int) int { return 97300000 + boss - customBossFirstID }

func customBossCoverArtwork(b customBoss) []byte {
	if len(b.CoverArtwork) > 0 {
		return b.CoverArtwork
	}
	for _, t := range b.Targets {
		if t.BattleIndex == 0 && t.EnemyIndex == 0 && len(t.Artwork) > 0 {
			return t.Artwork
		}
	}
	return nil
}

func customBossSeparateWave(b customBoss, wave int, enemies map[int][]string) bool {
	if b.ArtworkLayout == "template" {
		return false
	}
	var group struct {
		Bosses []struct {
			Model int `json:"14"`
		} `json:"10"`
	}
	if json.Unmarshal(b.SourceGroup, &group) != nil || len(group.Bosses) != 1 || group.Bosses[0].Model != 0 {
		return false
	}
	uploaded := b.ArtworkLayout == "separate"
	for _, t := range b.Targets {
		if t.BattleIndex != wave {
			continue
		}
		row := enemies[t.Stats.EnemyID]
		if len(row) < 25 || row[4] == "" || row[4] == "0" || row[5] != "" {
			return false
		}
		uploaded = uploaded || len(t.Artwork) > 0
	}
	return uploaded
}

// Reserve separate client identities for every wave and slot. Server combat
// continues to use the original party, enemy IDs, AI and skill definitions.
func customBossPartyID(boss, wave int) int { return 900000000 + (boss-customBossFirstID)*64 + wave }
func customBossEnemyID(boss, wave, slot int) int {
	return 910000000 + (boss-customBossFirstID)*256 + wave*4 + slot
}
func customBossImageID(boss, wave, slot int) int {
	// 98xxxxxx is reserved for custom cards, which share chr50 resource names.
	return 97000000 + (boss-customBossFirstID)*256 + wave*4 + slot
}

func decodeCustomBossArtwork(raw []byte) (image.Image, error) {
	if len(raw) == 0 || len(raw) > 4<<20 {
		return nil, errors.New("单张部位PNG须为4MB以内")
	}
	c, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || c.Width < 64 || c.Height < 64 || c.Width > 4096 || c.Height > 4096 || c.Width*c.Height > 8*1024*1024 {
		return nil, errors.New("部位图片须为PNG，边长64–4096且总像素不超过800万")
	}
	return png.Decode(bytes.NewReader(raw))
}

func customBossAppearanceHash(b customBoss) string {
	type part struct {
		Wave      int                         `json:"wave"`
		Slot      int                         `json:"slot"`
		Name      string                      `json:"name"`
		Art       string                      `json:"art"`
		Transform *customBossArtworkTransform `json:"transform,omitempty"`
	}
	parts := []part{}
	for _, t := range b.Targets {
		parts = append(parts, part{t.BattleIndex, t.EnemyIndex, t.Name, resourceHash(t.Artwork), t.ArtworkTransform})
	}
	slices.SortFunc(parts, func(a, b part) int { return (a.Wave*4 + a.Slot) - (b.Wave*4 + b.Slot) })
	raw, _ := json.Marshal(struct {
		Source  int
		Parts   []part
		Layout  string
		Cover   string
		Version int
	}{b.SourceBossID, parts, b.ArtworkLayout, resourceHash(customBossCoverArtwork(b)), 2})
	return resourceHash(raw)
}

func (c *contentStore) loadCustomBossAppearances(path string) error {
	c.customBossAppearances = map[int]customBossAppearance{}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &c.customBossAppearances); err != nil {
		return err
	}
	for id, a := range c.customBossAppearances {
		if a.CoverPictureID != 0 && a.CoverPictureID != customBossCoverID(id) {
			return errors.New("自制Boss封面编号无效")
		}
		if id < customBossFirstID || id > customBossLastID || a.SourceBossID < 1 || len(a.Parties) < 1 || len(a.Parties) > 64 || len(a.Fingerprint) != 64 {
			return errors.New("自制Boss外观发布记录无效")
		}
		for i, party := range a.Parties {
			if party != customBossPartyID(id, i) {
				return errors.New("自制Boss外观编队编号无效")
			}
		}
	}
	return nil
}

func (c *contentStore) customBossAppearanceCurrent(b customBoss) bool {
	if a, ok := c.customBossAppearances[b.BossID]; ok {
		return a.SourceBossID == b.SourceBossID && a.Fingerprint == customBossAppearanceHash(b)
	}
	source := c.bosses[b.SourceBossID]
	if len(b.CoverArtwork) > 0 || b.ArtworkLayout == "separate" {
		return false
	}
	for _, t := range b.Targets {
		i := slices.IndexFunc(source.Targets, func(s dropTarget) bool { return s.BattleIndex == t.BattleIndex && s.EnemyIndex == t.EnemyIndex })
		if i < 0 || t.Name != source.Targets[i].Name || len(t.Artwork) > 0 {
			return false
		}
	}
	return true
}

func (a *API) exportCustomBossAppearance(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		Expected *int `json:"expected_revision"`
		BossID   int  `json:"boss_id"`
	}
	if err := decodeAdminJSON(r, &input); err != nil || input.Expected == nil {
		WriteAdminError(w, 400, "请提交已保存Boss的版本和编号")
		return
	}
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	if o.content == nil {
		WriteAdminError(w, 503, "未载入Boss目录")
		return
	}
	if *input.Expected != o.content.customBossRevision {
		WriteAdminError(w, 409, "配置已变化，请重新载入后生成")
		return
	}
	i := slices.IndexFunc(o.content.customBosses, func(b customBoss) bool { return b.BossID == input.BossID })
	if i < 0 {
		WriteAdminError(w, 404, "未找到自制Boss")
		return
	}
	b := o.content.customBosses[i]
	if err := validateCustomBoss(b, o.content.bosses[b.SourceBossID].Targets); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	patch, err := a.buildCustomBossAppearancePatch(b)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=custom-boss-%d-v%d.zip", b.BossID, *input.Expected))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(patch)
}

func customBossCSVRows(text string, firstWins ...bool) (map[int][]string, error) {
	rows := map[int][]string{}
	for _, line := range strings.Split(text, "\n") {
		id, _, _ := strings.Cut(line, ",")
		n, err := strconv.Atoi(strings.Trim(id, "\"\r\ufeff"))
		if err != nil || n < 1 {
			continue
		}
		r := csv.NewReader(strings.NewReader(line))
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		row, err := r.Read()
		if err != nil {
			return nil, err
		}
		if _, exists := rows[n]; exists {
			// Official enemy_target.csv contains historical duplicates. Its
			// client loader keeps the first row; mirror that without rewriting it.
			if len(firstWins) > 0 && firstWins[0] {
				continue
			}
			return nil, fmt.Errorf("模板资料%d重复", n)
		}
		rows[n] = row
	}
	return rows, nil
}

func customBossWaves(b customBoss) []gamestate.TeamBattleReplayBattle {
	if len(b.Replay.Battles) > 0 {
		return b.Replay.Battles
	}
	return []gamestate.TeamBattleReplayBattle{{EnemyPartyID: b.Replay.EnemyPartyID, EnemyType: b.Replay.EnemyType}}
}

func (a *API) buildCustomBossAppearancePatch(boss customBoss) (result []byte, err error) {
	defer func() {
		if v := recover(); v != nil {
			result = nil
			err = fmt.Errorf("Boss资源布局不支持，未生成更新包：%v", v)
		}
	}()
	root, err := a.collectionRoot()
	if err != nil {
		return nil, err
	}
	files, original := map[string][]byte{}, map[string][]byte{}
	read := func(name string) ([]byte, error) {
		if !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\:") {
			return nil, errors.New("资源路径无效")
		}
		raw, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if e == nil {
			original[name] = raw
		}
		return raw, e
	}
	load := func(name string) (map[string]any, error) {
		raw, e := read(name)
		if e != nil {
			return nil, e
		}
		var m map[string]any
		e = json.Unmarshal(raw, &m)
		return m, e
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
	files[adminPath], err = read(adminPath)
	if err != nil {
		return nil, err
	}
	installed := &contentStore{}
	if err = installed.loadCustomBossAppearances(filepath.Join(root, filepath.FromSlash(customBossAppearancePath))); err != nil {
		return nil, err
	}
	if _, err = read(customBossAppearancePath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	_, owned := installed.customBossAppearances[boss.BossID]
	if owned && installed.customBossAppearances[boss.BossID].SourceBossID != boss.SourceBossID {
		return nil, errors.New("Boss外观编号已被其他模板占用")
	}
	waves := customBossWaves(boss)
	if len(waves) == 0 || len(waves) > 64 {
		return nil, errors.New("Boss波次数须为1至64")
	}
	const containerPath = "resources/patch/main_c/container.dat"
	raw, err := read(containerPath)
	if err != nil {
		return nil, err
	}
	container, err := readResourceBundle(raw)
	if err != nil {
		return nil, err
	}
	if len(container.nodes) != 1 {
		return nil, errors.New("客户端container须为单文件资源")
	}
	texts := map[string]string{}
	extract := map[string]func(string) (string, error){}
	for _, name := range []string{"enemy.csv", "enemy_party.csv", "enemy_lvup.csv", "enemy_target.csv", "enemy_uipos.csv"} {
		extract[name] = func(s string) (string, error) { texts[name] = s; return s, nil }
	}
	if _, err = replaceResourceTexts(container.nodes[0].data, extract); err != nil {
		return nil, err
	}
	enemies, err := customBossCSVRows(texts["enemy.csv"])
	if err != nil {
		return nil, err
	}
	parties, err := customBossCSVRows(texts["enemy_party.csv"])
	if err != nil {
		return nil, err
	}
	levels, err := customBossCSVRows(texts["enemy_lvup.csv"])
	if err != nil {
		return nil, err
	}
	targets, err := customBossCSVRows(texts["enemy_target.csv"], true)
	if err != nil {
		return nil, err
	}
	changes := map[string]map[string][]string{"enemy.csv": {}, "enemy_party.csv": {}, "enemy_lvup.csv": {}, "enemy_target.csv": {}, "enemy_uipos.csv": {}}
	newParties := make([]int, len(waves))
	bundles := assets["bundles"].([]any)
	catalog := assets["catalog_assets"].([]any)
	textureBytes := 0
	for wave, w := range waves {
		party := slices.Clone(parties[w.EnemyPartyID])
		if len(party) < 14 {
			return nil, errors.New("模板编队资料不完整")
		}
		newParty := customBossPartyID(boss.BossID, wave)
		if _, exists := parties[newParty]; exists && !owned {
			return nil, errors.New("自制Boss编队编号已被占用")
		}
		party[0] = strconv.Itoa(newParty)
		newParties[wave] = newParty
		separate := customBossSeparateWave(boss, wave, enemies)
		count := 0
		for _, t := range boss.Targets {
			if t.BattleIndex == wave {
				count++
			}
		}
		if separate && count == 4 && !strings.Contains("\n"+texts["enemy_uipos.csv"], "\nSSSS,") {
			row := []string{"SSSS"}
			for _, x := range []int{-288, -96, 96, 288} {
				row = append(row, strconv.Itoa(x), "0", "0", "0", "-75", "-10", "140", "-75", "-10", "140")
			}
			row = append(row, "")
			changes["enemy_uipos.csv"]["SSSS"] = row
		}
		for _, t := range boss.Targets {
			if t.BattleIndex != wave {
				continue
			}
			if t.EnemyIndex < 0 || t.EnemyIndex > 3 || t.Stats == nil {
				return nil, errors.New("部位结构无效")
			}
			slot := 2 + t.EnemyIndex*3
			if slot >= len(party) || party[slot] != strconv.Itoa(t.Stats.EnemyID) {
				return nil, fmt.Errorf("客户端模板编队与服务端不一致：第%d波部位%d，编队%d，敌人%d", wave+1, t.EnemyIndex+1, w.EnemyPartyID, t.Stats.EnemyID)
			}
			enemy := slices.Clone(enemies[t.Stats.EnemyID])
			lv := slices.Clone(levels[t.Stats.EnemyID])
			if len(enemy) < 14 || len(lv) < 2 {
				return nil, errors.New("客户端模板敌人资料不完整")
			}
			newEnemy := customBossEnemyID(boss.BossID, wave, t.EnemyIndex)
			if _, exists := enemies[newEnemy]; exists && !owned {
				return nil, errors.New("自制Boss部位编号已被占用")
			}
			if _, exists := levels[newEnemy]; exists && !owned {
				return nil, errors.New("自制Boss部位等级编号已被占用")
			}
			id := strconv.Itoa(newEnemy)
			party[slot] = id
			enemy[0], enemy[6], lv[0] = id, t.Name, id
			target := slices.Clone(targets[t.Stats.EnemyID])
			if len(target) < 9 {
				target = []string{id, "0", "0", "0", "0", "0", "0", "0", "0", ""}
			}
			if _, exists := targets[newEnemy]; exists && !owned {
				return nil, errors.New("自制Boss目标坐标编号已被占用")
			}
			target[0] = id
			if separate {
				size := "S"
				if count == 1 {
					size = "L"
				} else if count == 2 {
					size = "M"
				}
				enemy[24] = size
				// Client targets anchor to their own separated sprite. Server parent
				// relations remain in the unchanged original combat party.
				party[slot+2] = strconv.Itoa(t.EnemyIndex)
				for j := 1; j <= 4; j++ {
					target[j] = "0"
				}
			}
			changes["enemy_target.csv"][id] = target
			if len(t.Artwork) > 0 {
				if enemy[5] != "" || enemy[4] == "" || enemy[4] == "0" {
					return nil, errors.New("该部位不是普通2D图片")
				}
				oldPict, e := strconv.Atoi(enemy[4])
				if e != nil {
					return nil, e
				}
				newPict := customBossImageID(boss.BossID, wave, t.EnemyIndex)
				bundleName := fmt.Sprintf("main_c/image/custom_boss_%d_%d_%d.dat", boss.BossID, wave, t.EnemyIndex)
				path := "resources/patch/" + bundleName
				if _, e = read(path); e == nil && !owned {
					return nil, errors.New("Boss图片资源路径已被占用")
				} else if e != nil && !os.IsNotExist(e) {
					return nil, e
				}
				var entries []map[string]any
				sourceBundle := ""
				for _, v := range catalog {
					entry := v.(map[string]any)
					if entry["name"] == fmt.Sprintf("chr50_%08d", oldPict) {
						name := entry["bundle"].(string)
						if sourceBundle != "" && sourceBundle != name {
							return nil, errors.New("部位图片模板目录重复")
						}
						sourceBundle = name
						entries = append(entries, entry)
					}
				}
				if sourceBundle == "" {
					return nil, errors.New("找不到该部位的2D贴图模板")
				}
				scrambled, found := false, 0
				for _, v := range bundles {
					info := v.(map[string]any)
					if info["bundle"] == sourceBundle {
						scrambled = info["scrambled"] == true
						found++
					}
				}
				if found != 1 {
					return nil, errors.New("图片模板资源记录无效")
				}
				template, e := read("resources/patch/" + sourceBundle)
				if e != nil {
					return nil, e
				}
				delete(original, "resources/patch/"+sourceBundle)
				paths := map[string]string{}
				for _, entry := range entries {
					p, e := customArtworkEntryContainerPath(entry)
					if e != nil {
						return nil, e
					}
					paths[p] = customArtworkResourcePath(p, oldPict, newPict)
				}
				art, e := decodeCustomBossArtwork(t.Artwork)
				if e != nil {
					return nil, e
				}
				texture, cab, e := buildCustomArtworkBundle(template, scrambled, oldPict, newPict, art, paths, customArtworkOverrides{cardMode: "texture", bossTransform: t.ArtworkTransform})
				if e != nil {
					return nil, e
				}
				files[path] = texture
				textureBytes += len(texture)
				if textureBytes > 512<<20 {
					return nil, errors.New("Boss外观资源超过512MB，请减少图片数量")
				}
				for _, v := range catalog {
					entry := v.(map[string]any)
					if entry["name"] == fmt.Sprintf("chr50_%08d", newPict) && entry["bundle"] != bundleName {
						return nil, errors.New("Boss图片编号已被占用")
					}
				}
				bundles = slices.DeleteFunc(bundles, func(v any) bool { return v.(map[string]any)["bundle"] == bundleName })
				bundles = append(bundles, map[string]any{"bundle": bundleName, "cab_name": cab, "scrambled": false, "delivery_crc32": fmt.Sprintf("%08X", crc32.ChecksumIEEE(texture)), "dependencies": []any{}})
				catalog = slices.DeleteFunc(catalog, func(v any) bool { return v.(map[string]any)["bundle"] == bundleName })
				for _, entry := range entries {
					next := map[string]any{}
					for key, value := range entry {
						next[key] = value
						if s, ok := value.(string); ok && (key == "name" || key == "directory" || key == "container_path") {
							next[key] = customArtworkResourcePath(s, oldPict, newPict)
						}
					}
					next["bundle"] = bundleName
					catalog = append(catalog, next)
				}
				enemy[4] = strconv.Itoa(newPict)
			}
			changes["enemy.csv"][id] = enemy
			changes["enemy_lvup.csv"][id] = lv
		}
		changes["enemy_party.csv"][strconv.Itoa(newParty)] = party
	}
	replacements := map[string]func(string) (string, error){}
	for name, rows := range changes {
		replacements[name] = func(s string) (string, error) { return rewriteResourceCSV(s, rows) }
	}
	container.nodes[0].data, err = replaceResourceTexts(container.nodes[0].data, replacements)
	if err != nil {
		return nil, err
	}
	files[containerPath] = container.save()
	assets["bundles"], assets["catalog_assets"] = bundles, catalog
	coverID, err := buildCustomBossCoverResource(boss, assets, files, original, read, owned)
	if err != nil {
		return nil, err
	}
	installed.customBossAppearances[boss.BossID] = customBossAppearance{SourceBossID: boss.SourceBossID, Fingerprint: customBossAppearanceHash(boss), Parties: newParties, CoverPictureID: coverID}
	files[customBossAppearancePath], err = resourceJSON(installed.customBossAppearances)
	if err != nil {
		return nil, err
	}
	return finishCustomResourcePatch(files, original, assets, manifest, read, map[string]any{"boss_id": boss.BossID, "appearance": installed.customBossAppearances[boss.BossID]}, "自制Boss外观资源更新包\n正常停服并备份资源与数据库。核对configuration.json的base_sha256与当前文件一致（空值为新文件），再按原路径合并resource-set并重启。使用CDN时同步资源与清单。客户端完全退出、重新登录并下载资源后生效，无需修改APK。\n保存配置和下载ZIP不会应用图片或部位名。已应用的外观保持到下次应用更新包；数值与行动保存后用于新开房。仅替换当前复制品，不覆盖原Boss。不同更新包须依次基于已应用的资源生成。\n在手机核对各波次的PNG、部位名、位置、点击目标、破坏与出招。完整角色PNG可选独立排列，分层部件图可选模板拼接；部位数量与实际战斗破坏关系沿用模板。更新早期版本后须重新生成并应用本包，补齐封面和目标坐标。不支持新增骨骼、Live2D或3D模型。\n")
}
