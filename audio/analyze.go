package audio

import (
	"fmt"
	"math"
	"sort"
)

// FrameMs is the analysis resolution: one RMS value per 10 ms, which also lets
// WAVs at different sample rates share a time axis.
const FrameMs = 10

// Params tune the analysis. The defaults suit 8/16 kHz telephone speech.
type Params struct {
	SpeechRMS     float64 `json:"speech_rms,omitempty"` // int16 RMS above which a frame is audio
	MinSegmentMs  int     `json:"min_segment_ms,omitempty"`
	MergeGapMs    int     `json:"merge_gap_ms,omitempty"` // sounds closer than this form one segment
	MatchMinCorr  float64 `json:"match_min_corr,omitempty"`
	MatchWindowMs int     `json:"match_window_ms,omitempty"`
	MatchStepMs   int     `json:"match_step_ms,omitempty"`
}

// DefaultParams returns the tuning used when a scenario doesn't override it.
func DefaultParams() Params {
	return Params{
		SpeechRMS:     300,
		MinSegmentMs:  200,
		MergeGapMs:    700,
		MatchMinCorr:  0.8,
		MatchWindowMs: 1500,
		MatchStepMs:   250,
	}
}

func (p Params) withDefaults() Params {
	d := DefaultParams()
	if p.SpeechRMS == 0 {
		p.SpeechRMS = d.SpeechRMS
	}
	if p.MinSegmentMs == 0 {
		p.MinSegmentMs = d.MinSegmentMs
	}
	if p.MergeGapMs == 0 {
		p.MergeGapMs = d.MergeGapMs
	}
	if p.MatchMinCorr == 0 {
		p.MatchMinCorr = d.MatchMinCorr
	}
	if p.MatchWindowMs == 0 {
		p.MatchWindowMs = d.MatchWindowMs
	}
	if p.MatchStepMs == 0 {
		p.MatchStepMs = d.MatchStepMs
	}
	return p
}

// Expected names a WAV the far end should be heard playing.
type Expected struct {
	Name string
	Path string
}

// Match is one window of an expected WAV found in the recording.
type Match struct {
	RefMs int     `json:"ref_ms"` // position in the expected WAV
	AtMs  int     `json:"at_ms"`  // position in the recording (ms after answer)
	Corr  float64 `json:"corr"`
}

// Heard is how much of an expected WAV the recording contains, and from when.
type Heard struct {
	Name      string  `json:"name"`
	HeardMs   int     `json:"heard_ms"`
	HeardAtMs *int    `json:"heard_at_ms"` // first heard, ms after answer
	OffsetMs  *int    `json:"offset_ms"`   // recording position minus WAV position
	Matches   []Match `json:"matches"`
}

// Text is the one-line summary shown on the run page.
func (h Heard) Text() string {
	if h.HeardAtMs == nil {
		return "not heard"
	}
	t := fmt.Sprintf("heard %.1f s of it · first heard at %.1f s", float64(h.HeardMs)/1000, float64(*h.HeardAtMs)/1000)
	if h.OffsetMs != nil {
		t += fmt.Sprintf(" · offset %+d ms", *h.OffsetMs)
	}
	return t
}

// Report is what Analyze measured. Times are from answer, which is when
// voip_patrol starts its recorder.
type Report struct {
	DurationMs      int      `json:"duration_ms"`
	RxSpeechMs      int      `json:"rx_speech_ms"`
	RxFirstSpeechMs *int     `json:"rx_first_speech_ms"`
	RxSegmentsMs    [][2]int `json:"rx_segments_ms"`
	Expect          []Heard  `json:"expect"`
}

func median(xs []int) float64 {
	s := append([]int(nil), xs...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return float64(s[n/2-1]+s[n/2]) / 2
}

// Envelope is the RMS of each 10 ms frame.
func Envelope(samples []float64, sampleRate int) []float64 {
	n := sampleRate * FrameMs / 1000
	if n == 0 {
		return nil
	}
	out := make([]float64, len(samples)/n)
	for i := range out {
		var sum float64
		for _, x := range samples[i*n : (i+1)*n] {
			sum += x * x
		}
		out[i] = math.Sqrt(sum / float64(n))
	}
	return out
}

// runs returns the [start, end) frame ranges where env exceeds thr.
func runs(env []float64, thr float64) [][2]int {
	var out [][2]int
	start := -1
	for i, v := range env {
		if v > thr && start < 0 {
			start = i
		} else if v <= thr && start >= 0 {
			out = append(out, [2]int{start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(env)})
	}
	return out
}

// Segments returns the frame ranges of audio, merged across short gaps.
func Segments(env []float64, p Params) [][2]int {
	var merged [][2]int
	for _, r := range runs(env, p.SpeechRMS) {
		if r[1]-r[0] < p.MinSegmentMs/FrameMs {
			continue
		}
		if n := len(merged); n > 0 && r[0]-merged[n-1][1] < p.MergeGapMs/FrameMs {
			merged[n-1][1] = r[1]
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// bestMatch slides one window of ref over rx positions [lo, hi] and returns the best
// Pearson correlation and where it occurs. Prefix sums keep each position O(window).
func bestMatch(win []float64, rx, sum, sumSq []float64, lo, hi int) (float64, int) {
	n := float64(len(win))
	var mean float64
	for _, v := range win {
		mean += v
	}
	mean /= n
	centered := make([]float64, len(win))
	var norm float64
	for i, v := range win {
		centered[i] = v - mean
		norm += centered[i] * centered[i]
	}
	if norm == 0 {
		return 0, -1
	}
	bestC, bestQ := 0.0, -1
	for q := max(lo, 0); q <= hi && q+len(win) <= len(rx); q++ {
		s, ss := sum[q+len(win)]-sum[q], sumSq[q+len(win)]-sumSq[q]
		v := ss - s*s/n
		if v <= 0 {
			continue
		}
		var dot float64
		for i, c := range centered {
			dot += c * rx[q+i]
		}
		if c := dot / math.Sqrt(norm*v); c > bestC {
			bestC, bestQ = c, q
		}
	}
	return bestC, bestQ
}

// peaks returns every position in rx where the window correlates >= minCorr, one per
// run of qualifying positions. Audio played on a loop appears once per pass.
func peaks(win []float64, rx, sum, sumSq []float64, minCorr float64) []int {
	var out []int
	for q := 0; q+len(win) <= len(rx); {
		c, at := bestMatch(win, rx, sum, sumSq, q, q+len(win)-1)
		if c < minCorr {
			q += len(win)
			continue
		}
		out = append(out, at)
		q = at + len(win)
	}
	return out
}

// earliestCluster groups offsets within 100 ms of each other and returns the median
// of the earliest well-supported group: at least minMatches windows and half the
// strongest group's. Each pass of looped audio is one such group; chance groups stay small.
func earliestCluster(ms []Match) (int, bool) {
	sorted := append([]Match(nil), ms...)
	off := func(m Match) int { return m.AtMs - m.RefMs }
	sort.Slice(sorted, func(i, j int) bool { return off(sorted[i]) < off(sorted[j]) })
	type group struct {
		offs    []int
		windows int
	}
	var groups []group
	best := 0
	for i := 0; i < len(sorted); {
		j := i + 1
		for j < len(sorted) && off(sorted[j])-off(sorted[j-1]) <= 100 {
			j++
		}
		g, windows := group{}, map[int]bool{}
		for _, m := range sorted[i:j] {
			windows[m.RefMs] = true
			g.offs = append(g.offs, off(m))
		}
		g.windows = len(windows)
		groups = append(groups, g)
		best = max(best, g.windows)
		i = j
	}
	need := max(minMatches, (best+1)/2)
	for _, g := range groups {
		if g.windows >= need {
			return int(median(g.offs)), true
		}
	}
	return 0, false
}

// keepConsistent keeps matches whose offset is near the median, plus neighbours that
// drift at most 60 ms per window from the last kept one (jitter buffers, clock skew).
// A match at an unrelated offset is a chance resemblance, not the expected audio.
func keepConsistent(ms []Match, medianOffset int) []Match {
	off := func(i int) int { return ms[i].AtMs - ms[i].RefMs }
	keep := make([]bool, len(ms))
	for i := range ms {
		keep[i] = abs(off(i)-medianOffset) <= 100
	}
	for _, dir := range []int{1, -1} {
		last := -1
		for k := range ms {
			i := k
			if dir < 0 {
				i = len(ms) - 1 - k
			}
			switch {
			case keep[i]:
				last = i
			case last >= 0 && abs(off(i)-off(last)) <= 60:
				keep[i], last = true, i
			}
		}
	}
	var out []Match
	for i, m := range ms {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}

// onset returns the start of the earliest ref word, walking back from end, that is
// heard unbroken at the given offset. Word-level so one stray click doesn't count.
func onset(ref, rx []float64, end, offset, floor int, thr float64) int {
	var words [][2]int
	for _, w := range runs(ref[floor:end], thr) {
		if w[1]-w[0] >= 5 {
			words = append(words, [2]int{w[0] + floor, w[1] + floor})
		}
	}
	first := end
	for i := len(words) - 1; i >= 0; i-- {
		s, e := words[i][0], words[i][1]
		if s+offset < 0 || e+offset > len(rx) {
			break
		}
		heard := 0
		for _, v := range rx[s+offset : e+offset] {
			if v > thr {
				heard++
			}
		}
		if float64(heard)/float64(e-s) < 0.5 {
			break
		}
		first = s
	}
	return first
}

const (
	minMatches = 3   // windows that must agree before audio counts as heard
	maxDrift   = 150 // frames (1.5 s) pass 2 searches around the consensus position
)

// FindExpected reports how much of ref (an expected WAV's envelope) the recording
// envelope rx contains, and when it was first heard.
func FindExpected(rx, ref []float64, p Params) Heard {
	p = p.withDefaults()
	h := Heard{Matches: []Match{}}
	win, step := p.MatchWindowMs/FrameMs, p.MatchStepMs/FrameMs
	if win == 0 || len(rx) < win || len(ref) < win {
		return h
	}
	sum, sumSq := make([]float64, len(rx)+1), make([]float64, len(rx)+1)
	for i, v := range rx {
		sum[i+1], sumSq[i+1] = sum[i]+v, sumSq[i]+v*v
	}

	// Windows of ref worth matching: mostly-silent ones have nothing to recognise.
	var starts []int
	for s := 0; s+win <= len(ref); s += step {
		active := 0
		for _, v := range ref[s : s+win] {
			if v > p.SpeechRMS {
				active++
			}
		}
		if float64(active)/float64(win) >= 0.3 {
			starts = append(starts, s)
		}
	}
	match := func(lo, hi func(s int) int) []Match {
		var ms []Match
		for _, s := range starts {
			if c, q := bestMatch(ref[s:s+win], rx, sum, sumSq, lo(s), hi(s)); c >= p.MatchMinCorr {
				ms = append(ms, Match{RefMs: s * FrameMs, AtMs: q * FrameMs, Corr: math.Round(c*1000) / 1000})
			}
		}
		return ms
	}
	offsets := func(ms []Match) []int {
		o := make([]int, len(ms))
		for i, m := range ms {
			o[i] = m.AtMs - m.RefMs
		}
		return o
	}

	// Pass 1 finds where ref is first heard; pass 2 re-matches every window near that
	// position, so a chance resemblance elsewhere can't win a window.
	var all []Match
	for _, s := range starts {
		for _, q := range peaks(ref[s:s+win], rx, sum, sumSq, p.MatchMinCorr) {
			all = append(all, Match{RefMs: s * FrameMs, AtMs: q * FrameMs})
		}
	}
	first, ok := earliestCluster(all)
	if !ok {
		return h // matches that don't agree on a position are chance resemblances
	}
	consensus := first / FrameMs
	ms := match(func(s int) int { return s + consensus - maxDrift }, func(s int) int { return s + consensus + maxDrift })
	ms = keepConsistent(ms, consensus*FrameMs)
	if len(ms) < minMatches {
		return h // a few scattered windows are a chance resemblance, not the expected audio
	}
	off := int(median(offsets(ms)))
	h.Matches = ms
	h.HeardMs = len(ms) * p.MatchStepMs
	h.OffsetMs = &off

	// Any earlier audible part would have matched its own window, so the onset lies
	// within one window before the first match.
	start := ms[0].RefMs / FrameMs
	at := onset(ref, rx, start+win, off/FrameMs, max(0, start-win), p.SpeechRMS)*FrameMs + off
	h.HeardAtMs = &at
	return h
}

// Analyze measures an inbound recording, and looks for each expected WAV in it.
func Analyze(recording string, expected []Expected, p Params) (*Report, error) {
	p = p.withDefaults()
	samples, rate, err := ReadPCM16(recording)
	if err != nil {
		return nil, err
	}
	rx := Envelope(samples, rate)
	segs := Segments(rx, p)

	r := &Report{DurationMs: len(rx) * FrameMs, RxSegmentsMs: [][2]int{}, Expect: []Heard{}}
	for _, s := range segs {
		r.RxSpeechMs += (s[1] - s[0]) * FrameMs
		r.RxSegmentsMs = append(r.RxSegmentsMs, [2]int{s[0] * FrameMs, s[1] * FrameMs})
	}
	if len(segs) > 0 {
		v := segs[0][0] * FrameMs
		r.RxFirstSpeechMs = &v
	}
	for _, e := range expected {
		es, erate, err := ReadPCM16(e.Path)
		if err != nil {
			return nil, err
		}
		h := FindExpected(rx, Envelope(es, erate), p)
		h.Name = e.Name
		r.Expect = append(r.Expect, h)
	}
	return r, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
