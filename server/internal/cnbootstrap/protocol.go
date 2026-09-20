package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func writeCNProtocolResponse(writer http.ResponseWriter, common map[string]any, payload map[string]any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	commonJSON, err := json.Marshal(common)
	if err != nil {
		http.Error(writer, "encode common response", http.StatusInternalServerError)
		return
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		http.Error(writer, "encode protocol response", http.StatusInternalServerError)
		return
	}
	_, _ = writer.Write(append(append(commonJSON, '\n'), append(payloadJSON, '\n')...))
}

func writeCNProtocolResponseWithPopup(writer http.ResponseWriter, common map[string]any, payload map[string]any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	segments := make([][]byte, 0, 3)
	for _, value := range []any{
		common,
		payload,
		map[string]any{"popup": []any{}},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			http.Error(writer, "encode CN protocol response", http.StatusInternalServerError)
			return
		}
		segments = append(segments, encoded)
	}
	_, _ = writer.Write(bytes.Join(segments, []byte{'\n'}))
}
