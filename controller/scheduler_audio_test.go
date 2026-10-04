package controller

import (
	"strings"
	"testing"

	"github.com/jchavanton/ace/audio"
	"github.com/jchavanton/ace/config"
	"github.com/jchavanton/ace/models"
)

// A run whose SIP side passed but whose audio checks failed must fail the bot.
func TestAudioFailureFailsBotRun(t *testing.T) {
	dir := t.TempDir()
	if err := models.SaveBot(dir, &models.Bot{Name: "probe", Scenario: "agent_echo", IntervalMinutes: 5, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s := &Scheduler{Cfg: &config.Config{BotsDir: dir}}
	val, max := 21000.0, 15000.0
	s.onRunFinish(&models.Run{
		ID: "r1", Scenario: "agent_echo", TriggeredBy: "bot:probe", Status: "done",
		Aggregate: models.Aggregate{Total: 1, Pass: 1},
		Audio:     &audio.Verdict{Checks: []audio.Check{{Name: "transfer_ms", Value: &val, Max: &max}}},
	})

	b, err := models.LoadBot(dir, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if b.LastStatus != "fail" {
		t.Errorf("bot LastStatus %q, want fail", b.LastStatus)
	}
	alerts, _ := models.ListAlerts(dir)
	if len(alerts) != 1 || !strings.Contains(alerts[0].Reason, "audio: transfer_ms=21000 (want <= 15000)") {
		t.Errorf("alerts %+v", alerts)
	}
}
