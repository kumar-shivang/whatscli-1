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
	// block, when set, is received from before the lookup returns.
	block chan struct{}
	// started is closed when a blocked lookup begins.
	started chan struct{}
	// errs are returned (one per call) before the map is consulted.
	errs []error
}

func (f *fakeChatSettings) GetChatSettings(_ context.Context, chat types.JID) (types.LocalChatSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.block != nil {
		close(f.started)
		blk := f.block
		f.mu.Unlock()
		<-blk
		f.mu.Lock()
	}
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
		{"muted no end", muteEvt(a, proto.Bool(true), nil), time.UnixMilli(0)},
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

func TestHandleMuteEmptyJIDIgnored(t *testing.T) {
	sm, mock := newTestSessionManager(t)
	eh := &eventHandler{sm: sm}
	eh.handleMute(muteEvt(types.EmptyJID, proto.Bool(true), proto.Int64(-1)))
	if len(mock.ChatLists) != 0 {
		t.Error("empty JID must be ignored")
	}
}

func TestMutedUntilLazyFillDoesNotOverwriteNewerWrite(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	a := pnJID("alice")
	fs := &fakeChatSettings{
		until:   map[string]time.Time{a.String(): time.Time{}},
		block:   make(chan struct{}),
		started: make(chan struct{}),
	}
	sm.chatSettingsOverride = fs
	res := make(chan time.Time)
	go func() { res <- sm.mutedUntil(a) }()
	<-fs.started
	newer := time.Now().Add(time.Hour)
	sm.setMutedUntil(a, newer)
	close(fs.block)
	if got := <-res; !got.Equal(newer) {
		t.Errorf("mutedUntil = %v, want newer %v", got, newer)
	}
	if got := sm.mutedUntil(a); !got.Equal(newer) {
		t.Errorf("cache = %v, want %v", got, newer)
	}
}

func TestIsMutedCachedNeverReadsStore(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	a := pnJID("alice")
	fs := &fakeChatSettings{until: map[string]time.Time{a.String(): store.MutedForever}}
	sm.chatSettingsOverride = fs
	if sm.IsMutedCached(a.String()) {
		t.Error("cache miss must read as not muted")
	}
	if fs.callCount() != 0 {
		t.Errorf("store calls = %d, want 0", fs.callCount())
	}
	done := make(chan struct{})
	sm.WarmMuteCache([]string{a.String()}, func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("warm-up did not finish")
	}
	if !sm.IsMutedCached(a.String()) {
		t.Error("muted chat should read as muted after warm-up")
	}
	calls := fs.callCount()
	sm.WarmMuteCache([]string{a.String()}, func() { t.Error("done called with nothing to fill") })
	if sm.IsMutedCached("not a jid") || fs.callCount() != calls {
		t.Error("unparsable ID or second warm-up misbehaved")
	}
}

// A full app-state sync stores mutes without events.Mute; the completion
// event must drop the cached "not muted" so the store is read again.
func TestAppStateSyncCompleteRefreshesMutes(t *testing.T) {
	sm, mock := newTestSessionManager(t)
	a := pnJID("alice")
	fs := &fakeChatSettings{until: map[string]time.Time{}}
	sm.chatSettingsOverride = fs
	if sm.IsMuted(a.String()) {
		t.Fatal("not muted before the sync")
	}
	// The sync lands in the store.
	fs.mu.Lock()
	fs.until[a.String()] = store.MutedForever
	fs.mu.Unlock()
	if sm.IsMuted(a.String()) {
		t.Fatal("cache should still say not muted before the event")
	}
	eh := &eventHandler{sm: sm}
	eh.Handle(&events.AppStateSyncComplete{Name: "regular_high", Version: 3})
	if len(mock.ChatLists) != 1 {
		t.Errorf("UpdateChatList calls = %d, want 1", len(mock.ChatLists))
	}
	if sm.IsMutedCached(a.String()) {
		t.Error("cache should be empty after the event")
	}
	if !sm.mutedUntil(a).Equal(store.MutedForever) || !sm.IsMuted(a.String()) {
		t.Errorf("after event: mutedUntil = %v, want forever", sm.mutedUntil(a))
	}
}

// staleSettings answers "not muted", then waits on release before returning,
// standing in for a lookup that read the store just before a full sync.
type staleSettings struct {
	started chan struct{}
	release chan struct{}
}

func (s *staleSettings) GetChatSettings(context.Context, types.JID) (types.LocalChatSettings, error) {
	close(s.started)
	<-s.release
	return types.LocalChatSettings{}, nil
}

// A store read that started before the reset must not be cached after it.
func TestMuteFillBeforeResetNotCached(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	a := pnJID("alice")
	st := &staleSettings{started: make(chan struct{}), release: make(chan struct{})}
	sm.chatSettingsOverride = st
	res := make(chan time.Time, 1)
	go func() { res <- sm.mutedUntil(a) }()
	<-st.started
	sm.muteCache.reset()
	close(st.release)
	select {
	case <-res:
	case <-time.After(5 * time.Second):
		t.Fatal("lookup did not finish")
	}
	if _, ok := sm.muteCache.get(a.String()); ok {
		t.Fatal("a fill read before the reset was cached")
	}
}
