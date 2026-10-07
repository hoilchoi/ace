package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/models"
)

// JSON counterparts of the Run button and the run page, for scripts such
// as a deploy pipeline: start a run, poll it, gate on "passed".

// apiRun is GET /api/runs/:id. Passed is null while the run is still
// going, so a poller can loop until it is set.
type apiRun struct {
	Passed *bool       `json:"passed"`
	Run    *models.Run `json:"run"`
}

func (s *Server) handleAPIRun(c *gin.Context) {
	run, status, err := s.startScenarioRun(c)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Header("Location", "/api/runs/"+run.ID)
	c.JSON(http.StatusAccepted, apiRun{Run: run})
}

func (s *Server) handleAPIRunStatus(c *gin.Context) {
	run, err := models.LoadRun(s.Cfg.RunsDir, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, apiRun{Passed: runPassed(run), Run: run})
}

// runPassed is nil while the run is running; otherwise true only for a
// finished run whose every call PASSed, after any scenario verdict.
func runPassed(run *models.Run) *bool {
	if run.Status == "running" {
		return nil
	}
	passed := run.Status == "done" && len(run.Calls) > 0
	for _, call := range run.Calls {
		if !strings.EqualFold(call.Result, "PASS") {
			passed = false
		}
	}
	return &passed
}
