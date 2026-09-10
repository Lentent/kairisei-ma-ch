package cnbootstrap

import (
	"errors"
	"fmt"
	"os"
)

func (storage *cnSaveDatabase) ensureCurrentDatabase() error {
	if info, err := os.Stat(storage.databasePath); err == nil {
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return errors.New("CN state database must be a non-empty regular file")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat CN state database: %w", err)
	}

	legacyInfo, err := os.Stat(storage.legacyDatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat legacy CN save database: %w", err)
	}
	if !legacyInfo.Mode().IsRegular() || legacyInfo.Size() <= 0 {
		return errors.New("legacy CN save database must be a non-empty regular file")
	}
	return errors.New("legacy CN database is unsupported; use a fresh data directory")
}
