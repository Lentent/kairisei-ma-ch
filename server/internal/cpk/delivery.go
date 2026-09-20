package cpk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Logical names retain client spelling; SourcePath is the selected physical
// owner. Case folding is for collision detection/lookup, not for renaming files.
type File struct {
	Name       string
	SourcePath string
	Size       int64
	Version    uint64
}

func Load(cpkRoot string, aliases []Alias) ([]File, error) {
	if cpkRoot == "" {
		return nil, errors.New("official CN CPK root is required")
	}
	root, err := filepath.Abs(cpkRoot)
	if err != nil {
		return nil, err
	}
	versions, err := loadVersions(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var delivery []File
	seen := make(map[string]bool)
	add := func(name, source string) error {
		key := strings.ToLower(name)
		if !ValidName(name) || seen[key] {
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
		delivery = append(delivery, File{name, source, info.Size(), versions.version(name)})
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
		delivery[len(delivery)-1].Version = max(versions.version(alias.AliasName), versions.version(alias.SourceName))
	}
	if len(delivery) == 0 {
		return nil, errors.New("official CN CPK root contains no .cpk files")
	}
	return delivery, nil
}
