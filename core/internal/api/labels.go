package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/go-chi/chi/v5"
)

// maxLabelRunes bounds an operator device label.
const maxLabelRunes = 48

func (h *handlers) labelRoutes(r chi.Router) {
	r.Get("/device-labels", h.listLabels)
	r.Put("/device-labels/{address}", h.putLabel)
	r.Delete("/device-labels/{address}", h.deleteLabel)
}

func (h *handlers) listLabels(w http.ResponseWriter, r *http.Request) {
	ls, err := h.d.Store.ListDeviceLabels(r.Context())
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": ls})
}

// labelAddress reads and validates the {address} path parameter.
func labelAddress(w http.ResponseWriter, r *http.Request) (string, bool) {
	a := strings.ToUpper(pathParam(r, "address"))
	if !reMAC.MatchString(a) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "address must be AA:BB:CC:DD:EE:FF")
		return "", false
	}
	return a, true
}

// cleanLabel trims and validates a label: 1..maxLabelRunes runes of valid
// UTF-8 with no control characters.
func cleanLabel(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", errors.New("label must not be empty (DELETE removes a label)")
	case !utf8.ValidString(s):
		return "", errors.New("label must be valid UTF-8")
	case utf8.RuneCountInString(s) > maxLabelRunes:
		return "", errors.New("label is too long (max 48 characters)")
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		return "", errors.New("label must not contain control characters")
	}
	return s, nil
}

func (h *handlers) putLabel(w http.ResponseWriter, r *http.Request) {
	addr, ok := labelAddress(w, r)
	if !ok {
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", `expected {"label":"..."}`)
		return
	}
	label, err := cleanLabel(body.Label)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	l := storage.DeviceLabel{Address: addr, Label: label, UpdatedAt: time.Now().UnixMilli()}
	if err := h.d.Store.SetDeviceLabel(r.Context(), l); err != nil {
		internal(w, h.d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": l})
}

func (h *handlers) deleteLabel(w http.ResponseWriter, r *http.Request) {
	addr, ok := labelAddress(w, r)
	if !ok {
		return
	}
	switch err := h.d.Store.DeleteDeviceLabel(r.Context(), addr); {
	case errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "no label for that address")
	case err != nil:
		internal(w, h.d.Log, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
