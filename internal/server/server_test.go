package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"kachel.solar/internal/battery"
	"kachel.solar/internal/stats"
)

type stubBattery struct {
	rd battery.Reading
	ok bool
}

func (s stubBattery) Current() (battery.Reading, bool) { return s.rd, s.ok }

type stubStats struct {
	rep   stats.Report
	err   error
	calls int32
}

func (s *stubStats) Stats() (stats.Report, error) {
	atomic.AddInt32(&s.calls, 1)
	return s.rep, s.err
}

func newTestServer(t *testing.T, webDir string, bat BatterySource, st StatsSource, ttl time.Duration) http.Handler {
	t.Helper()
	return New(Options{WebDir: webDir, Battery: bat, Stats: st, StatsTTL: ttl, Version: "test"})
}

func TestBatteryEndpoint(t *testing.T) {
	st := &stubStats{}

	// No reading yet -> 503.
	h := newTestServer(t, "", stubBattery{ok: false}, st, time.Minute)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/battery", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no reading: status = %d, want 503", rec.Code)
	}

	// Reading present -> 200 with the payload and no-store.
	rd := battery.Reading{Percent: 72.5, Charging: true, ChargeState: battery.StateCharging, Voltage: 3.95}
	h = newTestServer(t, "", stubBattery{rd: rd, ok: true}, st, time.Minute)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/battery", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var got battery.Reading
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Percent != 72.5 || !got.Charging {
		t.Errorf("payload mismatch: %+v", got)
	}
}

func TestSensorEndpointCaches(t *testing.T) {
	st := &stubStats{rep: stats.Report{Day: stats.Window{EnergyGeneratedWh: 5}}}
	h := newTestServer(t, "", stubBattery{ok: true}, st, time.Minute)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sensor", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d", i, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
			t.Errorf("Cache-Control = %q", cc)
		}
	}
	if n := atomic.LoadInt32(&st.calls); n != 1 {
		t.Errorf("Stats called %d times, want 1 (cached)", n)
	}
}

func TestSensorEndpointError(t *testing.T) {
	st := &stubStats{err: os.ErrPermission}
	h := newTestServer(t, "", stubBattery{ok: true}, st, time.Minute)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sensor", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	h := newTestServer(t, "", stubBattery{ok: true}, &stubStats{}, time.Minute)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ok" || body["version"] != "test" {
		t.Errorf("health body = %v", body)
	}
}

func TestStaticCleanURLsAnd404(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<h1>home</h1>")
	write("writeup.html", "<h1>writeup</h1>")
	write("404.html", "<h1>battery empty</h1>")

	h := newTestServer(t, dir, stubBattery{ok: true}, &stubStats{}, time.Minute)

	cases := []struct {
		path       string
		wantStatus int
		wantBody   string
		wantCache  string
	}{
		{"/", 200, "<h1>home</h1>", "no-cache"},
		{"/writeup", 200, "<h1>writeup</h1>", "no-cache"},
		{"/writeup.html", 200, "<h1>writeup</h1>", "no-cache"},
		{"/nope", 404, "<h1>battery empty</h1>", ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.wantStatus {
			t.Errorf("%s: status = %d, want %d", c.path, rec.Code, c.wantStatus)
		}
		if got := rec.Body.String(); got != c.wantBody {
			t.Errorf("%s: body = %q, want %q", c.path, got, c.wantBody)
		}
		if c.wantCache != "" {
			if cc := rec.Header().Get("Cache-Control"); cc != c.wantCache {
				t.Errorf("%s: Cache-Control = %q, want %q", c.path, cc, c.wantCache)
			}
		}
	}
}

func TestStaticAssetCacheHeader(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "style.css"), []byte("body{}"), 0o644)
	h := newTestServer(t, dir, stubBattery{ok: true}, &stubStats{}, time.Minute)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/style.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestStaticNoWebDirServesPlaceholder(t *testing.T) {
	h := newTestServer(t, "", stubBattery{ok: true}, &stubStats{}, time.Minute)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("placeholder: status = %d len = %d", rec.Code, rec.Body.Len())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing path without web dir: status = %d, want 404", rec.Code)
	}
}

func TestStaticRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644)
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	os.WriteFile(secret, []byte("top secret"), 0o644)
	defer os.Remove(secret)

	// Hit the static handler directly so the ServeMux's own path-cleaning
	// doesn't mask whether resolve() contains the traversal.
	h := newStaticHandler(dir)

	for _, p := range []string{"/../secret.txt", "/../../secret.txt", "/..%2fsecret.txt"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
		req.URL.Path = p
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && rec.Body.String() == "top secret" {
			t.Fatalf("path traversal succeeded for %q", p)
		}
	}
}
