package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadScenarioChecks(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plain.xml", "<config/>")
	write("good.xml", "<config/>")
	write("good.checks.json", `{"thresholds": {"rx_first_speech_ms": {"max": 8000}}, "expect": [{"name": "echo", "wav": "prompts/p.wav", "heard_at_ms": {"max": 27000}}]}`)
	write("bad.xml", "<config/>")
	write("bad.checks.json", `{"thresholds": {"echo_ms": {"min": 2}}}`)

	if s, _ := LoadScenario(dir, "plain"); s.Checks != nil || s.ChecksError != "" {
		t.Errorf("plain: want no checks, got %+v %q", s.Checks, s.ChecksError)
	}
	if s, _ := LoadScenario(dir, "good"); s.Checks == nil || len(s.Checks.Expect) != 1 || *s.Checks.Expect[0].HeardAtMs.Max != 27000 {
		t.Errorf("good: got %+v %q", s.Checks, s.ChecksError)
	}
	// A broken file must surface, not silently disable the checks.
	if s, _ := LoadScenario(dir, "bad"); s.Checks != nil || s.ChecksError == "" {
		t.Errorf("bad: want an error, got %+v", s.Checks)
	}
}
