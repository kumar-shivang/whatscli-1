// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PalWorks/whatscli/config"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// notifyRecorder records calls to the pop-up and sound hooks.
type notifyRecorder struct {
	mu       sync.Mutex
	popups   [][2]string
	popupErr error
	sounds   atomic.Int32
}

func (r *notifyRecorder) popupList() [][2]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][2]string(nil), r.popups...)
}

// notifyHarness returns a manager with notifications initialised, the hooks
// replaced by recorders, and the notification settings set to their defaults
// (restored when the test ends).
func notifyHarness(t *testing.T) (*SessionManager, *MockUiHandler, *notifyRecorder) {
	t.Helper()
	g := config.Config.General
	saved := *g
	t.Cleanup(func() { *g = saved })
	g.EnableNotifications = true
	g.UseTerminalBell = false
	g.PlaySound = true
	g.NotificationSound = ""
	g.NotificationTimeout = 60

	sm, ui := newTestSessionManager(t)
	sm.initNotifications()
	rec := &notifyRecorder{}
	sm.popupFunc = func(title, body string) error {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.popups = append(rec.popups, [2]string{title, body})
		return rec.popupErr
	}
	sm.soundFunc = func() { rec.sounds.Add(1) }
	return sm, ui, rec
}

// name returns a sender-name func for notifyIncoming.
func name(s string) func() string { return func() string { return s } }

var (
	notifyGroupJID = types.NewJID("120363000000000001", types.GroupServer)
	notifyUserJID  = types.NewJID("15550001111", types.DefaultUserServer)
)

func TestNotifyIncomingGroupUnfocused(t *testing.T) {
	sm, ui, rec := notifyHarness(t)
	sm.SetTerminalFocus(false)
	sm.notifyIncoming(notifyGroupJID, true, false, time.Now(), name("Asha"), "Family", "hi", "hi")
	sm.notifyWG.Wait()
	got := rec.popupList()
	if len(got) != 1 || got[0] != [2]string{"Asha · Family", "hi"} {
		t.Fatalf("popups = %q", got)
	}
	if n := rec.sounds.Load(); n != 1 {
		t.Fatalf("sounds = %d", n)
	}
	if ui.Beeps != 0 || len(ui.Errors) != 0 {
		t.Fatalf("beeps=%d errors=%v", ui.Beeps, ui.Errors)
	}
}

func TestNotifyIncomingOpenChatUnknownFocus(t *testing.T) {
	sm, ui, rec := notifyHarness(t)
	sm.currentReceiver = notifyUserJID.String()
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if len(rec.popupList()) != 0 || rec.sounds.Load() != 0 || ui.Beeps != 0 {
		t.Fatalf("popups=%q sounds=%d beeps=%d", rec.popupList(), rec.sounds.Load(), ui.Beeps)
	}
	// Another chat still notifies with focus unknown (today's behaviour).
	other := types.NewJID("15550002222", types.DefaultUserServer)
	sm.notifyIncoming(other, false, false, time.Now(), name("Cara"), "Cara", "yo", "yo")
	sm.notifyWG.Wait()
	if got := rec.popupList(); len(got) != 1 || got[0] != [2]string{"Cara", "yo"} {
		t.Fatalf("other chat popups = %q", got)
	}
}

func TestNotifyIncomingMuted(t *testing.T) {
	sm, ui, rec := notifyHarness(t)
	sm.SetTerminalFocus(false)
	sm.setMutedUntil(notifyUserJID, store.MutedForever)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if len(rec.popupList()) != 0 || rec.sounds.Load() != 0 || ui.Beeps != 0 {
		t.Fatalf("popups=%q sounds=%d beeps=%d", rec.popupList(), rec.sounds.Load(), ui.Beeps)
	}
}

func TestNotifyIncomingFocusedSoundOnly(t *testing.T) {
	sm, _, rec := notifyHarness(t)
	sm.SetTerminalFocus(true)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if len(rec.popupList()) != 0 || rec.sounds.Load() != 1 {
		t.Fatalf("popups=%q sounds=%d", rec.popupList(), rec.sounds.Load())
	}
}

func TestNotifyIncomingBellSetting(t *testing.T) {
	sm, ui, rec := notifyHarness(t)
	config.Config.General.UseTerminalBell = true
	sm.SetTerminalFocus(false)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if ui.Beeps != 1 || rec.sounds.Load() != 0 {
		t.Fatalf("beeps=%d sounds=%d", ui.Beeps, rec.sounds.Load())
	}
	if len(rec.popupList()) != 1 {
		t.Fatalf("bell setting must keep the pop-up: %q", rec.popupList())
	}

	// play_sound = false silences the bell too; the pop-up still shows.
	config.Config.General.PlaySound = false
	other := types.NewJID("15550002222", types.DefaultUserServer)
	sm.notifyIncoming(other, false, false, time.Now(), name("Cara"), "Cara", "yo", "yo")
	sm.notifyWG.Wait()
	if ui.Beeps != 1 || rec.sounds.Load() != 0 {
		t.Fatalf("play_sound=false: beeps=%d sounds=%d", ui.Beeps, rec.sounds.Load())
	}
	if len(rec.popupList()) != 2 {
		t.Fatalf("play_sound=false must keep the pop-up: %q", rec.popupList())
	}
}

func TestNotifyIncomingPopupErrorReported(t *testing.T) {
	sm, ui, rec := notifyHarness(t)
	rec.popupErr = errors.New("no daemon")
	sm.SetTerminalFocus(false)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if len(ui.Errors) != 1 || !errors.Is(ui.Errors[0], rec.popupErr) {
		t.Fatalf("errors = %v", ui.Errors)
	}
	if !strings.Contains(ui.Errors[0].Error(), popupErrorHint) {
		t.Errorf("error %q lacks the hint", ui.Errors[0])
	}
}

func TestNotifyIncomingPopupErrorReportedOnce(t *testing.T) {
	sm, ui, rec := notifyHarness(t)
	rec.popupErr = errors.New("no daemon")
	sm.SetTerminalFocus(false)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	other := types.NewJID("15550002222", types.DefaultUserServer)
	sm.notifyIncoming(other, false, false, time.Now(), name("Cara"), "Cara", "yo", "yo")
	sm.notifyWG.Wait()
	if n := len(rec.popupList()); n != 2 {
		t.Fatalf("popups tried = %d, want 2", n)
	}
	if len(ui.Errors) != 1 {
		t.Fatalf("errors = %v, want only the first", ui.Errors)
	}
}

func TestNotifyIncomingOnlyPersonalAndGroupChats(t *testing.T) {
	tests := []struct {
		jid    types.JID
		notify bool
	}{
		{types.StatusBroadcastJID, false},
		{types.NewJID("120363000000000009", types.NewsletterServer), false},
		{types.NewJID("1700000000", types.BroadcastServer), false},
		{notifyUserJID, true},
		{types.NewJID("123456789012345", types.HiddenUserServer), true},
		{notifyGroupJID, true},
	}
	for _, tt := range tests {
		sm, _, rec := notifyHarness(t)
		sm.SetTerminalFocus(false)
		called := false
		sm.notifyIncoming(tt.jid, tt.jid.Server == types.GroupServer, false, time.Now(),
			func() string { called = true; return "Bob" }, "Chat", "hi", "hi")
		sm.notifyWG.Wait()
		got := len(rec.popupList()) == 1 && rec.sounds.Load() == 1
		if got != tt.notify {
			t.Errorf("%s: notified = %v, want %v", tt.jid, got, tt.notify)
		}
		if called != tt.notify {
			t.Errorf("%s: sender name looked up = %v, want %v", tt.jid, called, tt.notify)
		}
	}
}

func TestNotifyIncomingSenderNameOnlyForPopup(t *testing.T) {
	sm, _, rec := notifyHarness(t)
	calls := 0
	fn := func() string { calls++; return "Bob" }
	// Own and old messages, and the focused (sound only) case, never look
	// the sender up.
	sm.notifyIncoming(notifyUserJID, false, true, time.Now(), fn, "Bob", "hi", "hi")
	sm.notifyIncoming(notifyUserJID, false, false, time.Now().Add(-time.Hour), fn, "Bob", "hi", "hi")
	sm.SetTerminalFocus(true)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), fn, "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if calls != 0 || rec.sounds.Load() != 1 {
		t.Fatalf("calls=%d sounds=%d", calls, rec.sounds.Load())
	}
	sm.SetTerminalFocus(false)
	other := types.NewJID("15550002222", types.DefaultUserServer)
	sm.notifyIncoming(other, false, false, time.Now(), fn, "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 for the pop-up", calls)
	}
}

func TestReactionsNeverNotify(t *testing.T) {
	sm, _, rec := notifyHarness(t)
	eh := &eventHandler{sm: sm}
	sm.SetTerminalFocus(false)
	src := types.MessageSource{Chat: notifyUserJID, Sender: notifyUserJID}
	key := &waCommon.MessageKey{ID: proto.String("X1")}
	for i, txt := range []string{"\U0001F44D", ""} { // added, then removed
		eh.handleMessage(&events.Message{
			Info: types.MessageInfo{ID: types.MessageID("R" + string(rune('0'+i))), MessageSource: src, Timestamp: time.Now()},
			Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
				Key: key, Text: proto.String(txt),
			}},
		})
	}
	sm.notifyWG.Wait()
	if len(rec.popupList()) != 0 || rec.sounds.Load() != 0 {
		t.Fatalf("reaction notified: popups=%q sounds=%d", rec.popupList(), rec.sounds.Load())
	}
	eh.handleMessage(&events.Message{
		Info:    types.MessageInfo{ID: "T1", MessageSource: src, Timestamp: time.Now()},
		Message: &waE2E.Message{Conversation: proto.String("hello")},
	})
	sm.notifyWG.Wait()
	if got := rec.popupList(); len(got) != 1 || got[0][1] != "hello" {
		t.Fatalf("text after reactions: popups = %q", got)
	}
}

func TestNotifyIncomingNotInitialised(t *testing.T) {
	// Managers built without Init (most tests) never notify.
	sm, ui := newTestSessionManager(t)
	sm.notifyIncoming(notifyUserJID, false, false, time.Now(), name("Bob"), "Bob", "hi", "hi")
	sm.notifyWG.Wait()
	if ui.Beeps != 0 || len(ui.Errors) != 0 {
		t.Fatalf("beeps=%d errors=%v", ui.Beeps, ui.Errors)
	}
}

func TestProcessIncomingMessageNotifies(t *testing.T) {
	sm, _, rec := notifyHarness(t)
	eh := &eventHandler{sm: sm}
	sm.SetTerminalFocus(false)
	src := types.MessageSource{Chat: notifyUserJID, Sender: notifyUserJID}
	eh.handleMessage(&events.Message{
		Info:    types.MessageInfo{ID: "N1", MessageSource: src, Timestamp: time.Now()},
		Message: &waE2E.Message{Conversation: proto.String("hello")},
	})
	sm.notifyWG.Wait()
	got := rec.popupList()
	if len(got) != 1 || got[0][1] != "hello" {
		t.Fatalf("popups = %q", got)
	}
	// Our own message (from another device) never notifies.
	src.IsFromMe = true
	eh.handleMessage(&events.Message{
		Info:    types.MessageInfo{ID: "N2", MessageSource: src, Timestamp: time.Now()},
		Message: &waE2E.Message{Conversation: proto.String("mine")},
	})
	sm.notifyWG.Wait()
	if len(rec.popupList()) != 1 || rec.sounds.Load() != 1 {
		t.Fatalf("own message notified: popups=%q sounds=%d", rec.popupList(), rec.sounds.Load())
	}
}

func TestPlayNotifySoundFallsBackToBell(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	g := config.Config.General
	saved := *g
	t.Cleanup(func() { *g = saved })
	g.NotificationSound = ""

	sm, ui := newTestSessionManager(t)
	sm.initNotifications()
	var picks atomic.Int32
	sm.sound.pick = func(string) (string, []string, []string, bool) { picks.Add(1); return "", nil, nil, false }
	sm.sound.run = func(context.Context, []string, string, ...string) error {
		t.Error("player run although none was found")
		return nil
	}
	sm.playNotifySound()
	sm.playNotifySound()
	sm.sound.wait()
	if ui.Beeps != 2 || len(ui.Errors) != 0 {
		t.Fatalf("beeps=%d errors=%v", ui.Beeps, ui.Errors)
	}
	if picks.Load() != 1 {
		t.Fatalf("player looked up %d times, want once", picks.Load())
	}
}

func TestPlayNotifySoundUsesPlayer(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	g := config.Config.General
	saved := *g
	t.Cleanup(func() { *g = saved })
	g.NotificationSound = "/nonexistent/custom.wav"

	sm, ui := newTestSessionManager(t)
	sm.initNotifications()
	sm.sound.pick = func(f string) (string, []string, []string, bool) { return "fake", []string{f}, nil, true }
	var ran atomic.Int32
	sm.sound.run = func(context.Context, []string, string, ...string) error { ran.Add(1); return nil }
	sm.playNotifySound()
	sm.sound.wait()
	sm.playNotifySound()
	sm.sound.wait()
	if ran.Load() != 2 || ui.Beeps != 0 {
		t.Fatalf("ran=%d beeps=%d", ran.Load(), ui.Beeps)
	}
	// The bad custom path is reported once, not per sound.
	if len(ui.Errors) != 1 {
		t.Fatalf("errors = %v", ui.Errors)
	}
}
