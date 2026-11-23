// SPDX-License-Identifier: AGPL-3.0-only

package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func makeArchive(t *testing.T, content string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "whatscli", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return b.Bytes()
}

func server(t *testing.T, tag string, archive []byte, sum string) {
	t.Helper()
	asset := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	base := "/" + Repo + "/releases/download/" + tag
	mux.HandleFunc(base+"/whatscli-"+tag+"-checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum, asset)
	})
	mux.HandleFunc(base+"/"+asset, func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	srv := httptest.NewServer(mux)
	oldA, oldD := APIBase, DownloadBase
	APIBase, DownloadBase = srv.URL, srv.URL
	t.Cleanup(func() { srv.Close(); APIBase, DownloadBase = oldA, oldD })
}

func TestLatestAndInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update unsupported on windows")
	}
	arc := makeArchive(t, "new-binary")
	s := sha256.Sum256(arc)
	server(t, "v9.9.9", arc, hex.EncodeToString(s[:]))

	tag, err := Latest()
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
	exe := filepath.Join(t.TempDir(), "whatscli")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := Install(tag, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatalf("binary = %q", got)
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	server(t, "v9.9.9", makeArchive(t, "evil"), "00"+string(bytes.Repeat([]byte("0"), 62)))
	exe := filepath.Join(t.TempDir(), "whatscli")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := Install("v9.9.9", exe); err == nil {
		t.Fatal("want checksum error")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary was modified: %q", got)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".whatscli-update-*"))
	if len(left) != 0 {
		t.Fatalf("temp file left behind: %v", left)
	}
}
