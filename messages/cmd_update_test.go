// SPDX-License-Identifier: AGPL-3.0-only

package messages

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PalWorks/whatscli/update"
)

func stubRelease(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	}))
	oldA, oldC := update.APIBase, update.Current
	update.APIBase = srv.URL
	t.Cleanup(func() { srv.Close(); update.APIBase, update.Current = oldA, oldC })
}

func TestUpdateRegistered(t *testing.T) {
	if _, ok := commandRegistry["update"]; !ok {
		t.Fatal("update is not registered")
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	stubRelease(t, "v1.2.3")
	update.Current = "v1.2.3"
	sm, ui := newTestSessionManager(t)
	sm.runUpdate()
	if ui.QuitCalled != 0 || len(ui.Errors) != 0 {
		t.Fatalf("quit=%d errors=%v", ui.QuitCalled, ui.Errors)
	}
}

func TestUpdateFailureDoesNotQuit(t *testing.T) {
	stubRelease(t, "v1.2.4") // checksums/asset 404 -> Install fails
	update.Current = "v1.2.3"
	oldExe := updateExecutable
	updateExecutable = func() (string, error) { return t.TempDir() + "/whatscli", nil }
	t.Cleanup(func() { updateExecutable = oldExe })
	oldD := update.DownloadBase
	update.DownloadBase = update.APIBase
	t.Cleanup(func() { update.DownloadBase = oldD })
	sm, ui := newTestSessionManager(t)
	sm.runUpdate()
	if ui.QuitCalled != 0 || len(ui.Errors) != 1 {
		t.Fatalf("quit=%d errors=%v", ui.QuitCalled, ui.Errors)
	}
}
