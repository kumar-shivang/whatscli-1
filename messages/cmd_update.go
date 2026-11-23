// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"fmt"

	"github.com/PalWorks/whatscli/update"
	"go.mau.fi/whatsmeow"
)

// updateExecutable returns the binary to replace; a variable so tests never
// touch the test binary.
var updateExecutable = update.Executable

// cmdUpdate installs the latest release over the running binary and exits.
// It runs in its own goroutine so the manager loop is never blocked by the
// network.
func cmdUpdate(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	if len(params) > 0 {
		sm.printCommandUsage(cmdName, "")
		return
	}
	go sm.runUpdate()
}

func (sm *SessionManager) runUpdate() {
	sm.uiHandler.PrintText("Checking for updates...")
	tag, err := update.Latest()
	if err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("update: %w", err))
		return
	}
	if tag == update.Current {
		sm.uiHandler.PrintText(fmt.Sprintf("Already up to date (%s).", tag))
		return
	}
	sm.uiHandler.PrintText(fmt.Sprintf("Installing %s (current: %s)...", tag, update.Current))
	exe, err := updateExecutable()
	if err == nil {
		err = update.Install(tag, exe)
	}
	if err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("update: %w", err))
		return
	}
	sm.uiHandler.PrintText(fmt.Sprintf("Updated to %s. Exiting; start whatscli again to use it.", tag))
	sm.uiHandler.Quit()
}
