// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func baseInput(now time.Time) notifyInput {
	return notifyInput{Enabled: true, MsgTime: now, Now: now, Focus: FocusUnfocused}
}

func TestDecide(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tests := []struct {
		name         string
		mod          func(*notifyInput)
		popup, sound bool
	}{
		{"disabled", func(i *notifyInput) { i.Enabled = false }, false, false},
		{"from me", func(i *notifyInput) { i.FromMe = true }, false, false},
		{"old message", func(i *notifyInput) { i.MsgTime = now.Add(-31 * time.Second) }, false, false},
		{"30 s is still fresh", func(i *notifyInput) { i.MsgTime = now.Add(-30 * time.Second) }, true, true},
		{"muted", func(i *notifyInput) { i.Muted = true }, false, false},
		{"unfocused, other chat", func(i *notifyInput) {}, true, true},
		{"unfocused, open chat", func(i *notifyInput) { i.IsOpenChat = true }, true, true},
		{"focused, other chat", func(i *notifyInput) { i.Focus = FocusFocused }, false, true},
		{"focused, open chat", func(i *notifyInput) { i.Focus = FocusFocused; i.IsOpenChat = true }, false, true},
		{"unknown, other chat", func(i *notifyInput) { i.Focus = FocusUnknown }, true, true},
		{"unknown, open chat", func(i *notifyInput) { i.Focus = FocusUnknown; i.IsOpenChat = true }, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput(now)
			tt.mod(&in)
			popup, sound := newNotifier(60*time.Second).decide("a", in)
			if popup != tt.popup || sound != tt.sound {
				t.Errorf("got (%v,%v), want (%v,%v)", popup, sound, tt.popup, tt.sound)
			}
		})
	}
}

func TestDecideThrottle(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	n := newNotifier(60 * time.Second)
	at := func(chat string, d time.Duration) (bool, bool) {
		in := baseInput(now.Add(d))
		return n.decide(chat, in)
	}
	check := func(label string, p, s, wp, ws bool) {
		t.Helper()
		if p != wp || s != ws {
			t.Errorf("%s: got (%v,%v), want (%v,%v)", label, p, s, wp, ws)
		}
	}
	p, s := at("a", 0)
	check("first", p, s, true, true)
	p, s = at("a", 59*time.Second)
	check("59s", p, s, false, false)
	p, s = at("a", 60*time.Second)
	check("60s", p, s, true, true)
	p, s = at("b", 61*time.Second)
	check("other chat", p, s, true, true)

	z := newNotifier(0)
	for i := 0; i < 3; i++ {
		p, s := z.decide("a", baseInput(now))
		check("zero timeout", p, s, true, true)
	}
}

func TestDecideSuppressedDoesNotStartWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	n := newNotifier(60 * time.Second)
	in := baseInput(now)
	in.Muted = true
	if p, s := n.decide("a", in); p || s {
		t.Fatal("muted should not notify")
	}
	in2 := baseInput(now.Add(time.Second))
	if p, s := n.decide("a", in2); !p || !s {
		t.Error("second message should notify")
	}
}

func TestDecideBurstAcrossChats(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	n := newNotifier(60 * time.Second)
	count := 0
	for i := 0; i < 20; i++ {
		p, s := n.decide(fmt.Sprintf("chat%d", i%5), baseInput(now))
		if p || s {
			count++
		}
	}
	if count != 5 {
		t.Errorf("got %d notifications, want 5", count)
	}
}

func TestNotifyTitle(t *testing.T) {
	tests := []struct {
		sender, group, want string
	}{
		{"Asha", "", "Asha"},
		{"Asha", "Family", "Asha · Family"},
		{"", "Family", "Family"},
		{"", "", "New message"},
	}
	for _, tt := range tests {
		if got := notifyTitle(tt.sender, tt.group); got != tt.want {
			t.Errorf("notifyTitle(%q, %q) = %q, want %q", tt.sender, tt.group, got, tt.want)
		}
	}
}

func TestNotifyBody(t *testing.T) {
	const goos = "darwin"
	long := strings.Repeat("x", 150)
	devanagari := strings.Repeat("क", 99)
	tests := []struct {
		name, text, preview, want string
	}{
		{"text", "hi", "", "hi"},
		{"preview fallback", "", "Photo", "Photo"},
		{"nothing", "", "", "New message"},
		{"newlines flattened", "a\nb\r\nc", "", "a b c"},
		{"devanagari unchanged", devanagari, "", devanagari},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notifyBody(tt.text, tt.preview, goos); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	got := []rune(notifyBody(long, "", goos))
	if len(got) != 101 {
		t.Fatalf("truncated length = %d runes, want 101", len(got))
	}
	if got[100] != '…' {
		t.Errorf("last rune = %q, want the ellipsis", got[100])
	}
	if string(got[:100]) != strings.Repeat("x", 100) {
		t.Errorf("kept text is wrong")
	}
}

func TestNotifyBodyEscapesMarkupOnLinux(t *testing.T) {
	const in = "<b>hi</b> & co"
	if got := notifyBody(in, "", "linux"); got != "&lt;b&gt;hi&lt;/b&gt; &amp; co" {
		t.Errorf("linux: got %q", got)
	}
	if got := notifyBody(in, "", "darwin"); got != in {
		t.Errorf("darwin: got %q, want unchanged", got)
	}
}

func TestTerminalFocusState(t *testing.T) {
	sm := &SessionManager{}
	if got := sm.terminalFocus(); got != FocusUnknown {
		t.Fatalf("new session manager focus = %v, want FocusUnknown", got)
	}
	sm.SetTerminalFocus(false)
	if got := sm.terminalFocus(); got != FocusUnfocused {
		t.Fatalf("after SetTerminalFocus(false) = %v, want FocusUnfocused", got)
	}
	sm.SetTerminalFocus(true)
	if got := sm.terminalFocus(); got != FocusFocused {
		t.Fatalf("after SetTerminalFocus(true) = %v, want FocusFocused", got)
	}
}
