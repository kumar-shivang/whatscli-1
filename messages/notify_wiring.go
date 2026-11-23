// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/PalWorks/whatscli/config"
	"github.com/gen2brain/beeep"
	"go.mau.fi/whatsmeow/types"
)

// initNotifications sets up the notifier (with the configured throttle), the
// sound player and the default pop-up and sound hooks. Init calls it; tests
// call it and then replace popupFunc and soundFunc.
func (sm *SessionManager) initNotifications() {
	timeout := time.Duration(config.Config.General.NotificationTimeout) * time.Second
	sm.notifier = newNotifier(timeout)
	sm.sound = newSoundPlayer(sm.uiHandler.PrintError)
	sm.popupFunc = func(title, body string) error { return beeep.Notify(title, body, "") }
	sm.soundFunc = sm.playNotifySound
}

// popupErrorHint follows the first pop-up error, which is the only one shown.
const popupErrorHint = "set enable_notifications = false to stop these pop-ups"

// notifiableServer reports whether messages in a chat on this JID server may
// notify: 1:1 chats (phone number or LID) and groups. Status updates
// (status@broadcast), broadcast lists and newsletters never notify.
func notifiableServer(server string) bool {
	switch server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
		return true
	}
	return false
}

// notifyIncoming shows a pop-up and/or plays a sound for a newly stored
// message, following the rules in notifier.decide. senderName is called only
// when a pop-up will be shown, so its contact lookups are skipped for
// messages that do not notify. It runs on the message-handling path: it may
// read the mute store, so it must not be called from the UI goroutine or with
// sm.mu held. The pop-up and sound run in their own goroutines.
func (sm *SessionManager) notifyIncoming(chat types.JID, isGroup, fromMe bool, msgTime time.Time, senderName func() string, chatName, text, preview string) {
	if sm.notifier == nil {
		return // not initialised (managers built without Init)
	}
	if !notifiableServer(chat.Server) {
		return
	}
	g := config.Config.General
	now := time.Now()
	// The cheap rules first, so a message that cannot notify never reads
	// the mute store. decide applies them again.
	if !g.EnableNotifications || fromMe || now.Sub(msgTime) > maxNotifyAge {
		return
	}

	canon := sm.canonicalChatJID(chat)
	key := canon.String()
	sm.mu.RLock()
	isOpen := key == sm.currentReceiver
	sm.mu.RUnlock()
	muted := isMutedAt(sm.mutedUntil(canon), now)

	popup, sound := sm.notifier.decide(key, notifyInput{
		Enabled:    g.EnableNotifications,
		FromMe:     fromMe,
		Muted:      muted,
		IsOpenChat: isOpen,
		MsgTime:    msgTime,
		Now:        now,
		Focus:      sm.terminalFocus(),
	})

	if popup {
		group := ""
		if isGroup {
			group = chatName
		}
		sender := ""
		if senderName != nil {
			sender = senderName()
		}
		title := notifyTitle(sender, group)
		body := notifyBody(text, preview, runtime.GOOS)
		sm.notifyWG.Add(1)
		go func() {
			defer sm.notifyWG.Done()
			if err := sm.popupFunc(title, body); err != nil {
				// Without a notification daemon every pop-up fails;
				// report the first failure only.
				sm.popupErrOnce.Do(func() {
					sm.uiHandler.PrintError(fmt.Errorf("notification: %w (%s)", err, popupErrorHint))
				})
			}
		}()
	}

	// play_sound = false silences both the sound and the bell;
	// use_terminal_bell replaces the sound with the bell.
	if sound && g.PlaySound {
		if g.UseTerminalBell {
			sm.uiHandler.Beep()
			return
		}
		sm.notifyWG.Add(1)
		go func() {
			defer sm.notifyWG.Done()
			sm.soundFunc()
		}()
	}
}

// playNotifySound is the default soundFunc. On first use it resolves the
// sound file (reporting any warning once) and looks up a system player; it
// then plays the file, or rings the terminal bell when no player or file is
// available.
func (sm *SessionManager) playNotifySound() {
	sm.soundOnce.Do(func() {
		cache, err := os.UserCacheDir()
		if err != nil {
			sm.uiHandler.PrintError(fmt.Errorf("notification sound: %w; using the terminal bell", err))
			return
		}
		path, warn := soundFile(config.Config.General.NotificationSound, cache)
		if warn != nil {
			sm.uiHandler.PrintError(warn)
		}
		sm.soundPath = path
	})
	if sm.soundPath == "" || !sm.sound.HasPlayer(sm.soundPath) {
		sm.uiHandler.Beep()
		return
	}
	sm.sound.Play(sm.soundPath)
}
