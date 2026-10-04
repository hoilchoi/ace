package audio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

// echoRun lays out a run dir the way voip_patrol does: the call recorded as
// record_<Call-ID>_<contact>_rec.wav, with our prompt echoed back from 6 s, and the
// prompt under the scenarios dir.
func echoRun(t *testing.T) (runDir, scenariosDir string) {
	t.Helper()
	runDir, scenariosDir = t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(scenariosDir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := append([]burst{{1.0, 2.0}}, wordLike(3.0, 12.0, 0)...)
	writeWAV(t, filepath.Join(scenariosDir, "prompts", "p.wav"), 8000, 12.0, prompt)
	rec := append([]burst{{0.0, 0.6}}, shift(prompt, 6.0, 0.15)...)
	writeWAV(t, filepath.Join(runDir, "record_abc-123_1000_rec.wav"), 16000, 12.0, rec)
	return runDir, scenariosDir
}

var call = Call{CallID: "abc-123", RxPackets: 1695, HasRTP: true}

func TestEvaluatePassAndFail(t *testing.T) {
	runDir, scn := echoRun(t)
	cfg := &Config{
		Thresholds: map[string]Threshold{
			"rx_rtp_packets":     {Min: f(1000)},
			"rx_first_speech_ms": {Max: f(8000)},
		},
		Expect: []Expect{{Name: "echo", WAV: "prompts/p.wav", HeardMs: &Threshold{Min: f(3000)}, HeardAtMs: &Threshold{Max: f(9000)}}},
	}
	v := Evaluate(runDir, scn, cfg, call)
	if !v.Passed {
		t.Fatalf("want pass, got %s", v.Summary())
	}
	if len(v.Checks) != 4 {
		t.Errorf("want 4 checks, got %+v", v.Checks)
	}

	cfg.Expect[0].HeardAtMs = &Threshold{Max: f(5000)}
	v = Evaluate(runDir, scn, cfg, call)
	if v.Passed || !strings.HasPrefix(v.Summary(), "expect.echo.heard_at_ms=") || !strings.HasSuffix(v.Summary(), "(want <= 5000)") {
		t.Errorf("want heard_at failure, got passed=%v %q", v.Passed, v.Summary())
	}
}

// Listing expected audio with no rule means it must be heard at all.
func TestExpectWithoutRulesMustBeHeard(t *testing.T) {
	runDir, scn := echoRun(t)
	other := filepath.Join(scn, "prompts", "other.wav")
	writeWAV(t, other, 8000, 6.0, []burst{{0.5, 1.6}, {1.7, 1.8}, {2.4, 3.9}, {4.0, 4.1}, {4.6, 5.4}})

	v := Evaluate(runDir, scn, &Config{Expect: []Expect{{Name: "echo", WAV: "prompts/p.wav"}}}, call)
	if !v.Passed || len(v.Checks) != 1 || v.Checks[0].Name != "expect.echo.heard_ms" {
		t.Errorf("present: passed=%v checks=%+v", v.Passed, v.Checks)
	}
	if v := Evaluate(runDir, scn, &Config{Expect: []Expect{{Name: "other", WAV: other}}}, call); v.Passed {
		t.Errorf("absent audio must fail, got %s", v.Summary())
	}
}

func TestEvaluateErrors(t *testing.T) {
	runDir, scn := echoRun(t)
	if v := Evaluate(t.TempDir(), scn, &Config{}, call); v.Passed || !strings.Contains(v.Error, `record="true"`) {
		t.Errorf("no recording: passed=%v error=%q", v.Passed, v.Error)
	}
	v := Evaluate(runDir, scn, &Config{Expect: []Expect{{Name: "x", WAV: "prompts/nope.wav"}}}, call)
	if v.Passed || !strings.HasPrefix(v.Error, `expect "x":`) {
		t.Errorf("missing wav: passed=%v error=%q", v.Passed, v.Error)
	}
}

func TestValidate(t *testing.T) {
	for name, cfg := range map[string]Config{
		"unknown metric": {Thresholds: map[string]Threshold{"echo_ms": {Min: f(1)}}},
		"bad name":       {Expect: []Expect{{Name: "a b", WAV: "x.wav"}}},
		"duplicate name": {Expect: []Expect{{Name: "a", WAV: "x.wav"}, {Name: "a", WAV: "y.wav"}}},
		"no wav":         {Expect: []Expect{{Name: "a"}}},
	} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	ok := Config{Thresholds: map[string]Threshold{"rx_first_speech_ms": {Max: f(8000)}}, Expect: []Expect{{Name: "echo", WAV: "p.wav"}}}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

// With record_tx a run dir also holds record_<id>_<contact>_tx.wav (what we sent);
// the checks must analyze the inbound _rec.wav, never the _tx.wav.
func TestFindRecordingIgnoresSentAudio(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"record_abc_1000_rec.wav", "record_abc_1000_tx.wav"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"abc", "", "other"} {
		if got := filepath.Base(FindRecording(dir, id)); got != "record_abc_1000_rec.wav" {
			t.Errorf("call id %q: got %q, want the _rec.wav", id, got)
		}
	}
}
