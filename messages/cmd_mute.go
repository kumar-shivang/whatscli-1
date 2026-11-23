// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// muteSendTimeout bounds the app-state patch sent by /mute and /unmute.
const muteSendTimeout = 15 * time.Second

// parseMuteDuration parses a /mute argument. The empty string means forever
// (0). It accepts Go durations plus whole-number Nd (days) and Nw (weeks);
// zero, negative and malformed values are errors naming the input.
func parseMuteDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	bad := fmt.Errorf("invalid mute duration %q: use e.g. 30m, 8h, 1d or 1w", s)
	var d time.Duration
	if n := len(s); n > 1 && (s[n-1] == 'd' || s[n-1] == 'w') {
		v, err := strconv.ParseUint(s[:n-1], 10, 32)
		if err != nil {
			return 0, bad
		}
		unit := 24 * time.Hour
		if s[n-1] == 'w' {
			unit = 7 * 24 * time.Hour
		}
		d = time.Duration(v) * unit
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, bad
		}
	}
	if d <= 0 {
		return 0, bad
	}
	return d, nil
}

// cmdMute mutes the open chat for [duration] (forever when omitted), here and
// on the user's phone: /mute [duration].
func cmdMute(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	if len(params) > 1 {
		sm.printCommandUsage(cmdName, "[duration]")
		return
	}
	arg := ""
	if len(params) == 1 {
		arg = params[0]
	}
	d, err := parseMuteDuration(arg)
	if err != nil {
		sm.printCommandUsage(cmdName, "[duration]")
		return
	}
	sm.setMute(client, true, d)
}

// cmdUnmute unmutes the open chat: /unmute.
func cmdUnmute(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	if len(params) != 0 {
		sm.printCommandUsage(cmdName, "")
		return
	}
	sm.setMute(client, false, 0)
}

// setMute sends a mute (d == 0: forever) or unmute patch for the open chat in
// a goroutine, and on success updates the mute cache and redraws the chat list.
func (sm *SessionManager) setMute(client *whatsmeow.Client, mute bool, d time.Duration) {
	sm.mu.RLock()
	receiver := sm.currentReceiver
	sm.mu.RUnlock()
	if receiver == "" {
		sm.uiHandler.PrintError(errors.New("open a chat first"))
		return
	}
	jid, err := types.ParseJID(receiver)
	if err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("invalid JID: %v", err))
		return
	}
	if client == nil || (sm.appStateSender == nil && !client.IsConnected()) {
		sm.uiHandler.PrintError(errors.New("not connected to WhatsApp: try again once connected"))
		return
	}
	patch := appstate.BuildMute(jid, mute, d)
	go func() {
		if sm.muteDone != nil {
			defer sm.muteDone()
		}
		ctx, cancel := context.WithTimeout(sm.rootContext(), muteSendTimeout)
		defer cancel()
		send := sm.appStateSender
		if send == nil {
			send = client.SendAppState
		}
		if err := send(ctx, patch); err != nil {
			sm.uiHandler.PrintError(fmt.Errorf("could not update mute: %v", err))
			return
		}
		var until time.Time
		msg := "Chat unmuted"
		switch {
		case mute && d == 0:
			until = store.MutedForever
			msg = "Chat muted: always"
		case mute:
			until = time.Now().Add(d)
			msg = "Chat muted until " + until.Format("2006-01-02 15:04")
		}
		sm.setMutedUntil(jid, until)
		sm.mu.RLock()
		safeList := sm.snapshotPQ()
		sm.mu.RUnlock()
		sm.uiHandler.UpdateChatList(safeList)
		sm.uiHandler.PrintText(msg)
	}()
}
