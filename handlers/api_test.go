package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/config"
	"github.com/jchavanton/ace/controller"
)

// apiServer runs scenarios with a stub voip_patrol that writes the given
// results.json lines into the run dir, as the real one does.
func apiServer(t *testing.T, results ...string) (*gin.Engine, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		ScenariosDir:   filepath.Join(dir, "scenarios"),
		RunsDir:        filepath.Join(dir, "runs"),
		BotsDir:        filepath.Join(dir, "bots"),
		VoipPatrolBin:  filepath.Join(dir, "voip_patrol"),
		VoipPatrolPort: 5060,
		RTPPortStart:   10000,
		RTPPortEnd:     10100,
	}
	for _, d := range []string{cfg.ScenariosDir, cfg.RunsDir, cfg.BotsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub := "#!/bin/sh\ncat > results.json <<'EOF'\n" + strings.Join(results, "\n") + "\nEOF\n"
	if err := os.WriteFile(cfg.VoipPatrolBin, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ScenariosDir, "probe.xml"), []byte("<config><actions/></config>"), 0o644); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	(&Server{Cfg: cfg, Runner: &controller.Runner{Cfg: cfg}}).Register(r)
	return r, cfg
}

type apiRunBody struct {
	Passed *bool `json:"passed"`
	Run    struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Calls  []struct {
			Result string `json:"result"`
			Reason string `json:"reason"`
		} `json:"calls"`
	} `json:"run"`
	Error string `json:"error"`
}

func call(t *testing.T, r *gin.Engine, method, path string) (int, apiRunBody, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	var body apiRunBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s %s: not JSON: %q", method, path, w.Body.String())
	}
	return w.Code, body, w.Header()
}

// runToCompletion starts the probe scenario and polls until passed is set.
func runToCompletion(t *testing.T, r *gin.Engine) apiRunBody {
	t.Helper()
	code, started, hdr := call(t, r, http.MethodPost, "/api/scenarios/probe/run")
	if code != http.StatusAccepted || started.Run.ID == "" || started.Passed != nil {
		t.Fatalf("start: %d %+v", code, started)
	}
	if loc := hdr.Get("Location"); loc != "/api/runs/"+started.Run.ID {
		t.Errorf("Location = %q", loc)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, got, _ := call(t, r, http.MethodGet, "/api/runs/"+started.Run.ID)
		if got.Passed != nil {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never finished: %+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAPIRunReportsPass(t *testing.T) {
	r, _ := apiServer(t, `{"label":"probe","action":"call","result":"PASS","cause_code":200}`)
	got := runToCompletion(t, r)
	if !*got.Passed || got.Run.Status != "done" {
		t.Fatalf("got %+v, want passed on a done run", got)
	}
}

func TestAPIRunReportsAFailedCallAndItsReason(t *testing.T) {
	r, _ := apiServer(t,
		`{"label":"a","action":"call","result":"PASS","cause_code":200}`,
		`{"label":"b","action":"call","result":"FAIL","cause_code":503,"reason":"no available destination"}`)
	got := runToCompletion(t, r)
	if *got.Passed || len(got.Run.Calls) != 2 || got.Run.Calls[1].Reason != "no available destination" {
		t.Fatalf("got %+v, want not passed with the failing call's reason", got)
	}
}

// The scenario verdict is applied before the result is read, so a call
// voip_patrol PASSed but the verdict failed reads as not passed.
func TestAPIRunAppliesTheScenarioVerdict(t *testing.T) {
	r, cfg := apiServer(t, `{"label":"probe","action":"call","result":"PASS","cause_code":200,"rtp_stats":[{"Rx":{"voice_frames":2},"Tx":{"voice_frames":50}}]}`)
	if err := os.WriteFile(filepath.Join(cfg.ScenariosDir, "probe.verdict.json"), []byte(`{"min_rx_voice_ms":3000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runToCompletion(t, r)
	if *got.Passed || !strings.Contains(got.Run.Calls[0].Reason, "rx voice=200ms") {
		t.Fatalf("got %+v, want not passed with the verdict's reason", got)
	}
}

func TestAPIRunWithNoCallsIsNotAPass(t *testing.T) {
	r, _ := apiServer(t)
	if got := runToCompletion(t, r); *got.Passed {
		t.Fatalf("got %+v; a run that produced no calls proves nothing", got)
	}
}

func TestAPIUnknownScenarioAndRunAreJSON404s(t *testing.T) {
	r, _ := apiServer(t)
	for _, req := range [][2]string{{http.MethodPost, "/api/scenarios/nope/run"}, {http.MethodGet, "/api/runs/nope"}} {
		if code, body, _ := call(t, r, req[0], req[1]); code != http.StatusNotFound || body.Error == "" {
			t.Errorf("%s %s: %d %+v, want 404 with an error", req[0], req[1], code, body)
		}
	}
}

func TestTheRunFormStillRedirectsToTheRunPage(t *testing.T) {
	r, _ := apiServer(t, `{"label":"probe","action":"call","result":"PASS"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/scenarios/probe/run", nil))
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/runs/") {
		t.Fatalf("got %d to %q, want a redirect to the run page", w.Code, w.Header().Get("Location"))
	}
}
