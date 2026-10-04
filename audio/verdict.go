package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Metrics names the call-wide metrics a Config may set thresholds on. Each expect
// entry adds expect.<name>.heard_ms, .heard_at_ms and .offset_ms.
var Metrics = []string{
	"rx_rtp_packets",
	"rx_speech_ms",
	"rx_first_speech_ms",
}

// Threshold bounds one metric; either side may be omitted.
type Threshold struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// Expect is audio the far end should be heard playing: an IVR prompt, an
// announcement, or, for an echo target, the scenario's own play= file.
type Expect struct {
	Name string `json:"name"`
	// WAV is a 16-bit PCM WAV; relative paths resolve against the scenarios dir.
	WAV       string     `json:"wav"`
	HeardMs   *Threshold `json:"heard_ms,omitempty"`    // how much of it was recognised
	HeardAtMs *Threshold `json:"heard_at_ms,omitempty"` // when it was first heard, ms after answer
}

// Config is a scenario's <name>.checks.json.
type Config struct {
	// CallLabel picks the call action to check when a scenario has several.
	CallLabel  string               `json:"call_label,omitempty"`
	Thresholds map[string]Threshold `json:"thresholds,omitempty"`
	Expect     []Expect             `json:"expect,omitempty"`
	Params     Params               `json:"params,omitempty"`
}

// Validate rejects config that would otherwise silently never fail.
func (c *Config) Validate() error {
	known := map[string]bool{}
	for _, m := range Metrics {
		known[m] = true
	}
	var unknown []string
	for name := range c.Thresholds {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown metrics %v; known: %v (expected audio goes in \"expect\")", unknown, Metrics)
	}
	if err := c.Params.validate(); err != nil {
		return fmt.Errorf("params: %w", err)
	}
	seen := map[string]bool{}
	for i, e := range c.Expect {
		if !validName(e.Name) {
			return fmt.Errorf("expect[%d]: name %q must be letters, digits, '_' or '-'", i, e.Name)
		}
		if seen[e.Name] {
			return fmt.Errorf("expect: duplicate name %q", e.Name)
		}
		seen[e.Name] = true
		if e.WAV == "" {
			return fmt.Errorf("expect %q: wav is required", e.Name)
		}
	}
	return nil
}

func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// Call is what the verdict needs from voip_patrol's result for the checked call.
type Call struct {
	CallID    string
	RxPackets int
	HasRTP    bool
}

// Check is one rule applied to one metric.
type Check struct {
	Name   string   `json:"name"`
	Value  *float64 `json:"value"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	Passed bool     `json:"passed"`
}

// ValueText renders the measured value for the UI and alerts.
func (c Check) ValueText() string {
	if c.Value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%g", *c.Value)
}

// WantText renders the rule, e.g. ">= 3000 and <= 9000".
func (c Check) WantText() string {
	var want []string
	if c.Min != nil {
		want = append(want, fmt.Sprintf(">= %g", *c.Min))
	}
	if c.Max != nil {
		want = append(want, fmt.Sprintf("<= %g", *c.Max))
	}
	return strings.Join(want, " and ")
}

// Verdict is the audio outcome of a run, stored on the run record.
type Verdict struct {
	Passed  bool                `json:"passed"`
	Error   string              `json:"error,omitempty"` // analysis could not run
	Checks  []Check             `json:"checks"`
	Metrics map[string]*float64 `json:"metrics"`
	Report  *Report             `json:"report,omitempty"`
}

// Summary is a one-line reason, used in alerts.
func (v *Verdict) Summary() string {
	if v.Error != "" {
		return v.Error
	}
	var parts []string
	for _, c := range v.Checks {
		if !c.Passed {
			parts = append(parts, fmt.Sprintf("%s=%s (want %s)", c.Name, c.ValueText(), c.WantText()))
		}
	}
	if len(parts) == 0 {
		return "all audio checks passed"
	}
	return strings.Join(parts, ", ")
}

func fp(v int) *float64 {
	f := float64(v)
	return &f
}

func fpp(v *int) *float64 {
	if v == nil {
		return nil
	}
	return fp(*v)
}

// FindRecording returns voip_patrol's inbound recording for callID in runDir, named
// record_<Call-ID>_<contact>_rec.wav (record_tx's ..._tx.wav is what we sent, so it's
// skipped). With no Call-ID match, a lone inbound recording is used.
func FindRecording(runDir, callID string) string {
	all, _ := filepath.Glob(filepath.Join(runDir, "record_*_rec.wav"))
	sort.Strings(all)
	if callID != "" {
		for _, p := range all {
			if strings.HasPrefix(filepath.Base(p), "record_"+callID+"_") {
				return p
			}
		}
	}
	if len(all) == 1 {
		return all[0]
	}
	return ""
}

func evaluate(name string, val *float64, t Threshold) Check {
	ok := val != nil && (t.Min == nil || *val >= *t.Min) && (t.Max == nil || *val <= *t.Max)
	return Check{Name: name, Value: val, Min: t.Min, Max: t.Max, Passed: ok}
}

// Evaluate analyzes the run's recording and applies cfg.
func Evaluate(runDir, scenariosDir string, cfg *Config, call Call) *Verdict {
	v := &Verdict{Metrics: map[string]*float64{}, Checks: []Check{}}
	if call.HasRTP {
		v.Metrics["rx_rtp_packets"] = fp(call.RxPackets)
	}

	rec := FindRecording(runDir, call.CallID)
	if rec == "" {
		v.Error = `no recording: add record="true" to the call action`
		return v
	}
	var expected []Expected
	for _, e := range cfg.Expect {
		path := e.WAV
		if !filepath.IsAbs(path) {
			path = filepath.Join(scenariosDir, path)
		}
		if _, err := os.Stat(path); err != nil {
			v.Error = fmt.Sprintf("expect %q: %v", e.Name, err)
			return v
		}
		expected = append(expected, Expected{Name: e.Name, Path: path})
	}
	r, err := Analyze(rec, expected, cfg.Params)
	if err != nil {
		v.Error = "analyze: " + err.Error()
		return v
	}
	v.Report = r
	v.Metrics["rx_speech_ms"] = fp(r.RxSpeechMs)
	v.Metrics["rx_first_speech_ms"] = fpp(r.RxFirstSpeechMs)

	names := make([]string, 0, len(cfg.Thresholds))
	for n := range cfg.Thresholds {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v.Checks = append(v.Checks, evaluate(n, v.Metrics[n], cfg.Thresholds[n]))
	}

	for i, e := range cfg.Expect {
		h := r.Expect[i]
		prefix := "expect." + e.Name + "."
		v.Metrics[prefix+"heard_ms"] = fp(h.HeardMs)
		v.Metrics[prefix+"heard_at_ms"] = fpp(h.HeardAtMs)
		v.Metrics[prefix+"offset_ms"] = fpp(h.OffsetMs)
		if e.HeardMs == nil && e.HeardAtMs == nil {
			// Listing expected audio without a rule means it must be heard at all.
			one := 1.0
			v.Checks = append(v.Checks, evaluate(prefix+"heard_ms", v.Metrics[prefix+"heard_ms"], Threshold{Min: &one}))
			continue
		}
		if e.HeardMs != nil {
			v.Checks = append(v.Checks, evaluate(prefix+"heard_ms", v.Metrics[prefix+"heard_ms"], *e.HeardMs))
		}
		if e.HeardAtMs != nil {
			v.Checks = append(v.Checks, evaluate(prefix+"heard_at_ms", v.Metrics[prefix+"heard_at_ms"], *e.HeardAtMs))
		}
	}

	v.Passed = true
	for _, c := range v.Checks {
		v.Passed = v.Passed && c.Passed
	}
	return v
}
