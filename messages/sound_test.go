// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuiltinSoundFormat(t *testing.T) {
	b := builtinSound
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatal("not a RIFF/WAVE file")
	}
	le := binary.LittleEndian
	if string(b[12:16]) != "fmt " {
		t.Fatal("missing fmt chunk")
	}
	if f, ch, rate, bits := le.Uint16(b[20:]), le.Uint16(b[22:]), le.Uint32(b[24:]), le.Uint16(b[34:]); f != 1 || ch != 1 || rate != 44100 || bits != 16 {
		t.Fatalf("format = %d/%d/%d/%d", f, ch, rate, bits)
	}
	if string(b[36:40]) != "data" {
		t.Fatal("missing data chunk")
	}
	data := b[44:]
	if dur := float64(len(data)/2) / 44100; dur >= 0.4 {
		t.Fatalf("duration %.3fs too long", dur)
	}
	peak := 0
	for i := 0; i+1 < len(data); i += 2 {
		s := int(int16(le.Uint16(data[i:])))
		if s < 0 {
			s = -s
		}
		if s > peak {
			peak = s
		}
	}
	if peak > 16422 || peak < 8000 {
		t.Fatalf("peak %d out of range", peak)
	}
}

func TestSoundFileBuiltin(t *testing.T) {
	dir := t.TempDir()
	p, warn := soundFile("", dir)
	if warn != nil {
		t.Fatal(warn)
	}
	if p != filepath.Join(dir, "whatscli", "notify.wav") {
		t.Fatalf("path = %s", p)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != string(builtinSound) {
		t.Fatalf("content mismatch: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if _, warn := soundFile("", dir); warn != nil {
		t.Fatal(warn)
	}
	st, _ := os.Stat(p)
	if !st.ModTime().Equal(old) {
		t.Fatal("file was rewritten")
	}
}

func TestSoundFileCustom(t *testing.T) {
	dir := t.TempDir()
	c := filepath.Join(dir, "my.wav")
	if err := os.WriteFile(c, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, warn := soundFile(c, dir)
	if warn != nil || p != c {
		t.Fatalf("got %s, %v", p, warn)
	}
}

func TestSoundFileFallsBackOnBadPath(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "whatscli", "notify.wav")
	for _, bad := range []string{"/nonexistent/x.wav", dir} {
		p, warn := soundFile(bad, dir)
		if p != want {
			t.Errorf("%s: path = %s", bad, p)
		}
		if warn == nil || !strings.Contains(warn.Error(), bad) {
			t.Errorf("%s: warn = %v", bad, warn)
		}
	}
}

func TestPickPlayer(t *testing.T) {
	look := func(have ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	tests := []struct {
		goos string
		have []string
		cmd  string
		args []string
		ok   bool
	}{
		{"linux", []string{"pw-play", "paplay"}, "pw-play", []string{"f.wav"}, true},
		{"linux", []string{"paplay"}, "paplay", []string{"f.wav"}, true},
		{"linux", []string{"aplay"}, "aplay", []string{"-q", "f.wav"}, true},
		{"linux", nil, "", nil, false},
		{"freebsd", []string{"paplay"}, "paplay", []string{"f.wav"}, true},
		{"darwin", []string{"afplay"}, "afplay", []string{"f.wav"}, true},
		{"windows", []string{"powershell"}, "powershell", []string{"-NoProfile", "-Command", "(New-Object Media.SoundPlayer 'f.wav').PlaySync()"}, true},
	}
	for _, tc := range tests {
		cmd, args, ok := pickPlayer(tc.goos, look(tc.have...), "f.wav")
		if cmd != tc.cmd || ok != tc.ok || !reflect.DeepEqual(args, tc.args) {
			t.Errorf("%s %v: got %q %v %v", tc.goos, tc.have, cmd, args, ok)
		}
	}
	_, args, _ := pickPlayer("windows", look("powershell"), "C:\\it's.wav")
	if !strings.Contains(args[2], "'C:\\it''s.wav'") {
		t.Errorf("quote not doubled: %v", args)
	}
}

func newTestPlayer(run func(name string, args ...string) error, report func(error)) *soundPlayer {
	p := newSoundPlayer(report)
	p.pick = func(string) (string, []string, bool) { return "fake", []string{"f"}, true }
	p.run = func(_ context.Context, name string, args ...string) error { return run(name, args...) }
	return p
}

func TestPlayerSingleFlight(t *testing.T) {
	var calls atomic.Int32
	gate := make(chan struct{})
	started := make(chan struct{}, 4)
	p := newTestPlayer(func(string, ...string) error {
		calls.Add(1)
		started <- struct{}{}
		<-gate
		return nil
	}, func(error) {})
	p.Play("f")
	<-started
	p.Play("f")
	p.Play("f")
	close(gate)
	p.wait()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	p.Play("f")
	p.wait()
	if calls.Load() != 2 {
		t.Fatalf("calls after = %d", calls.Load())
	}
}

func TestPlayerErrorReportedOnce(t *testing.T) {
	var mu sync.Mutex
	reports := 0
	p := newTestPlayer(func(string, ...string) error { return errors.New("boom") },
		func(error) { mu.Lock(); reports++; mu.Unlock() })
	for i := 0; i < 3; i++ {
		p.Play("f")
		p.wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if reports != 1 {
		t.Fatalf("reports = %d", reports)
	}
}

func TestPlayerLooksUpOnce(t *testing.T) {
	var picks atomic.Int32
	p := newTestPlayer(func(string, ...string) error { return nil }, func(error) {})
	p.pick = func(string) (string, []string, bool) { picks.Add(1); return "fake", nil, true }
	if !p.HasPlayer("f") {
		t.Fatal("HasPlayer = false")
	}
	for i := 0; i < 3; i++ {
		p.Play("f")
		p.wait()
	}
	if picks.Load() != 1 {
		t.Fatalf("picks = %d, want 1", picks.Load())
	}
}
