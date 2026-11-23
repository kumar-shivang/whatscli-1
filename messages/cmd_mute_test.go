// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

func TestParseMuteDuration(t *testing.T) {
	good := map[string]time.Duration{
		"": 0, "30m": 30 * time.Minute, "8h": 8 * time.Hour,
		"1d": 24 * time.Hour, "1w": 168 * time.Hour, "1h30m": 90 * time.Minute,
	}
	for in, want := range good {
		got, err := parseMuteDuration(in)
		if err != nil || got != want {
			t.Errorf("parseMuteDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "-1h", "abc", "1.5d", "d", "99999999d", "99999999w"} {
		if _, err := parseMuteDuration(in); err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("parseMuteDuration(%q) error = %v, want one naming the input", in, err)
		}
	}
}

const muteTestJID = "1555000111@s.whatsapp.net"

// muteHarness wires a recording sender and a done channel into a test manager.
func muteHarness(t *testing.T, sendErr error) (*SessionManager, *MockUiHandler, *[]appstate.PatchInfo, chan struct{}) {
	t.Helper()
	sm, ui := newTestSessionManager(t)
	var sent []appstate.PatchInfo
	done := make(chan struct{}, 1)
	sm.appStateSender = func(_ context.Context, p appstate.PatchInfo) error {
		sent = append(sent, p)
		return sendErr
	}
	sm.muteDone = func() { done <- struct{}{} }
	sm.currentReceiver = muteTestJID
	return sm, ui, &sent, done
}

func TestCmdMuteNoOpenChat(t *testing.T) {
	sm, ui, sent, _ := muteHarness(t, nil)
	sm.currentReceiver = ""
	cmdMute(sm, &whatsmeow.Client{}, "mute", nil)
	if len(*sent) != 0 {
		t.Fatalf("sent=%d texts=%v errors=%v", len(*sent), ui.Texts, ui.Errors)
	}
	if len(ui.Errors) == 0 || !strings.Contains(ui.Errors[0].Error(), "open a chat first") {
		t.Errorf("errors = %v", ui.Errors)
	}
}

func TestCmdMuteOffline(t *testing.T) {
	sm, ui, sent, _ := muteHarness(t, nil)
	cmdMute(sm, nil, "mute", []string{"8h"})
	if len(*sent) != 0 || len(ui.Errors) != 1 || !strings.Contains(ui.Errors[0].Error(), "not connected") {
		t.Fatalf("sent=%d errors=%v", len(*sent), ui.Errors)
	}
	if sm.IsMuted(muteTestJID) {
		t.Error("state changed")
	}
}

func TestCmdMuteSuccess(t *testing.T) {
	sm, ui, sent, done := muteHarness(t, nil)
	jid, _ := types.ParseJID(muteTestJID)
	c := &whatsmeow.Client{}

	cmdMute(sm, c, "mute", []string{"8h"})
	<-done
	want := appstate.BuildMute(jid, true, 8*time.Hour)
	if len(*sent) != 1 || (*sent)[0].Type != want.Type || (*sent)[0].Mutations[0].Index[1] != want.Mutations[0].Index[1] {
		t.Fatalf("sent = %+v", *sent)
	}
	if !(*sent)[0].Mutations[0].Value.GetMuteAction().GetMuted() {
		t.Error("patch is not a mute")
	}
	if len(ui.Texts) != 1 || !strings.Contains(ui.Texts[0], "muted until "+time.Now().Add(8*time.Hour).Format("2006-01-02")) {
		t.Errorf("confirmation = %v", ui.Texts)
	}
	if !sm.IsMuted(muteTestJID) || len(ui.Texts) != 1 || len(ui.ChatLists) != 1 {
		t.Fatalf("muted=%v texts=%v lists=%d", sm.IsMuted(muteTestJID), ui.Texts, len(ui.ChatLists))
	}

	cmdUnmute(sm, c, "unmute", nil)
	<-done
	if len(*sent) != 2 || (*sent)[1].Mutations[0].Value.GetMuteAction().GetMuted() {
		t.Fatalf("unmute patch wrong: %+v", *sent)
	}
	if sm.IsMuted(muteTestJID) {
		t.Error("still muted after /unmute")
	}

	cmdMute(sm, c, "mute", nil)
	<-done
	if !sm.IsMuted(muteTestJID) || !strings.Contains(strings.Join(ui.Texts, "\n"), "always") {
		t.Errorf("forever mute: muted=%v texts=%v", sm.IsMuted(muteTestJID), ui.Texts)
	}
}

func TestCmdMuteSendFails(t *testing.T) {
	sm, ui, _, done := muteHarness(t, errors.New("boom"))
	cmdMute(sm, &whatsmeow.Client{}, "mute", []string{"8h"})
	<-done
	if len(ui.Errors) != 1 || sm.IsMuted(muteTestJID) {
		t.Fatalf("errors=%v muted=%v", ui.Errors, sm.IsMuted(muteTestJID))
	}
}

func TestCmdMuteBadDuration(t *testing.T) {
	sm, ui, sent, _ := muteHarness(t, nil)
	cmdMute(sm, &whatsmeow.Client{}, "mute", []string{"soon"})
	if len(*sent) != 0 || len(ui.Texts) != 1 || !strings.Contains(ui.Texts[0], "[duration]") {
		t.Fatalf("sent=%d texts=%v", len(*sent), ui.Texts)
	}
}
