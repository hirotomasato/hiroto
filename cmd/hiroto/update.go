package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repoAPI = "https://api.github.com/repos/hirotomasato/hiroto/releases/latest"

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// checkUpdate compares the running version against the latest GitHub release.
// Returns latest version string and whether an update is available.
func checkUpdate() (latest string, available bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(repoAPI)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", false
	}
	latest = strings.TrimPrefix(rel.TagName, "v")
	if latest == "" {
		return "", false
	}
	available = latest != version
	return latest, available
}

// doUpdate runs `go install` to update Hiroto to the latest version, then
// syncs the freshly built binary onto the one that is currently running.
// Without the sync step, `go install` writes to $GOBIN (~/go/bin) while the
// running binary may live elsewhere (e.g. ~/.local/bin) — a "successful"
// update that the user never actually runs.
func doUpdate() (string, error) {
	cmd := exec.Command("go", "install", "github.com/hirotomasato/hiroto/cmd/hiroto@latest")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("update failed: %v\n%s", err, string(out))
	}

	notes := []string{strings.TrimSpace(string(out))}
	synced, syncMsg := syncRunningBinary()
	if syncMsg != "" {
		notes = append(notes, syncMsg)
	}
	if synced {
		notes = append(notes, "installed binary updated")
	}
	return strings.Join(notes, "\n"), nil
}

// syncRunningBinary copies the freshly installed binary over the executable
// this process was started from, so the user's PATH entry really runs the new
// version. Safe no-op when both are already the same file.
func syncRunningBinary() (bool, string) {
	self, err := os.Executable()
	if err != nil {
		return false, "note: could not resolve running binary path; update landed in GOPATH/bin"
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return false, "note: could not resolve running binary path; update landed in GOPATH/bin"
	}
	gobinOut, err := exec.Command("go", "env", "GOBIN").Output()
	if err != nil {
		return false, "note: could not read GOBIN; update landed in GOPATH/bin"
	}
	dst := strings.TrimSpace(string(gobinOut))
	if dst == "" {
		gopath, err := exec.Command("go", "env", "GOPATH").Output()
		if err != nil {
			return false, "note: could not read GOPATH; update landed in GOPATH/bin"
		}
		dst = filepath.Join(strings.TrimSpace(string(gopath)), "bin", binaryName())
	} else {
		dst = filepath.Join(dst, binaryName())
	}

	if err := syncBinary(dst, self); err != nil {
		return false, fmt.Sprintf("note: sync to %s failed: %v", self, err)
	}
	if samePath(dst, self) {
		return false, "" // PATH entry already points at the fresh binary
	}
	return true, fmt.Sprintf("synced: %s -> %s", dst, self)
}

// binaryName returns the platform-specific executable name.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "hiroto.exe"
	}
	return "hiroto"
}

// syncBinary replaces dst with src via rename (atomic on POSIX), so the
// running binary is never truncated mid-execution. No-op when both are the
// same path or already identical. Falls back to copy+rename on Windows where
// renaming over a running executable fails.
func syncBinary(src, dst string) error {
	if src == dst || samePath(src, dst) {
		return nil
	}
	if si, di, err := sameFile(src, dst); err == nil && si != nil && di != nil && os.SameFile(si, di) {
		return nil
	}
	if runtime.GOOS != "windows" {
		return os.Rename(src, dst)
	}
	// Windows: a running exe cannot be renamed over or removed.
	// Move the old binary aside first, then rename the new one in.
	old := dst + ".old"
	if err := os.Rename(dst, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		// try to restore
		_ = os.Rename(old, dst)
		return err
	}
	_ = os.Remove(old)
	return nil
}

// samePath reports whether two paths refer to the same file after resolving
// symlinks and case differences on macOS/Windows.
func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		if ra == rb {
			return true
		}
		return filepath.Clean(strings.ToLower(ra)) == filepath.Clean(strings.ToLower(rb)) &&
			(runtime.GOOS == "windows" || runtime.GOOS == "darwin")
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// sameFile stats both paths; error means either is missing or unreadable.
func sameFile(a, b string) (os.FileInfo, os.FileInfo, error) {
	si, err := os.Stat(a)
	if err != nil {
		return nil, nil, err
	}
	di, err := os.Stat(b)
	if err != nil {
		return nil, nil, err
	}
	return si, di, nil
}
