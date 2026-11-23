// SPDX-License-Identifier: AGPL-3.0-only

// Command gen-notify-sound synthesises the built-in notification sound: two
// short ascending plucks, written as a 16-bit mono 44.1 kHz PCM WAV file.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
)

const (
	sampleRate = 44100
	peakTarget = 16422 // -6 dBFS
)

// note renders one pluck: fundamental plus a 0.3-amplitude second harmonic,
// a sub-5 ms linear attack and an exponential decay.
func note(freq, dur float64) []float64 {
	n := int(dur * sampleRate)
	attack := 0.004 * sampleRate
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / sampleRate
		env := math.Exp(-t * 35)
		if float64(i) < attack {
			env *= float64(i) / attack
		}
		w := math.Sin(2*math.Pi*freq*t) + 0.3*math.Sin(2*math.Pi*2*freq*t)
		out[i] = w * env
	}
	return out
}

func main() {
	outPath := flag.String("o", "sound/notify.wav", "output WAV path")
	flag.Parse()

	var samples []float64
	samples = append(samples, note(880, 0.09)...)
	samples = append(samples, make([]float64, int(0.01*sampleRate))...)
	samples = append(samples, note(1320, 0.09)...)

	peak := 0.0
	for _, s := range samples {
		peak = math.Max(peak, math.Abs(s))
	}
	pcm := make([]int16, len(samples))
	for i, s := range samples {
		pcm[i] = int16(math.Round(s / peak * peakTarget))
	}

	dataLen := uint32(len(pcm) * 2)
	buf := make([]byte, 0, 44+int(dataLen))
	le := binary.LittleEndian
	buf = append(buf, "RIFF"...)
	buf = le.AppendUint32(buf, 36+dataLen)
	buf = append(buf, "WAVEfmt "...)
	buf = le.AppendUint32(buf, 16)
	buf = le.AppendUint16(buf, 1) // PCM
	buf = le.AppendUint16(buf, 1) // mono
	buf = le.AppendUint32(buf, sampleRate)
	buf = le.AppendUint32(buf, sampleRate*2)
	buf = le.AppendUint16(buf, 2)
	buf = le.AppendUint16(buf, 16)
	buf = append(buf, "data"...)
	buf = le.AppendUint32(buf, dataLen)
	for _, s := range pcm {
		buf = le.AppendUint16(buf, uint16(s))
	}
	if err := os.WriteFile(*outPath, buf, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
