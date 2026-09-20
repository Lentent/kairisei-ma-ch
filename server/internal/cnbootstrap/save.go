package cnbootstrap

import (
	"encoding/json"
	"fmt"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

func encodeCNSaveState(state gamestate.State) ([]byte, error) {
	save := accountstore.SaveFromState(state)
	if err := accountstore.ValidateSave(save); err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(save, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode CN save: %w", err)
	}
	content = append(content, '\n')
	return content, nil
}
