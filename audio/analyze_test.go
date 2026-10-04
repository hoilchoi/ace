package audio

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite <name>.want.json from the current output")

// TestAnalyzeGolden runs real recordings kept outside the repo: point ACE_AUDIO_TESTDATA
// at a dir of <name>.wav (voip_patrol's recording), <name>.expect.wav (audio expected
// in it) and <name>.want.json. A new case starts as {} and is filled in with -update.
func TestAnalyzeGolden(t *testing.T) {
	dir := os.Getenv("ACE_AUDIO_TESTDATA")
	if dir == "" {
		t.Skip("set ACE_AUDIO_TESTDATA to a dir of recordings to run golden tests")
	}
	cases, _ := filepath.Glob(filepath.Join(dir, "*.want.json"))
	if len(cases) == 0 {
		t.Fatalf("no *.want.json in %s", dir)
	}
	for _, wantPath := range cases {
		name := strings.TrimSuffix(filepath.Base(wantPath), ".want.json")
		t.Run(name, func(t *testing.T) {
			got, err := Analyze(filepath.Join(dir, name+".wav"), []Expected{{Name: "expected", Path: filepath.Join(dir, name+".expect.wav")}}, Params{})
			if err != nil {
				t.Fatal(err)
			}
			if *update {
				b, _ := json.MarshalIndent(got, "", " ")
				if err := os.WriteFile(wantPath, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			raw, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatal(err)
			}
			var want Report
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			eqInt(t, "duration_ms", &got.DurationMs, &want.DurationMs)
			eqInt(t, "rx_speech_ms", &got.RxSpeechMs, &want.RxSpeechMs)
			eqInt(t, "rx_first_speech_ms", got.RxFirstSpeechMs, want.RxFirstSpeechMs)
			if len(want.Expect) != 1 {
				t.Fatalf("want.json has %d expect results", len(want.Expect))
			}
			g, w := got.Expect[0], want.Expect[0]
			eqInt(t, "heard_ms", &g.HeardMs, &w.HeardMs)
			eqInt(t, "heard_at_ms", g.HeardAtMs, w.HeardAtMs)
			eqInt(t, "offset_ms", g.OffsetMs, w.OffsetMs)
			if len(g.Matches) != len(w.Matches) {
				t.Fatalf("matches: got %d want %d", len(g.Matches), len(w.Matches))
			}
			for i := range g.Matches {
				gm, wm := g.Matches[i], w.Matches[i]
				if gm.RefMs != wm.RefMs || gm.AtMs != wm.AtMs || math.Abs(gm.Corr-wm.Corr) > 0.0015 {
					t.Errorf("match %d: got %+v want %+v", i, gm, wm)
				}
			}
		})
	}
}

func eqInt(t *testing.T, name string, got, want *int) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s: got %v want %v", name, deref(got), deref(want))
	case *got != *want:
		t.Errorf("%s: got %d want %d", name, *got, *want)
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// --- synthetic -------------------------------------------------------------------

type burst struct{ s, e float64 }

// writeWAV writes silence with 440 Hz tone bursts at the given [start, end) seconds.
func writeWAV(t *testing.T, path string, rate int, durS float64, bursts []burst) string {
	t.Helper()
	n := int(float64(rate) * durS)
	data := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		ts := float64(i) / float64(rate)
		var v float64
		for _, b := range bursts {
			if ts >= b.s && ts < b.e {
				v = 8000 * math.Sin(2*math.Pi*440*ts)
			}
		}
		binary.LittleEndian.PutUint16(data[2*i:], uint16(int16(v)))
	}
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(data)))
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1)
	binary.LittleEndian.PutUint16(hdr[22:], 1)
	binary.LittleEndian.PutUint32(hdr[24:], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(hdr[32:], 2)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(hdr, data...), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// wordLike returns bursts of varying length from start to end, so any window of
// them has a shape no other window shares. seed shifts the rhythm.
func wordLike(start, end float64, seed int) []burst {
	words, gaps := []float64{0.3, 0.5, 0.25, 0.4, 0.35, 0.6}, []float64{0.2, 0.15, 0.3, 0.2, 0.25}
	var out []burst
	for t, i := start, seed; t+words[i%6] < end; i++ {
		out = append(out, burst{t, t + words[i%6]})
		t += words[i%6] + gaps[i%5]
	}
	return out
}

func shift(bs []burst, from, by float64) []burst {
	var out []burst
	for _, b := range bs {
		if b.s >= from {
			out = append(out, burst{b.s + by, b.e + by})
		}
	}
	return out
}

func firstFrom(bs []burst, from float64) float64 {
	first := math.Inf(1)
	for _, b := range bs {
		if b.s >= from && b.s < first {
			first = b.s
		}
	}
	return first
}

// heard writes the expected WAV (8 kHz) and the recording (16 kHz), and looks for one in the other.
func heard(t *testing.T, expected, recording []burst, recS float64) Heard {
	t.Helper()
	dir := t.TempDir()
	exp := writeWAV(t, filepath.Join(dir, "expected.wav"), 8000, 12.0, expected)
	rec := writeWAV(t, filepath.Join(dir, "rec.wav"), 16000, recS, recording)
	r, err := Analyze(rec, []Expected{{Name: "x", Path: exp}}, Params{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Expect[0]
}

// Echo target: the expected audio is our own prompt, coming back from the bridge on.
func TestHeardEcho(t *testing.T) {
	prompt := append([]burst{{1.0, 2.0}}, wordLike(3.0, 12.0, 0)...)
	for _, bridge := range []float64{3.0, 4.0, 6.0, 8.0} {
		h := heard(t, prompt, shift(prompt, bridge, 0.15), 12.0)
		want := int(math.Round((firstFrom(prompt, bridge) + 0.15) * 1000))
		if h.HeardAtMs == nil || abs(*h.HeardAtMs-want) > 20 {
			t.Errorf("bridge %.0fs: heard_at_ms %v, want ~%d", bridge, deref(h.HeardAtMs), want)
		}
		if h.OffsetMs == nil || abs(*h.OffsetMs-150) > 10 {
			t.Errorf("bridge %.0fs: offset %v, want ~150 (the round trip)", bridge, deref(h.OffsetMs))
		}
	}
}

// Announcement / IVR prompt: the expected audio arrives at an arbitrary time.
func TestHeardAnnouncementAtAnyTime(t *testing.T) {
	announcement := wordLike(0.5, 5.0, 2)
	h := heard(t, announcement, append([]burst{{0.0, 0.8}}, shift(announcement, 0, 6.0)...), 15.0)
	if want := int(math.Round((announcement[0].s + 6.0) * 1000)); h.HeardAtMs == nil || abs(*h.HeardAtMs-want) > 20 {
		t.Errorf("heard_at_ms %v, want ~%d", deref(h.HeardAtMs), want)
	}
	if h.HeardMs < 3000 {
		t.Errorf("heard_ms %d, want most of the 4.5 s announcement", h.HeardMs)
	}
}

func TestNotHeard(t *testing.T) {
	expected := wordLike(0.5, 8.0, 0)
	for name, rec := range map[string][]burst{
		"silence":         nil,
		"different audio": {{6.0, 6.9}, {7.1, 7.3}, {7.6, 8.8}, {9.0, 9.2}, {9.5, 11.5}},
	} {
		if h := heard(t, expected, rec, 12.0); h.HeardMs != 0 || h.HeardAtMs != nil {
			t.Errorf("%s: heard_ms %d heard_at %v, want nothing", name, h.HeardMs, deref(h.HeardAtMs))
		}
	}
}

// Real paths drift (jitter buffers, clock skew): here 60 ms per second. It still counts.
func TestHeardFollowsDrift(t *testing.T) {
	prompt := wordLike(1.0, 12.0, 0)
	var drifting []burst
	for _, b := range prompt {
		if b.s >= 4.0 {
			d := 0.15 + 0.06*(b.s-4.0)
			drifting = append(drifting, burst{b.s + d, b.e + d})
		}
	}
	steady := heard(t, prompt, shift(prompt, 4.0, 0.15), 12.0)
	if h := heard(t, prompt, drifting, 12.0); h.HeardMs < steady.HeardMs*9/10 {
		t.Errorf("drifting heard_ms %d, want close to the steady %d", h.HeardMs, steady.HeardMs)
	}
}

func TestInboundSpeech(t *testing.T) {
	dir := t.TempDir()
	r, err := Analyze(writeWAV(t, filepath.Join(dir, "rec.wav"), 16000, 5.0, []burst{{1.0, 2.0}}), nil, Params{})
	if err != nil {
		t.Fatal(err)
	}
	if deref(r.RxFirstSpeechMs) != 1000 || r.RxSpeechMs != 1000 || len(r.Expect) != 0 {
		t.Errorf("got %+v", r)
	}
}
