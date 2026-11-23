// SPDX-License-Identifier: AGPL-3.0-only

package update

import (
	"archive/tar"
	"archive/zip"
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
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&b)
		w, _ := zw.Create("whatscli.exe")
		w.Write([]byte(content))
		zw.Close()
		return b.Bytes()
	}
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

func TestWindowsZipExtractAndMoveAside(t *testing.T) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create("whatscli.exe")
	w.Write([]byte("win-new"))
	zw.Close()
	s := sha256.Sum256(b.Bytes())
	var out bytes.Buffer
	if err := extractVerified(bytes.NewReader(b.Bytes()), hex.EncodeToString(s[:]), "windows", &out); err != nil || out.String() != "win-new" {
		t.Fatalf("extract: %q, %v", out.String(), err)
	}
	if AssetName("v1.0.0", "windows", "amd64") != "whatscli-v1.0.0-windows-amd64.zip" {
		t.Fatal("bad windows asset name")
	}

	dir := t.TempDir()
	exe, nw := filepath.Join(dir, "whatscli.exe"), filepath.Join(dir, "new")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(nw, []byte("new"), 0o755)
	if err := replaceBinary(nw, exe, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("exe = %q", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old" {
		t.Fatalf("old = %q", got)
	}
}
