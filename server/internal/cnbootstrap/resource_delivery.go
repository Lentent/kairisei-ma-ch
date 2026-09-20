package cnbootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

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
