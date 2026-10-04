package handlers

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/audio"
	"github.com/jchavanton/ace/config"
	"github.com/jchavanton/ace/controller"
	"github.com/jchavanton/ace/models"
)

// The run page shows each expected audio item's heard / heard-at / offset line.
func TestRunPageShowsExpectedAudio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunsDir: t.TempDir(), BotsDir: t.TempDir(), ScenariosDir: t.TempDir()}
	r := gin.New()
	r.LoadHTMLGlob("../templates/*.html")
	(&Server{Cfg: cfg, Runner: &controller.Runner{Cfg: cfg}}).Register(r)

	at, off := 17440, 70
	run := &models.Run{ID: "r1", Scenario: "probe", Status: "done", Audio: &audio.Verdict{
		Passed: true, Checks: []audio.Check{}, Metrics: map[string]*float64{},
		Report: &audio.Report{Expect: []audio.Heard{
			{Name: "echo", HeardMs: 15500, HeardAtMs: &at, OffsetMs: &off},
			{Name: "greeting", HeardMs: 0},
		}},
	}}
	if err := os.MkdirAll(filepath.Join(cfg.RunsDir, "r1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run.Save(cfg.RunsDir); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runs/r1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	for _, want := range []string{
		"heard 15.5 s of it · first heard at 17.4 s · offset +70 ms",
		"not heard",
	} {
		if !strings.Contains(html.UnescapeString(w.Body.String()), want) {
			t.Errorf("run page missing %q", want)
		}
	}
}
