package handlers

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/models"
)

const maxPromptBytes = 20 << 20 // a minute of 16 kHz mono is ~2 MB

func (s *Server) handlePrompts(c *gin.Context) {
	prompts, err := models.ListPrompts(s.Cfg.ScenariosDir)
	if err != nil {
		c.String(http.StatusInternalServerError, "list prompts: %v", err)
		return
	}
	s.render(c, http.StatusOK, gin.H{
		"Title":           "Prompts",
		"Page":            "prompts",
		"ContentTemplate": "content_prompts",
		"Prompts":         prompts,
		"PromptsDir":      models.PromptsDir(s.Cfg.ScenariosDir),
	})
}

func (s *Server) handlePromptFile(c *gin.Context) {
	name := models.SanitizePromptName(c.Param("file"))
	if name == "" {
		c.String(http.StatusBadRequest, "invalid name")
		return
	}
	c.File(filepath.Join(models.PromptsDir(s.Cfg.ScenariosDir), name))
}

// handlePromptUpload stores an uploaded WAV under <scenarios>/prompts/. The file is
// checked before it replaces anything: a scenario may already be playing that name.
func (s *Server) handlePromptUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPromptBytes+1<<20)
	fh, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, "file required: %v", err)
		return
	}
	if fh.Size > maxPromptBytes {
		c.String(http.StatusRequestEntityTooLarge, "prompt larger than %d MB", maxPromptBytes>>20)
		return
	}
	name := c.PostForm("name")
	if name == "" {
		name = filepath.Base(fh.Filename)
	}
	if name = strings.TrimSpace(name); filepath.Ext(name) == "" {
		name += ".wav"
	}
	if name = models.SanitizePromptName(name); name == "" {
		c.String(http.StatusBadRequest, "name must be a .wav (added if you leave the extension off) using only letters, digits, '_', '-' and '.'")
		return
	}
	dir := models.PromptsDir(s.Cfg.ScenariosDir)
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err == nil && c.PostForm("replace") == "" {
		c.String(http.StatusConflict, "prompt %q already exists; tick \"replace\" to overwrite it", name)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.String(http.StatusInternalServerError, "create prompts dir: %v", err)
		return
	}

	src, err := fh.Open()
	if err != nil {
		c.String(http.StatusBadRequest, "read upload: %v", err)
		return
	}
	defer src.Close()
	tmp, err := os.CreateTemp(dir, ".upload-*.wav")
	if err != nil {
		c.String(http.StatusInternalServerError, "temp file: %v", err)
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		c.String(http.StatusInternalServerError, "write: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		c.String(http.StatusInternalServerError, "write: %v", err)
		return
	}
	if _, _, err := models.ProbePrompt(tmp.Name()); err != nil {
		c.String(http.StatusBadRequest, "not a usable prompt (need a 16-bit PCM WAV): %v", err)
		return
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		c.String(http.StatusInternalServerError, "chmod: %v", err)
		return
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		c.String(http.StatusInternalServerError, "save: %v", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/prompts")
}

func (s *Server) handlePromptDelete(c *gin.Context) {
	name := models.SanitizePromptName(c.Param("file"))
	if name == "" {
		c.String(http.StatusBadRequest, "invalid name")
		return
	}
	if err := os.Remove(filepath.Join(models.PromptsDir(s.Cfg.ScenariosDir), name)); err != nil && !os.IsNotExist(err) {
		c.String(http.StatusInternalServerError, "delete: %v", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/prompts")
}
