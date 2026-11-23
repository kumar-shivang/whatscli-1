// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:generate go run ../tools/gen-notify-sound

//go:embed sound/notify.wav
var builtinSound []byte

// soundPlayTimeout bounds how long one playback may run.
const soundPlayTimeout = 5 * time.Second

// soundFile resolves the sound to play. With an empty custom it materialises
// the built-in sound under <cacheDir>/whatscli/notify.wav. A custom path that
// is a regular file is returned (with a leading ~ expanded); otherwise the
// built-in path is returned along with a warning naming the bad path.
func soundFile(custom, cacheDir string) (string, error) {
	if custom != "" {
		p := custom
		if p == "~" || strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[1:])
			}
		}
		st, err := os.Stat(p)
		if err == nil && st.Mode().IsRegular() {
			return p, nil
		}
		path, werr := writeBuiltinSound(cacheDir)
		warn := fmt.Errorf("notification sound %q is not a usable file; using the built-in sound", custom)
		if werr != nil {
			warn = fmt.Errorf("%w (%v)", warn, werr)
		}
		return path, warn
	}
	path, err := writeBuiltinSound(cacheDir)
	return path, err
}

// writeBuiltinSound writes the embedded sound to the cache if it is missing
// or different, and returns its path.
func writeBuiltinSound(cacheDir string) (string, error) {
	dir := filepath.Join(cacheDir, "whatscli")
	path := filepath.Join(dir, "notify.wav")
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, builtinSound) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return path, err
	}
	return path, os.WriteFile(path, builtinSound, 0o644)
}

// pickPlayer chooses a system audio player for goos, using lookPath to find
// binaries. ok is false when none is available.
func pickPlayer(goos string, lookPath func(string) (string, error), file string) (cmd string, args []string, ok bool) {
	has := func(n string) bool { _, err := lookPath(n); return err == nil }
	switch goos {
	case "darwin":
		if has("afplay") {
			return "afplay", []string{file}, true
		}
	case "windows":
		if has("powershell") {
			q := strings.ReplaceAll(file, "'", "''")
			return "powershell", []string{"-NoProfile", "-Command",
				"(New-Object Media.SoundPlayer '" + q + "').PlaySync()"}, true
		}
	default:
		if has("pw-play") {
			return "pw-play", []string{file}, true
		}
		if has("paplay") {
			return "paplay", []string{file}, true
		}
		if has("aplay") {
			return "aplay", []string{"-q", file}, true
		}
	}
	return "", nil, false
}

// soundPlayer plays notification sounds one at a time via a system player.
type soundPlayer struct {
	report func(error)
	run    func(ctx context.Context, name string, args ...string) error
	pick   func(file string) (string, []string, bool)

	mu       sync.Mutex
	playing  bool
	reported bool
	wg       sync.WaitGroup
}

// newSoundPlayer returns a player that hands the first playback error to
// report; later errors are silent.
func newSoundPlayer(report func(error)) *soundPlayer {
	return &soundPlayer{
		report: report,
		run: func(ctx context.Context, name string, args ...string) error {
			return exec.CommandContext(ctx, name, args...).Run()
		},
		pick: func(file string) (string, []string, bool) {
			return pickPlayer(runtime.GOOS, exec.LookPath, file)
		},
	}
}

// Play plays file without blocking. A call made while a sound is already
// playing is dropped.
func (p *soundPlayer) Play(file string) {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return
	}
	p.playing = true
	p.wg.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wg.Done()
		err := p.play(file)
		p.mu.Lock()
		p.playing = false
		first := err != nil && !p.reported
		if first {
			p.reported = true
		}
		p.mu.Unlock()
		if first && p.report != nil {
			p.report(err)
		}
	}()
}

func (p *soundPlayer) play(file string) error {
	cmd, args, ok := p.pick(file)
	if !ok {
		return fmt.Errorf("no audio player found for notification sound")
	}
	ctx, cancel := context.WithTimeout(context.Background(), soundPlayTimeout)
	defer cancel()
	return p.run(ctx, cmd, args...)
}

// wait blocks until in-flight playback finishes (used by tests).
func (p *soundPlayer) wait() { p.wg.Wait() }
