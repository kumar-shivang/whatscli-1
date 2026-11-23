// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// fakeChatSettings is a chatSettingsStore that counts calls.
type fakeChatSettings struct {
	mu    sync.Mutex
	until map[string]time.Time
	calls int
	// errs are returned (one per call) before the map is consulted.
	errs []error
}

func (f *fakeChatSettings) GetChatSettings(_ context.Context, chat types.JID) (types.LocalChatSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return types.LocalChatSettings{}, err
		}
	}
	u, ok := f.until[chat.String()]
	return types.LocalChatSettings{Found: ok, MutedUntil: u}, nil
}

func (f *fakeChatSettings) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestIsMutedAt(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		until time.Time
		want  bool
	}{
		{"zero", time.Time{}, false},
		{"forever", store.MutedForever, true},
		{"future", now.Add(time.Second), true},
		{"past", now.Add(-time.Second), false},
	}
	for _, tt := range tests {
		if got := isMutedAt(tt.until, now); got != tt.want {
			t.Errorf("%s: isMutedAt = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMutedUntilReadsStoreOnceAndCaches(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	a := pnJID("alice")
	until := time.Now().Add(time.Hour)
	fs := &fakeChatSettings{until: map[string]time.Time{a.String(): until}}
	sm.chatSettingsOverride = fs
	for i := 0; i < 2; i++ {
		if got := sm.mutedUntil(a); !got.Equal(until) {
			t.Fatalf("call %d: got %v, want %v", i, got, until)
		}
	}
	if fs.callCount() != 1 {
		t.Errorf("store calls = %d, want 1", fs.callCount())
	}
}

func TestMutedUntilChecksLIDAndPN(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	sm.lidMapperOverride = fakeLIDs{"123": "456"}
	until := time.Now().Add(time.Hour)
	sm.chatSettingsOverride = &fakeChatSettings{until: map[string]time.Time{lidJID("123").String(): until}}
	if got := sm.mutedUntil(pnJID("456")); !got.Equal(until) {
		t.Errorf("got %v, want %v", got, until)
	}
}

func TestMutedUntilStoreErrorNotCached(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	a := pnJID("alice")
	sm.chatSettingsOverride = &fakeChatSettings{
		errs:  []error{errors.New("boom")},
		until: map[string]time.Time{a.String(): store.MutedForever},
	}
	if got := sm.mutedUntil(a); !got.IsZero() {
		t.Errorf("first call = %v, want zero", got)
	}
	if got := sm.mutedUntil(a); !got.Equal(store.MutedForever) {
		t.Errorf("second call = %v, want forever", got)
	}
}

func muteEvt(jid types.JID, muted *bool, end *int64) *events.Mute {
	return &events.Mute{JID: jid, Action: &waSyncAction.MuteAction{Muted: muted, MuteEndTimestamp: end}}
}

func TestHandleMuteEvent(t *testing.T) {
	sm, mock := newTestSessionManager(t)
	sm.chatSettingsOverride = &fakeChatSettings{}
	eh := &eventHandler{sm: sm}
	a := pnJID("alice")
	endMs := time.Now().Add(2 * time.Hour).UnixMilli()

	tests := []struct {
		name string
		evt  *events.Mute
		want time.Time
	}{
		{"muted with end", muteEvt(a, proto.Bool(true), proto.Int64(endMs)), time.UnixMilli(endMs)},
		{"muted -1", muteEvt(a, proto.Bool(true), proto.Int64(-1)), store.MutedForever},
		{"muted no end", muteEvt(a, proto.Bool(true), nil), store.MutedForever},
		{"unmuted", muteEvt(a, proto.Bool(false), nil), time.Time{}},
	}
	for i, tt := range tests {
		eh.handleMute(tt.evt)
		if got := sm.mutedUntil(a); !got.Equal(tt.want) {
			t.Errorf("%s: mutedUntil = %v, want %v", tt.name, got, tt.want)
		}
		if len(mock.ChatLists) != i+1 {
			t.Errorf("%s: UpdateChatList calls = %d, want %d", tt.name, len(mock.ChatLists), i+1)
		}
	}
}

func TestMuteExpiresWithoutRestart(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	fs := &fakeChatSettings{}
	sm.chatSettingsOverride = fs
	a := pnJID("alice")
	now := time.Now()
	sm.setMutedUntil(a, now.Add(time.Hour))
	if !isMutedAt(sm.mutedUntil(a), now) {
		t.Error("want muted now")
	}
	if isMutedAt(sm.mutedUntil(a), now.Add(time.Hour+time.Second)) {
		t.Error("want unmuted after expiry")
	}
	if fs.callCount() != 0 {
		t.Errorf("store calls = %d, want 0", fs.callCount())
	}
}

func TestIsMutedBadID(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	if sm.IsMuted("not a jid") {
		t.Error("unparsable ID must not be muted")
	}
}
