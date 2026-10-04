package models

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jchavanton/ace/audio"
)

// Prompt is a WAV under <scenarios>/prompts/ that scenarios can play (play="...")
// and audio checks can expect ("wav": "prompts/<name>").
type Prompt struct {
	Name       string
	Path       string // absolute; what play="..." needs, since voip_patrol runs in the run dir
	SizeBytes  int64
	ModTime    time.Time
	DurationMs int
	SampleRate int
	Error      string // set when the file isn't a usable 16-bit PCM WAV
}

// PromptsDir is where prompt WAVs live.
func PromptsDir(scenariosDir string) string {
	return filepath.Join(scenariosDir, "prompts")
}

// SanitizePromptName returns name if it is a safe .wav file name, else "".
func SanitizePromptName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 5 || len(name) > 100 || name[0] == '.' || !strings.HasSuffix(strings.ToLower(name), ".wav") {
		return ""
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
		if !ok {
			return ""
		}
	}
	return name
}

// ListPrompts returns the prompt WAVs, sorted by name. A missing dir is an empty list.
func ListPrompts(scenariosDir string) ([]Prompt, error) {
	dir := PromptsDir(scenariosDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Prompt
	for _, e := range entries {
		if e.IsDir() || SanitizePromptName(e.Name()) == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := Prompt{Name: e.Name(), Path: filepath.Join(dir, e.Name()), SizeBytes: info.Size(), ModTime: info.ModTime()}
		if ms, rate, err := ProbePrompt(p.Path); err != nil {
			p.Error = err.Error()
		} else {
			p.DurationMs, p.SampleRate = ms, rate
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ProbePrompt checks that path is a playable 16-bit PCM WAV and returns its length.
func ProbePrompt(path string) (durationMs, sampleRate int, err error) {
	samples, rate, err := audio.ReadPCM16(path)
	if err != nil {
		return 0, 0, err
	}
	if rate <= 0 || len(samples) == 0 {
		return 0, 0, errors.New("WAV has no audio")
	}
	return len(samples) * 1000 / rate, rate, nil
}
