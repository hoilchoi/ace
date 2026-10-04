package audio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A header can claim far more data than the file holds (a truncated recording, or a
// hostile upload). The reader must stop at the end of the file, not allocate the claim.
func TestReadPCM16TrustsTheFileNotTheHeader(t *testing.T) {
	path := writeWAV(t, filepath.Join(t.TempDir(), "x.wav"), 8000, 0.5, []burst{{0, 0.5}})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(b[40:], 0xFFFFFFF0) // data chunk claims ~4 GB
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	samples, rate, err := ReadPCM16(path)
	runtime.ReadMemStats(&after)
	if err != nil || rate != 8000 || len(samples) != 4000 {
		t.Fatalf("got %d samples at %d Hz, err %v; want the 4000 actually present", len(samples), rate, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 10<<20 {
		t.Errorf("allocated %d MB reading an 8 KB file", allocated>>20)
	}
}
