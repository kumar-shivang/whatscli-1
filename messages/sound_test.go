// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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
		env  []string
		ok   bool
	}{
		{"linux", []string{"pw-play", "paplay"}, "pw-play", []string{"f.wav"}, nil, true},
		{"linux", []string{"paplay"}, "paplay", []string{"f.wav"}, nil, true},
		{"linux", []string{"aplay"}, "aplay", []string{"-q", "f.wav"}, nil, true},
		{"linux", nil, "", nil, nil, false},
		{"freebsd", []string{"paplay"}, "paplay", []string{"f.wav"}, nil, true},
		{"darwin", []string{"afplay"}, "afplay", []string{"f.wav"}, nil, true},
		{"windows", []string{"powershell"}, "powershell",
			[]string{"-NoProfile", "-Command", "(New-Object Media.SoundPlayer $env:WHATSCLI_SOUND).PlaySync()"},
			[]string{"WHATSCLI_SOUND=f.wav"}, true},
	}
	for _, tc := range tests {
		cmd, args, env, ok := pickPlayer(tc.goos, look(tc.have...), "f.wav")
		if cmd != tc.cmd || ok != tc.ok || !reflect.DeepEqual(args, tc.args) || !reflect.DeepEqual(env, tc.env) {
			t.Errorf("%s %v: got %q %v %v %v", tc.goos, tc.have, cmd, args, env, ok)
		}
	}
	// Quotes of any kind in the path never reach the script text.
	const odd = "C:\\it's \u2018odd\u2019.wav"
	_, args, env, _ := pickPlayer("windows", look("powershell"), odd)
	if strings.Contains(args[2], "odd") || !reflect.DeepEqual(env, []string{"WHATSCLI_SOUND=" + odd}) {
		t.Errorf("path leaked into the script or env wrong: %v %v", args, env)
	}
}

func TestPlayerErrorKeepsStderrTail(t *testing.T) {
	base := errors.New("exit status 1")
	if playerError(nil, []byte("noise")) != nil {
		t.Error("nil error must stay nil")
	}
	if got := playerError(base, []byte("  \n")); got != base {
		t.Errorf("blank stderr: got %v", got)
	}
	got := playerError(base, []byte("aplay: main: audio open error: Device busy\n"))
	if !errors.Is(got, base) || got.Error() != "exit status 1: aplay: main: audio open error: Device busy" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("x", 500) + "END"
	got = playerError(base, []byte(long))
	if tail := strings.TrimPrefix(got.Error(), "exit status 1: "); len(tail) != stderrTailBytes || !strings.HasSuffix(tail, "END") {
		t.Errorf("tail len = %d, %q", len(tail), tail)
	}
}

// runPlayer passes env to the process and reports its stderr. It runs sh,
// not an audio player.
func TestRunPlayerEnvAndStderr(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	err := runPlayer(context.Background(), []string{"WHATSCLI_SOUND=probe"}, "sh", "-c", `echo "got $WHATSCLI_SOUND" >&2; exit 3`)
	if err == nil || !strings.HasSuffix(err.Error(), "got probe") {
		t.Fatalf("err = %v", err)
	}
	if err := runPlayer(context.Background(), nil, "sh", "-c", "exit 0"); err != nil {
		t.Fatalf("success: %v", err)
	}
}

func newTestPlayer(run func(name string, args ...string) error, report func(error)) *soundPlayer {
	p := newSoundPlayer(report)
	p.pick = func(string) (string, []string, []string, bool) { return "fake", []string{"f"}, nil, true }
	p.run = func(_ context.Context, _ []string, name string, args ...string) error { return run(name, args...) }
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
	p.pick = func(string) (string, []string, []string, bool) { picks.Add(1); return "fake", nil, nil, true }
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

func TestPlayerPassesEnvToRun(t *testing.T) {
	var got []string
	p := newSoundPlayer(func(error) {})
	p.pick = func(string) (string, []string, []string, bool) {
		return "fake", []string{"-x"}, []string{"WHATSCLI_SOUND=f"}, true
	}
	p.run = func(_ context.Context, env []string, _ string, _ ...string) error { got = env; return nil }
	p.Play("f")
	p.wait()
	if !reflect.DeepEqual(got, []string{"WHATSCLI_SOUND=f"}) {
		t.Fatalf("env = %v", got)
	}
}
