// SPDX-License-Identifier: AGPL-3.0-only

// Package update downloads the latest GitHub release of whatscli-extended and
// replaces the running binary with it.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are read from.
const Repo = "kumar-shivang/whatscli-extended"

// maxDownload caps a release download (the binary is about 30 MB).
const maxDownload = 300 << 20

// Current is the running version, set by main at start-up.
var Current = "dev"

var (
	// APIBase and DownloadBase are variables so tests can point them at a server.
	APIBase      = "https://api.github.com"
	DownloadBase = "https://github.com"
	// Client is the HTTP client used for all requests.
	Client = &http.Client{Timeout: 5 * time.Minute}
)

// Latest returns the tag of the newest published (non pre-release) release.
func Latest() (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/releases/latest", APIBase, Repo), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release lookup failed: %s", resp.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", err
	}
	if rel.Tag == "" {
		return "", errors.New("release lookup returned no tag")
	}
	return rel.Tag, nil
}

// AssetName is the release archive for tag on the given platform.
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("whatscli-%s-%s-%s.tar.gz", tag, goos, goarch)
}

func get(url string) (io.ReadCloser, error) {
	resp, err := Client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// Install downloads tag for this platform, checks its SHA-256 against the
// release checksums file and replaces the executable at exePath.
func Install(tag, exePath string) error {
	if runtime.GOOS == "windows" {
		return errors.New("self-update is not supported on Windows; download the release zip manually")
	}
	asset := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("%s/%s/releases/download/%s", DownloadBase, Repo, tag)

	sums, err := get(fmt.Sprintf("%s/whatscli-%s-checksums.txt", base, tag))
	if err != nil {
		return err
	}
	sumData, err := io.ReadAll(io.LimitReader(sums, 1<<20))
	sums.Close()
	if err != nil {
		return err
	}
	want := checksumFor(string(sumData), asset)
	if want == "" {
		return fmt.Errorf("no checksum listed for %s (no build for %s/%s?)", asset, runtime.GOOS, runtime.GOARCH)
	}

	body, err := get(base + "/" + asset)
	if err != nil {
		return err
	}
	defer body.Close()

	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".whatscli-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s (try sudo or reinstall to a writable dir): %w", exePath, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if err := extractVerified(io.LimitReader(body, maxDownload), want, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, exePath)
}

// checksumFor finds the hash of name in sha256sum output.
func checksumFor(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// extractVerified reads a .tar.gz archive from r, checks the archive's
// SHA-256 equals want and writes the "whatscli" entry to out. Nothing is
// trusted until the hash matches: the archive is read to the end first.
func extractVerified(r io.Reader, want string, out io.Writer) error {
	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(r, h))
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("archive has no whatscli binary")
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "whatscli" {
			_, err := io.Copy(out, io.LimitReader(tr, maxDownload))
			return err
		}
	}
}

// Executable returns the real path of the running binary.
func Executable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}
