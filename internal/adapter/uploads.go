package adapter

import (
	"encoding/json"
	"os"
	"strings"
)

const serverUploadsField = "__server_uploads"

type serverUpload struct {
	FieldName string `json:"field_name,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	Path      string `json:"path,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

func parseServerUploads(body []byte) []serverUpload {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil
	}
	raw, ok := m[serverUploadsField]
	if !ok || len(raw) == 0 {
		return nil
	}
	var uploads []serverUpload
	if err := json.Unmarshal(raw, &uploads); err != nil {
		return nil
	}
	out := uploads[:0]
	for _, upload := range uploads {
		if strings.TrimSpace(upload.Path) == "" {
			continue
		}
		out = append(out, upload)
	}
	return out
}

func stripServerOnlyFields(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	delete(m, serverUploadsField)
	delete(m, "mode")
	delete(m, "scope")
	delete(m, "channel_id")
	delete(m, "credential_id")
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func readUploadBytes(upload serverUpload) ([]byte, error) {
	return os.ReadFile(upload.Path)
}
