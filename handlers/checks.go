package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/models"
)

// checksExample is the editor's placeholder.
const checksExample = `{
  "thresholds": {
    "rx_rtp_packets":     {"min": 500},
    "rx_first_speech_ms": {"max": 5000}
  },
  "expect": [
    {"name": "greeting", "wav": "prompts/greeting.wav", "heard_at_ms": {"max": 10000}}
  ]
}`

// handleScenarioChecksSave writes the scenario's .checks.json, or removes it when the
// submitted text is empty.
func (s *Server) handleScenarioChecksSave(c *gin.Context) {
	name := sanitizeScenarioName(c.Param("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "invalid name")
		return
	}
	if _, err := os.Stat(filepath.Join(s.Cfg.ScenariosDir, name+".xml")); err != nil {
		c.String(http.StatusNotFound, "scenario %q does not exist", name)
		return
	}
	body := strings.TrimSpace(c.PostForm("checks"))
	if body == "" {
		if err := models.DeleteScenarioChecks(s.Cfg.ScenariosDir, name); err != nil {
			c.String(http.StatusInternalServerError, "delete checks: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/scenarios/"+name)
		return
	}
	if _, err := models.ParseScenarioChecks([]byte(body)); err != nil {
		c.String(http.StatusBadRequest, "audio checks: %v", err)
		return
	}
	if err := models.SaveScenarioChecks(s.Cfg.ScenariosDir, name, body); err != nil {
		c.String(http.StatusInternalServerError, "save checks: %v", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/scenarios/"+name)
}
