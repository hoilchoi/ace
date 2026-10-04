// Package audio derives speech, reply and echo metrics from voip_patrol's call
// recordings, and evaluates them against per-scenario thresholds.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadPCM16 returns the samples of a 16-bit PCM WAV (channels averaged to mono)
// and its sample rate.
func ReadPCM16(path string) ([]float64, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("%s: not a WAV file", path)
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	var channels, bits uint16
	var rate uint32
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, 0, fmt.Errorf("%s: no data chunk", path)
			}
			return nil, 0, err
		}
		id, size := string(hdr[0:4]), int64(binary.LittleEndian.Uint32(hdr[4:8]))
		// Never trust a header's size beyond the file: a truncated recording or a
		// hostile upload can claim gigabytes.
		pos, err := f.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, 0, err
		}
		size = min(size, fi.Size()-pos)
		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return nil, 0, err
			}
			if len(buf) < 16 {
				return nil, 0, fmt.Errorf("%s: short fmt chunk", path)
			}
			format := binary.LittleEndian.Uint16(buf[0:2])
			channels = binary.LittleEndian.Uint16(buf[2:4])
			rate = binary.LittleEndian.Uint32(buf[4:8])
			bits = binary.LittleEndian.Uint16(buf[14:16])
			if (format != 1 && format != 0xFFFE) || bits != 16 || channels == 0 {
				return nil, 0, fmt.Errorf("%s: only 16-bit PCM is supported", path)
			}
		case "data":
			if channels == 0 {
				return nil, 0, fmt.Errorf("%s: data before fmt chunk", path)
			}
			buf := make([]byte, size)
			n, err := io.ReadFull(f, buf)
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, 0, err
			}
			// A recorder killed mid-call leaves a short data chunk; use what's there.
			frames := n / (2 * int(channels))
			out := make([]float64, frames)
			for i := 0; i < frames; i++ {
				var sum float64
				for c := 0; c < int(channels); c++ {
					off := (i*int(channels) + c) * 2
					sum += float64(int16(binary.LittleEndian.Uint16(buf[off : off+2])))
				}
				out[i] = sum / float64(channels)
			}
			return out, int(rate), nil
		default:
			if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
				return nil, 0, err
			}
		}
	}
}
