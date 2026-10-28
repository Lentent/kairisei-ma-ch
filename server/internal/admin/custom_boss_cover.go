package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Selection uses chr60 (or chr10 fallback), not the battle's chr50 texture.
func buildCustomBossCoverResource(b customBoss, assets map[string]any, files, original map[string][]byte, read func(string) ([]byte, error), owned bool) (int, error) {
	artwork := customBossCoverArtwork(b)
	if len(artwork) == 0 {
		return 0, nil
	}
	art, err := decodeCustomBossArtwork(artwork)
	if err != nil {
		return 0, err
	}
	newID := customBossCoverID(b.BossID)
	bundleName := fmt.Sprintf("main_c/image/custom_boss_%d_cover.dat", b.BossID)
	catalog, bundles := assets["catalog_assets"].([]any), assets["bundles"].([]any)
	for _, v := range catalog {
		e := v.(map[string]any)
		if e["name"] == fmt.Sprintf("chr60_%08d", newID) && e["bundle"] != bundleName {
			return 0, errors.New("Boss选关封面编号已被占用")
		}
	}
	entry, err := customBossCoverTemplate(b, catalog)
	if err != nil {
		return 0, err
	}
	name := entry["name"].(string)
	oldID, err := strconv.Atoi(strings.TrimPrefix(name, "chr60_"))
	if err != nil {
		return 0, err
	}
	sourceBundle := entry["bundle"].(string)
	scrambled, found := false, 0
	for _, v := range bundles {
		info := v.(map[string]any)
		if info["bundle"] == sourceBundle {
			scrambled = info["scrambled"] == true
			found++
		}
	}
	if found != 1 {
		return 0, errors.New("选关封面模板记录无效")
	}
	raw, err := read("resources/patch/" + sourceBundle)
	if err != nil {
		return 0, err
	}
	delete(original, "resources/patch/"+sourceBundle)
	oldPath, err := customArtworkEntryContainerPath(entry)
	if err != nil {
		return 0, err
	}
	newPath := customArtworkResourcePath(oldPath, oldID, newID)
	texture, cab, err := buildCustomArtworkBundle(raw, scrambled, oldID, newID, art, map[string]string{oldPath: newPath}, customArtworkOverrides{cardMode: "texture"})
	if err != nil {
		return 0, err
	}
	filePath := "resources/patch/" + bundleName
	if _, err = read(filePath); err == nil && !owned {
		return 0, errors.New("选关封面资源路径已被占用")
	} else if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	files[filePath] = texture
	bundles = slices.DeleteFunc(bundles, func(v any) bool { return v.(map[string]any)["bundle"] == bundleName })
	bundles = append(bundles, map[string]any{"bundle": bundleName, "cab_name": cab, "scrambled": false, "delivery_crc32": fmt.Sprintf("%08X", crc32.ChecksumIEEE(texture)), "dependencies": []any{}})
	catalog = slices.DeleteFunc(catalog, func(v any) bool { return v.(map[string]any)["bundle"] == bundleName })
	next := map[string]any{}
	for key, value := range entry {
		next[key] = value
		if s, ok := value.(string); ok && (key == "name" || key == "directory" || key == "container_path") {
			next[key] = customArtworkResourcePath(s, oldID, newID)
		}
	}
	next["bundle"] = bundleName
	catalog = append(catalog, next)
	assets["bundles"], assets["catalog_assets"] = bundles, catalog
	thumb := fmt.Sprintf("_local/control/server/cn602-admin-assets/boss/%d.png", newID)
	if _, err = read(thumb); err == nil && !owned {
		return 0, errors.New("Boss封面缩略图路径已被占用")
	} else if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	files[thumb], err = encodeCustomArtworkPNG(art, 160, 160)
	return newID, err
}

func customBossCoverTemplate(b customBoss, catalog []any) (map[string]any, error) {
	var group struct {
		Picture int `json:"7"`
		Bosses  []struct {
			Picture int `json:"9"`
		} `json:"10"`
	}
	if err := json.Unmarshal(b.SourceGroup, &group); err != nil {
		return nil, err
	}
	preferred := []string{fmt.Sprintf("chr60_%08d", group.Picture)}
	if len(group.Bosses) > 0 {
		preferred = append(preferred, fmt.Sprintf("chr60_%08d", group.Bosses[0].Picture))
	}
	var entry map[string]any
	for _, name := range preferred {
		for _, v := range catalog {
			e := v.(map[string]any)
			if e["name"] == name {
				entry = e
				break
			}
		}
		if entry != nil {
			break
		}
	}
	if entry == nil {
		// Some templates only have chr50. Use any official chr60 as a container;
		// its pixels are fully replaced with this Boss's independently owned PNG.
		for _, v := range catalog {
			e := v.(map[string]any)
			name, _ := e["name"].(string)
			bundle, _ := e["bundle"].(string)
			if strings.HasPrefix(name, "chr60_") && !strings.Contains(bundle, "custom_") {
				entry = e
				break
			}
		}
	}
	if entry == nil {
		return nil, errors.New("资源集缺少选关封面chr60模板")
	}
	return entry, nil
}
