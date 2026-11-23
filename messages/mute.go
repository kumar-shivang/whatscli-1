// SPDX-License-Identifier: AGPL-3.0-only

package messages

// mute.go: per-chat mute state. whatsmeow's ChatSettings store is the source
// of truth (it records every mute, including those from a full app-state
// sync). Lookups are cached in memory, keyed by canonical chat JID, and kept
// current by events.Mute. The cache has its own mutex, never sm.mu.

import (
	"context"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// chatSettingsStore is the subset of whatsmeow's store.ChatSettingsStore used
// here, so tests can supply a fake.
type chatSettingsStore interface {
	GetChatSettings(ctx context.Context, chat types.JID) (types.LocalChatSettings, error)
}

// muteCache holds mute-until times by canonical chat JID. The zero value is
// ready to use. A zero time means "known, not muted".
type muteCache struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (c *muteCache) get(key string) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.m[key]
	return t, ok
}

func (c *muteCache) set(key string, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]time.Time)
	}
	c.m[key] = t
}

// chatSettings returns the mute source: the test override if set, otherwise
// the connected client's store, or nil when neither is available.
func (sm *SessionManager) chatSettings() chatSettingsStore {
	if sm.chatSettingsOverride != nil {
		return sm.chatSettingsOverride
	}
	client := sm.getClient()
	if client == nil || client.Store == nil || client.Store.ChatSettings == nil {
		return nil
	}
	return client.Store.ChatSettings
}

// isMutedAt reports whether a mute ending at until is in effect at now.
// The zero time means not muted.
func isMutedAt(until, now time.Time) bool {
	return until.Equal(store.MutedForever) || until.After(now)
}

// mutedUntil returns when the chat's mute ends (zero when not muted). The
// result is cached under the canonical JID; a store error counts as not muted
// and is not cached. For 1:1 chats the LID and phone-number forms are both
// checked and the later end wins.
func (sm *SessionManager) mutedUntil(chat types.JID) time.Time {
	canon := sm.canonicalChatJID(chat)
	key := canon.String()
	if t, ok := sm.muteCache.get(key); ok {
		return t
	}
	st := sm.chatSettings()
	if st == nil {
		return time.Time{}
	}
	candidates := []types.JID{canon}
	if m := sm.lidMap(); m != nil && (canon.Server == types.DefaultUserServer || canon.Server == types.HiddenUserServer) {
		ctx, cancel := context.WithTimeout(context.Background(), lidLookupTimeout)
		var other types.JID
		var err error
		if canon.Server == types.DefaultUserServer {
			other, err = m.GetLIDForPN(ctx, canon)
		} else {
			other, err = m.GetPNForLID(ctx, canon)
		}
		cancel()
		if err == nil && !other.IsEmpty() {
			candidates = append(candidates, other.ToNonAD())
		}
	}
	var best time.Time
	for _, c := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), lidLookupTimeout)
		cs, err := st.GetChatSettings(ctx, c)
		cancel()
		if err != nil {
			return time.Time{}
		}
		if cs.Found && cs.MutedUntil.After(best) {
			best = cs.MutedUntil
		}
	}
	sm.muteCache.set(key, best)
	return best
}

// setMutedUntil records a chat's mute end (zero to unmute) under its
// canonical JID.
func (sm *SessionManager) setMutedUntil(chat types.JID, until time.Time) {
	sm.muteCache.set(sm.canonicalChatJID(chat).String(), until)
}

// IsMuted reports whether the chat with the given JID string is muted now.
// An unparsable ID is not muted.
func (sm *SessionManager) IsMuted(chatID string) bool {
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return false
	}
	return isMutedAt(sm.mutedUntil(jid), time.Now())
}

// handleMute applies a mute/unmute app-state event and redraws the chat list.
func (eh *eventHandler) handleMute(evt *events.Mute) {
	var until time.Time
	if a := evt.Action; a != nil && a.GetMuted() {
		end := a.GetMuteEndTimestamp()
		if end < 0 || a.MuteEndTimestamp == nil {
			until = store.MutedForever
		} else {
			until = time.UnixMilli(end)
		}
	}
	eh.sm.setMutedUntil(evt.JID, until)

	eh.sm.mu.RLock()
	safeList := eh.sm.snapshotPQ()
	eh.sm.mu.RUnlock()
	eh.sm.uiHandler.UpdateChatList(safeList)
}
