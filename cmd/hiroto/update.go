package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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

// doUpdate runs `go install` to update Hiroto to the latest version.
func doUpdate() (string, error) {
	cmd := exec.Command("go", "install", "github.com/hirotomasato/hiroto/cmd/hiroto@latest")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("update failed: %v\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}