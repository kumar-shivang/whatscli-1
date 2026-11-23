// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"strings"
	"sync"
	"time"
)

// FocusState is whether the terminal window currently has focus.
type FocusState int32

const (
	// FocusUnknown means the terminal has not reported focus since start.
	FocusUnknown FocusState = iota
	// FocusFocused means the terminal reported it has focus.
	FocusFocused
	// FocusUnfocused means the terminal reported it lost focus.
	FocusUnfocused
)

// SetTerminalFocus records whether the terminal window has focus. The UI
// calls it from its event-reading goroutine when the terminal reports a
// focus change; it is safe for concurrent use.
func (sm *SessionManager) SetTerminalFocus(focused bool) {
	state := FocusUnfocused
	if focused {
		state = FocusFocused
	}
	sm.focus.Store(int32(state))
}

// terminalFocus returns the last focus state the terminal reported, or
// FocusUnknown if it has reported none.
func (sm *SessionManager) terminalFocus() FocusState {
	return FocusState(sm.focus.Load())
}

// maxNotifyAge is how old a message may be and still notify. It stops
// history-sync bursts from producing notifications.
const maxNotifyAge = 30 * time.Second

// notifyInput is everything decide needs to know about one incoming message.
type notifyInput struct {
	Enabled    bool
	FromMe     bool
	Muted      bool
	IsOpenChat bool
	MsgTime    time.Time
	Now        time.Time
	Focus      FocusState
}

// notifier decides whether an incoming message produces a pop-up and/or a
// sound, and throttles per chat. It is safe for concurrent use.
type notifier struct {
	timeout time.Duration
	mu      sync.Mutex
	last    map[string]time.Time
}

// newNotifier returns a notifier that allows one notification per chat per
// timeout. A timeout of 0 disables the throttle.
func newNotifier(timeout time.Duration) *notifier {
	return &notifier{timeout: timeout, last: make(map[string]time.Time)}
}

// decide applies the notification rules in order. Only a call that returns a
// notification records the time for the chat's throttle window.
func (n *notifier) decide(chat string, in notifyInput) (popup, sound bool) {
	if !in.Enabled || in.FromMe || in.Now.Sub(in.MsgTime) > maxNotifyAge || in.Muted {
		return false, false
	}

	// Focus rules decide what would be shown; the throttle only applies when
	// something would be.
	switch in.Focus {
	case FocusUnfocused:
		popup, sound = true, true
	case FocusFocused:
		popup, sound = false, true
	default: // FocusUnknown: keep the historical behaviour.
		if in.IsOpenChat {
			return false, false
		}
		popup, sound = true, true
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.timeout > 0 {
		if t, ok := n.last[chat]; ok && in.Now.Sub(t) < n.timeout {
			return false, false
		}
	}
	n.last[chat] = in.Now
	return popup, sound
}

// maxBodyRunes is how many runes of message text a pop-up body keeps before
// it is cut and marked with an ellipsis.
const maxBodyRunes = 100

// notifyTitle returns the pop-up title: the sender, the group, or both joined
// with a middle dot. It returns "New message" when both are empty.
func notifyTitle(sender, group string) string {
	switch {
	case sender != "" && group != "":
		return sender + " \u00b7 " + group
	case sender != "":
		return sender
	case group != "":
		return group
	default:
		return "New message"
	}
}

// notifyBody returns the pop-up body for a message. It uses text, or preview
// (for example "Photo") when text is blank, and "New message" when both are
// empty. Line breaks become spaces, the text is cut to maxBodyRunes runes with
// an ellipsis, and on linux (where the notification daemon renders markup)
// &, < and > are escaped. Quotes are left alone because daemons would show
// &#34; literally.
func notifyBody(text, preview, goos string) string {
	body := text
	if strings.TrimSpace(body) == "" {
		body = preview
	}
	if strings.TrimSpace(body) == "" {
		return "New message"
	}
	body = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(body)
	if r := []rune(body); len(r) > maxBodyRunes {
		body = string(r[:maxBodyRunes]) + "\u2026"
	}
	if goos == "linux" {
		body = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(body)
	}
	return body
}
