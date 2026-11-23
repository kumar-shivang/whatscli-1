// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/PalWorks/whatscli/messages"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// press sends one key event to the input field on the app's event loop,
// through its key capture like a real key press.
func press(app *tview.Application, input *tview.InputField, ev *tcell.EventKey) {
	run(app, func() {
		if c := input.GetInputCapture(); c != nil {
			if ev = c(ev); ev == nil {
				return
			}
		}
		input.InputHandler()(ev, func(tview.Primitive) {})
	})
	settle(app)
}

// Shift+Enter, Ctrl+Enter, Alt+Enter and Ctrl+J (as a line feed or as a
// reported Ctrl+J) add a line break (shown as ↵); Enter sends it as a real
// line break.
func TestNewLineKeys(t *testing.T) {
	for name, ev := range map[string]*tcell.EventKey{
		"Ctrl+Enter":  tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl),
		"Shift+Enter": tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModShift),
		"Alt+Enter":   tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt),
		"Ctrl+J (LF)": tcell.NewEventKey(tcell.KeyRune, '\n', tcell.ModNone),
		"Ctrl+J":      tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModCtrl),
	} {
		t.Run(name, func(t *testing.T) {
			app, input, cmds := composeApp(t)
			run(app, func() { enterChat(chatA) })
			typeKeys(app, input, "line one")
			press(app, input, ev)
			typeKeys(app, input, "line two")
			if got := inputText(app, input); got != "line one↵line two" {
				t.Fatalf("input = %q", got)
			}
			typeKeys(app, input, tcell.KeyEnter)
			select {
			case cmd := <-cmds:
				if cmd.Name != "send" || cmd.Params[1] != "line one\nline two" {
					t.Fatalf("sent %+v", cmd)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Enter did not send")
			}
		})
	}
}

// In a command, Alt+Enter is not a line break.
func TestNewLineNotInCommands(t *testing.T) {
	app, input, _ := composeApp(t)
	run(app, func() { enterChat(chatA) })
	typeKeys(app, input, "/hel")
	press(app, input, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt))
	if got := inputText(app, input); got != "/help" && got != "/hel" && got != "" {
		t.Fatalf("input = %q", got)
	}
	if got := inputText(app, input); strings.ContainsRune(got, newlineRune) {
		t.Fatalf("↵ typed into a command: %q", got)
	}
}

// Editing a message with line breaks shows them as ↵ and sends them back.
func TestEditKeepsLineBreaks(t *testing.T) {
	app, input, cmds := composeApp(t)
	run(app, func() {
		enterChat(chatA)
		ctx.CurRegions = []messages.Message{{Id: "m", ChatId: chatA.Id, FromMe: true, Text: "a\nb", Timestamp: uint64(time.Now().Unix())}}
	})
	typeKeys(app, input, tcell.KeyUp)
	if got := inputText(app, input); got != "a↵b" {
		t.Fatalf("edit loaded %q", got)
	}
	typeKeys(app, input, "c", tcell.KeyEnter)
	if cmd := <-cmds; cmd.Name != "edit" || cmd.Params[2] != "a\nbc" {
		t.Fatalf("sent %+v", cmd)
	}
}
