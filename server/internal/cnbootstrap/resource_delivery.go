package cnbootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Logical names retain client spelling; SourcePath is the selected physical
// owner. Case folding is for collision detection/lookup, not for renaming files.
type cnCPKDelivery struct {
	Name       string
	SourcePath string
	Size       int64
	Version    uint64
}

func loadCNCPKDelivery(cpkRoot string, aliases []resolvedCPKAlias) ([]cnCPKDelivery, error) {
	if cpkRoot == "" {
		return nil, errors.New("official CN CPK root is required")
	}
	root, err := filepath.Abs(cpkRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var delivery []cnCPKDelivery
	seen := make(map[string]bool)
	add := func(name, source string) error {
		key := strings.ToLower(name)
		if !safeCPKFileName(name) || seen[key] {
			return fmt.Errorf("invalid or duplicate CPK delivery name %q", name)
		}
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return fmt.Errorf("CPK source is not a non-empty regular file: %s", source)
		}
		seen[key] = true
		delivery = append(delivery, cnCPKDelivery{name, source, info.Size(), cnCPKVersion(name)})
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".cpk") {
			if err := add(entry.Name(), filepath.Join(root, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	for _, alias := range aliases {
		if !seen[strings.ToLower(alias.SourceName)] {
			return nil, fmt.Errorf("CPK alias must point directly to a physical file: %s", alias.AliasName)
		}
		if err := add(alias.AliasName, alias.SourcePath); err != nil {
			return nil, err
		}
		// An alias shares the physical owner's bytes. A source repair must also
		// invalidate clients that downloaded those bytes under the logical name.
		delivery[len(delivery)-1].Version = max(cnCPKVersion(alias.AliasName), cnCPKVersion(alias.SourceName))
	}
	if len(delivery) == 0 {
		return nil, errors.New("official CN CPK root contains no .cpk files")
	}
	return delivery, nil
}

type cnPatchDelivery struct {
	Name string
	CRC  string
	Size int64
}

// Decode the generated wire catalog so HTTP version checks and CDN exports
// cannot drift from the overlay CRC/source selection that the client receives.
func cnPatchDeliveryFromCatalog(catalog []byte) ([]cnPatchDelivery, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(catalog))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	reader := csv.NewReader(compressed)
	reader.FieldsPerRecord = -1
	var delivery []cnPatchDelivery
	seen := make(map[string]bool)
	for {
		row, err := reader.Read()
		if err == io.EOF {
			return delivery, nil
		}
		if err != nil {
			return nil, err
		}
		if len(row) < 5 || row[0] != "<b>" {
			continue
		}
		size, err := strconv.ParseInt(row[4], 10, 64)
		if err != nil || size <= 0 || seen[strings.ToLower(row[1])] {
			return nil, fmt.Errorf("invalid or duplicate patch delivery: %q", row[1])
		}
		seen[strings.ToLower(row[1])] = true
		delivery = append(delivery, cnPatchDelivery{row[1], row[2], size})
	}
}
