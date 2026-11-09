package handover

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
)

// vscodeConfigDirs are where VS Code and its builds keep window state on Linux.
func vscodeConfigDirs(home string) []string {
	var out []string
	for _, n := range []string{"Code", "Code - OSS", "VSCodium", "Cursor"} {
		out = append(out, filepath.Join(home, ".config", n))
	}
	return out
}

// OpenFolders lists the folders VS Code has open, read from its saved window state. It reads one small
// JSON file per build and nothing else; a missing or odd file just contributes nothing.
func OpenFolders(home string) []string {
	seen := map[string]bool{}
	for _, dir := range vscodeConfigDirs(home) {
		raw, err := os.ReadFile(filepath.Join(dir, "User", "globalStorage", "storage.json"))
		if err != nil {
			continue
		}
		var s struct {
			WindowsState struct {
				LastActive struct {
					Folder string `json:"folder"`
				} `json:"lastActiveWindow"`
				Opened []struct {
					Folder string `json:"folder"`
				} `json:"openedWindows"`
			} `json:"windowsState"`
			Backup struct {
				Folders []struct {
					FolderURI string `json:"folderUri"`
				} `json:"folders"`
			} `json:"backupWorkspaces"`
		}
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		add := func(uri string) {
			if p := fileURIToPath(uri); p != "" {
				seen[p] = true
			}
		}
		add(s.WindowsState.LastActive.Folder)
		for _, w := range s.WindowsState.Opened {
			add(w.Folder)
		}
		for _, f := range s.Backup.Folders {
			add(f.FolderURI)
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// fileURIToPath turns "file:///home/me/proj%20x" into a path. Anything that is not a local file URI
// (a remote or WSL workspace, say) gives "" because this machine cannot snapshot it.
func fileURIToPath(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return ""
	}
	return filepath.Clean(u.Path)
}
