// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"reflect"
	"testing"
)

func TestVscodeModeCommand(t *testing.T) {
	sm, ui := newTestSessionManager(t)
	h := commandRegistry["vs-code-mode"]
	if h == nil {
		t.Fatal("vs-code-mode not registered")
	}
	h(sm, nil, "vs-code-mode", nil)
	h(sm, nil, "vs-code-mode", []string{"ON"})
	h(sm, nil, "vs-code-mode", []string{"off"})
	h(sm, nil, "vs-code-mode", []string{"maybe"}) // usage, no call
	if want := []string{"", "on", "off"}; !reflect.DeepEqual(ui.VscodeModes, want) {
		t.Errorf("modes = %v, want %v", ui.VscodeModes, want)
	}
}
