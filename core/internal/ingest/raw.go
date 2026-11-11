package ingest

import "encoding/json"

// rawField returns the raw JSON of one metadata member, or nil.
func rawField(meta json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(meta, &m) != nil {
		return nil
	}
	return m[key]
}
