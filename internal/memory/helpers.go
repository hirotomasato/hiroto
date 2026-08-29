package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func decodeJSON(data []byte, v any) { _ = json.Unmarshal(data, v) }

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func nextID(entries []Entry) string {
	max := 0
	for _, e := range entries {
		var n int
		_, _ = fmt.Sscanf(e.ID, "m%d", &n)
		if n > max {
			max = n
		}
	}
	return "m" + strconv.Itoa(max+1)
}

func homeDir() string {
	if h := os.Getenv("HIROTO_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hiroto")
}
