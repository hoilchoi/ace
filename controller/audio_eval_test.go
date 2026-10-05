package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jchavanton/ace/models"
)

// evaluateAudio passes voip_patrol's SIP result through, so an unanswered call is
// reported as such rather than as a missing record="true".
func TestEvaluateAudioReportsUnansweredCall(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.xml"), []byte("<config/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.checks.json"), []byte(`{"thresholds": {"rx_first_speech_ms": {"max": 8000}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	scn, err := models.LoadScenario(dir, "probe")
	if err != nil {
		t.Fatal(err)
	}
	unanswered := []models.CallResult{{Action: "call", CallID: "abc", Result: "FAIL", CauseCode: 503, Reason: "no available destination"}}
	if v := evaluateAudio(t.TempDir(), scn, unanswered); v == nil || v.Error != "call not answered (SIP 503 no available destination): no audio to check" {
		t.Errorf("unanswered: got %+v", v)
	}
	// Answered (200 received) but ended 408: the missing recording really is a missing record="true".
	answered := []models.CallResult{{Action: "call", CallID: "abc", Result: "FAIL", CauseCode: 408, Reason: "Request Timeout",
		Duration: 12, SIPLatency: models.SIPLatency{Invite200Ms: 900}}}
	if v := evaluateAudio(t.TempDir(), scn, answered); v == nil || v.Error != `no recording: add record="true" to the call action` {
		t.Errorf("answered: got %+v", v)
	}
}
