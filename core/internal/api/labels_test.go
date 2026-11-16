package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

func send(t *testing.T, srv *httptest.Server, method, path, ct, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m
}

// TestDeviceLabelsAndVendor covers label CRUD + validation, and that client
// rows carry the label plus address-derived vendor/hint/private fields, and
// that global search finds a client by its label.
func TestDeviceLabelsAndVendor(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	ctx := context.Background()
	const ap = "64:13:AB:A5:53:E8"
	const apple, private = "8C:98:6B:26:DD:59", "32:B4:EC:04:82:C5"
	now := time.Now().UnixMilli()
	err := st.WithTx(ctx, func(tx *storage.Tx) error {
		if err := tx.UpsertAP(ctx, storage.APUpdate{BSSID: ap, Seen: now, Sensor: "nano-1"}); err != nil {
			return err
		}
		for _, c := range []string{apple, private} {
			if err := tx.UpsertClient(ctx, storage.ClientUpdate{BSSID: ap, Client: c, Seen: now, Frames: 1, Sensor: "nano-1"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	const js = "application/json"
	for _, c := range []struct {
		name, method, path, ct, body string
		code                         int
	}{
		{"bad address", "PUT", "/api/v1/device-labels/nope", js, `{"label":"x"}`, 400},
		{"wrong content type", "PUT", "/api/v1/device-labels/" + private, "text/plain", `{"label":"x"}`, 415},
		{"empty label", "PUT", "/api/v1/device-labels/" + private, js, `{"label":"   "}`, 400},
		{"too long", "PUT", "/api/v1/device-labels/" + private, js, `{"label":"` + strings.Repeat("é", 49) + `"}`, 400},
		{"control char", "PUT", "/api/v1/device-labels/" + private, js, `{"label":"a\u0007b"}`, 400},
		{"unknown field", "PUT", "/api/v1/device-labels/" + private, js, `{"label":"x","y":1}`, 400},
		{"delete missing", "DELETE", "/api/v1/device-labels/" + private, "", "", 404},
		{"set (lower-case path, trimmed)", "PUT", "/api/v1/device-labels/32:b4:ec:04:82:c5", js, `{"label":"  Beau's iPhone "}`, 200},
		{"max length ok", "PUT", "/api/v1/device-labels/" + apple, js, `{"label":"` + strings.Repeat("é", 48) + `"}`, 200},
		{"replace", "PUT", "/api/v1/device-labels/" + apple, js, `{"label":"Living-room Mac"}`, 200},
	} {
		if code, body := send(t, srv, c.method, c.path, c.ct, c.body); code != c.code {
			t.Fatalf("%s: got %d %v, want %d", c.name, code, body, c.code)
		}
	}

	code, body := get(t, srv, "/api/v1/device-labels")
	if ls := body["data"].([]any); code != 200 || len(ls) != 2 ||
		ls[0].(map[string]any)["address"] != private || ls[0].(map[string]any)["label"] != "Beau's iPhone" ||
		ls[1].(map[string]any)["label"] != "Living-room Mac" {
		t.Fatalf("labels: %d %v", code, body)
	}

	_, body = get(t, srv, "/api/v1/clients?limit=10")
	rows := map[string]map[string]any{}
	for _, r := range body["data"].([]any) {
		rows[r.(map[string]any)["client"].(string)] = r.(map[string]any)
	}
	if a := rows[apple]; a["vendor"] != "Apple" || a["vendor_full"] != "Apple, Inc." || a["private"] != false ||
		a["label"] != "Living-room Mac" || !strings.Contains(a["device_hint"].(string), "iPad") {
		t.Fatalf("apple row: %v", a)
	}
	if p := rows[private]; p["vendor"] != "" || p["private"] != true || p["label"] != "Beau's iPhone" {
		t.Fatalf("private row: %v", p)
	}
	// Per-AP list carries the same fields.
	_, body = get(t, srv, "/api/v1/access-points/"+ap+"/clients")
	if n := len(body["data"].([]any)); n != 2 {
		t.Fatalf("ap clients: %v", body)
	}

	// Search by label text finds the client and titles it with the label.
	code, body = get(t, srv, "/api/v1/search?q=iphone")
	hits := body["data"].([]any)
	if code != 200 || len(hits) != 1 || hits[0].(map[string]any)["title"] != "Beau's iPhone" ||
		hits[0].(map[string]any)["id"] != private {
		t.Fatalf("search by label: %d %v", code, body)
	}
	_, body = get(t, srv, "/api/v1/search?q=8C986B")
	if h := body["data"].([]any); len(h) != 1 || !strings.Contains(h[0].(map[string]any)["detail"].(string), "Apple") {
		t.Fatalf("search by address: %v", body)
	}

	// Delete, then the row has no label.
	if code, _ = send(t, srv, "DELETE", "/api/v1/device-labels/"+private, "", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	_, body = get(t, srv, "/api/v1/clients?limit=10")
	for _, r := range body["data"].([]any) {
		if m := r.(map[string]any); m["client"] == private && m["label"] != "" {
			t.Fatalf("label survived delete: %v", m)
		}
	}
}
