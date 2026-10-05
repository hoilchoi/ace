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

// params values become frame counts; anything under one 10 ms frame (or negative)
// used to hang the run (step 0) or panic the whole process (negative slice index).
func TestValidateRejectsUnsafeParams(t *testing.T) {
	for name, p := range map[string]Params{
		"step under a frame": {MatchStepMs: 5},
		"negative step":      {MatchStepMs: -250},
		"window under frame": {MatchWindowMs: 9},
		"negative window":    {MatchWindowMs: -100},
		"negative rms":       {SpeechRMS: -1},
		"negative segment":   {MinSegmentMs: -1},
		"negative merge gap": {MergeGapMs: -1},
		"corr above 1":       {MatchMinCorr: 1.5},
		"negative corr":      {MatchMinCorr: -0.1},
	} {
		if err := (&Config{Params: p}).Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	ok := Config{Params: Params{MatchStepMs: 10, MatchWindowMs: 1000, MatchMinCorr: 0.9}}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid params rejected: %v", err)
	}
}

// A call that was never answered has no recording; say so, instead of suggesting
// record="true" (which the scenario may well have). voip_patrol's cause code is the
// last status at disconnect, so an answered call can end non-2xx (e.g. 408 BYE timeout).
func TestEvaluateUnansweredCall(t *testing.T) {
	_, scn := echoRun(t)
	const noRecord = `no recording: add record="true" to the call action`
	for name, tc := range map[string]struct {
		call Call
		want string
	}{
		"503, not answered":       {Call{SIPCode: 503, SIPReason: "no available destination"}, "call not answered (SIP 503 no available destination): no audio to check"},
		"no reason":               {Call{SIPCode: 503}, "call not answered (SIP 503): no audio to check"},
		"1xx at disconnect":       {Call{SIPCode: 180, SIPReason: "Ringing"}, "call not answered (SIP 180 Ringing): no audio to check"},
		"answered, ended 408":     {Call{SIPCode: 408, SIPReason: "Request Timeout", Answered: true}, noRecord},
		"answered, normal 200":    {Call{SIPCode: 200, SIPReason: "OK", Answered: true}, noRecord},
		"unknown (no call found)": {Call{}, noRecord},
	} {
		tc.call.CallID = "x"
		if v := Evaluate(t.TempDir(), scn, &Config{}, tc.call); v.Passed || v.Error != tc.want {
			t.Errorf("%s: got passed=%v error=%q, want %q", name, v.Passed, v.Error, tc.want)
		}
	}
}
