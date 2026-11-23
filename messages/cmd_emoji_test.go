// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"strings"
	"testing"
)

func TestCmdEmoji(t *testing.T) {
	sm, ui := newTestSessionManager(t)
	if _, ok := commandRegistry["emoji"]; !ok {
		t.Fatal("emoji is not registered")
	}
	cmdEmoji(sm, nil, "emoji", nil)
	if ui.EmojiOpened != 1 {
		t.Fatalf("OpenEmojiPicker called %d times, want 1", ui.EmojiOpened)
	}
	cmdEmoji(sm, nil, "emoji", []string{"smile"})
	if ui.EmojiOpened != 1 || len(ui.Texts) == 0 || !strings.Contains(ui.Texts[len(ui.Texts)-1], "Usage:") {
		t.Fatalf("/emoji smile: want a usage message and no picker, got %d opens, %q", ui.EmojiOpened, ui.Texts)
	}
}
