package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/config"
)

func testServer(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	r := gin.New()
	(&Server{Cfg: &config.Config{ScenariosDir: dir}}).Register(r)
	return r, dir
}

func postForm(r *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestScenarioChecksSave(t *testing.T) {
	r, dir := testServer(t)
	if err := os.WriteFile(filepath.Join(dir, "probe.xml"), []byte("<config/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := filepath.Join(dir, "probe.checks.json")

	w := postForm(r, "/scenarios/probe/checks", url.Values{"checks": {`{"thresholds": {"rx_speech_ms": {"min": 3000}}}`}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("valid: status %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(checks); !strings.Contains(string(b), `"rx_speech_ms"`) {
		t.Fatalf("valid: file %q", b)
	}
	if fi, _ := os.Stat(checks); fi.Mode().Perm() != 0o644 {
		t.Errorf("checks file mode %v, want 0644 like the scenario XML", fi.Mode().Perm())
	}

	for name, body := range map[string]string{
		"unknown metric":  `{"thresholds": {"echo_ms": {"min": 2}}}`,
		"typo'd key":      `{"threshold": {"rx_speech_ms": {"min": 3000}}}`,
		"bad expect":      `{"expect": [{"name": "greeting"}]}`,
		"step under 10ms": `{"params": {"match_step_ms": 5}}`,
		"negative window": `{"params": {"match_window_ms": -100}}`,
		"not json":        `{thresholds`,
	} {
		if w := postForm(r, "/scenarios/probe/checks", url.Values{"checks": {body}}); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, w.Code)
		}
	}
	if b, _ := os.ReadFile(checks); !strings.Contains(string(b), `"rx_speech_ms"`) {
		t.Errorf("rejected saves must leave the file alone, got %q", b)
	}

	if w := postForm(r, "/scenarios/probe/checks", url.Values{"checks": {"  "}}); w.Code != http.StatusSeeOther {
		t.Fatalf("empty: status %d", w.Code)
	}
	if _, err := os.Stat(checks); !os.IsNotExist(err) {
		t.Errorf("empty submit should remove the file, stat err=%v", err)
	}

	if w := postForm(r, "/scenarios/nope/checks", url.Values{"checks": {"{}"}}); w.Code != http.StatusNotFound {
		t.Errorf("unknown scenario: status %d, want 404", w.Code)
	}
}
