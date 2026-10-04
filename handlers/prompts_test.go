package handlers

import (
	"bytes"
	"encoding/binary"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jchavanton/ace/config"
	"github.com/jchavanton/ace/controller"
)

// pcm16WAV is a valid mono 8 kHz WAV of the given length in ms.
func pcm16WAV(ms int) []byte {
	data := make([]byte, 8*ms*2)
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

func upload(r *gin.Engine, filename string, body []byte, fields map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/prompts", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPromptUpload(t *testing.T) {
	r, dir := testServer(t)
	saved := filepath.Join(dir, "prompts", "ask.wav")

	if w := upload(r, "ask.wav", pcm16WAV(500), nil); w.Code != http.StatusSeeOther {
		t.Fatalf("valid upload: status %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("not saved: %v", err)
	}

	if w := upload(r, "ask.wav", pcm16WAV(900), nil); w.Code != http.StatusConflict {
		t.Errorf("same name without replace: status %d, want 409", w.Code)
	}
	if w := upload(r, "ask.wav", pcm16WAV(900), map[string]string{"replace": "1"}); w.Code != http.StatusSeeOther {
		t.Errorf("replace: status %d", w.Code)
	}
	if fi, _ := os.Stat(saved); fi.Size() != int64(len(pcm16WAV(900))) {
		t.Errorf("replace didn't overwrite")
	}

	for name, tc := range map[string]struct {
		filename string
		body     []byte
		fields   map[string]string
	}{
		"not a wav":     {"notes.wav", []byte("hello"), nil},
		"empty wav":     {"empty.wav", pcm16WAV(0), nil},
		"bad extension": {"ask.mp3", pcm16WAV(500), nil},
		"path in name":  {"x.wav", pcm16WAV(500), map[string]string{"name": "../escape.wav"}},
		"dotfile":       {".hidden.wav", pcm16WAV(500), nil},
	} {
		if w := upload(r, tc.filename, tc.body, tc.fields); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.wav")); err == nil {
		t.Errorf("upload escaped the prompts dir")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "prompts", ".upload-*")); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}

	// A name without an extension gets .wav; a different extension is still refused.
	if w := upload(r, "x.wav", pcm16WAV(500), map[string]string{"name": "my_prompt"}); w.Code != http.StatusSeeOther {
		t.Errorf("save as without .wav: status %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "my_prompt.wav")); err != nil {
		t.Errorf("save as without .wav: %v", err)
	}
	if w := upload(r, "prompt", pcm16WAV(500), nil); w.Code != http.StatusSeeOther {
		t.Errorf("file without extension: status %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "prompt.wav")); err != nil {
		t.Errorf("file without extension: %v", err)
	}

	if w := postForm(r, "/prompts/ask.wav/delete", url.Values{}); w.Code != http.StatusSeeOther {
		t.Fatalf("delete: status %d", w.Code)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Errorf("delete left the file: %v", err)
	}
}

// The new pages must render with the real templates.
func TestPromptsAndScenarioPagesRender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	cfg := &config.Config{ScenariosDir: dir, BotsDir: t.TempDir()}
	r := gin.New()
	r.LoadHTMLGlob("../templates/*.html")
	(&Server{Cfg: cfg, Runner: &controller.Runner{Cfg: cfg}}).Register(r)

	os.WriteFile(filepath.Join(dir, "probe.xml"), []byte("<config/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "probe.checks.json"), []byte(`{"thresholds": {"rx_speech_ms": {"min": 3000}}}`), 0o644)
	upload(r, "ask.wav", pcm16WAV(500), nil)

	for path, want := range map[string][]string{
		"/prompts":         {"ask.wav", "500 ms", filepath.Join(dir, "prompts", "ask.wav"), `"wav": "prompts/`},
		"/scenarios/probe": {"Audio checks", `&#34;rx_speech_ms&#34;`, "expect"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", path, w.Code, w.Body)
		}
		for _, s := range want {
			if !strings.Contains(w.Body.String(), s) {
				t.Errorf("%s: missing %q", path, s)
			}
		}
	}
}
