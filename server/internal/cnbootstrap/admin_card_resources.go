package cnbootstrap

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"kairisei.local/server/internal/clientcsv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type cnAdminCardResource struct {
	Name     string
	PictID   int
	Ready    bool
	Excluded bool
}

// LOCAL_POLICY: only exclude cards with no recoverable source in the current
// audited inputs. A subsequently closed resource pair restores selection.
//
//go:embed admin_card_exclusions.json
var cnAdminCardExclusionsJSON []byte

// Catalog visibility and gameplay resource closure are separate from the
// loopback-only thumbnail manifest. Recovered cards may have no web thumbnail.
func loadCNAdminCardResources(master cnCardRuntimeMaster, masterPath, assetMapPath string) (map[int]cnAdminCardResource, error) {
	var exclusions map[int]string
	if err := json.Unmarshal(cnAdminCardExclusionsJSON, &exclusions); err != nil {
		return nil, fmt.Errorf("read admin card exclusions: %w", err)
	}
	var provenance struct {
		Files []struct {
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(master.Source, &provenance); err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(masterPath), "cn602-card-master", "card.csv"))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	verified := false
	for _, source := range provenance.Files {
		if filepath.Base(filepath.FromSlash(source.Path)) == "card.csv" && source.Bytes == len(content) && source.SHA256 == hex.EncodeToString(digest[:]) {
			verified = true
		}
	}
	if !verified {
		return nil, errors.New("admin card source differs from the runtime master receipt")
	}
	file, err := os.Open(assetMapPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var assets struct {
		CatalogAssets []struct {
			Name string `json:"name"`
		} `json:"catalog_assets"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 128*1024*1024)).Decode(&assets); err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(assets.CatalogAssets))
	for _, asset := range assets.CatalogAssets {
		names[strings.ToLower(asset.Name)] = true
	}
	lines := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(content), "\ufeff")))
	lines.Buffer(make([]byte, 4096), 4*1024*1024)
	if !lines.Scan() {
		return nil, errors.New("admin card source has no header")
	}
	header := clientcsv.SplitLine(lines.Text())
	if len(header) <= 36 || header[36] != "PictID" {
		return nil, errors.New("admin card PictID column differs")
	}
	resources := make(map[int]cnAdminCardResource)
	// Use the same physical-line dialect as CardDataContainer / battle master.
	for lines.Scan() {
		row := clientcsv.SplitLine(lines.Text())
		if len(row) <= 36 {
			continue
		}
		id, err := strconv.Atoi(row[0])
		if err != nil {
			continue
		}
		pict, err := strconv.Atoi(row[36])
		if err != nil {
			return nil, fmt.Errorf("card %d has invalid PictID", id)
		}
		if _, duplicate := resources[id]; duplicate {
			return nil, fmt.Errorf("duplicate card identity %d", id)
		}
		ready := pict >= 10000000 && names[fmt.Sprintf("chr10_%08d", pict)] && names[fmt.Sprintf("chr20_%08d", pict)]
		resources[id] = cnAdminCardResource{Name: strings.TrimSpace(row[5]), PictID: pict, Ready: ready, Excluded: !ready && exclusions[id] != ""}
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}
	for _, card := range master.CardTemplates {
		if _, ok := resources[card.CardID]; !ok {
			return nil, fmt.Errorf("card %d has no presentation identity", card.CardID)
		}
	}
	return resources, nil
}
