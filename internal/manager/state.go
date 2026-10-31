package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Config is what the person set in Setup. The zero value is the default:
// both kinds of automatic update on.
type Config struct {
	GameDir string `json:"gameDir,omitempty"`
	// NoModUpdates stops installing GameBanana mod updates automatically.
	NoModUpdates bool `json:"noModUpdates,omitempty"`
	// NoAppUpdates stops wbmm updating itself.
	NoAppUpdates bool `json:"noAppUpdates,omitempty"`
}

// Record is a mod wbmm installed from GameBanana, so it can be updated.
type Record struct {
	UID       string    `json:"uid"`
	Folder    string    `json:"folder"` // name inside Mods/
	Name      string    `json:"name"`
	GBID      int       `json:"gbId"`
	FileID    int       `json:"fileId"`
	FileName  string    `json:"fileName"`
	FileAdded int64     `json:"fileAdded"`
	SHA256    string    `json:"sha256"`
	Installed time.Time `json:"installed"`
}

type state struct {
	Config  Config            `json:"config"`
	Records map[string]Record `json:"records"` // by UID
	// Verdicts of the malware scanner, by mod folder.
	Verdicts map[string]Verdict `json:"verdicts,omitempty"`
}

// ConfigDir is wbmm's own folder (settings, state, log).
func ConfigDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d = filepath.Join(d, "wbmm")
	return d, os.MkdirAll(d, 0o755)
}

func loadState() (*state, error) {
	s := &state{Records: map[string]Record{}, Verdicts: map[string]Verdict{}}
	d, err := ConfigDir()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(filepath.Join(d, "state.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return s, err
	}
	if s.Records == nil {
		s.Records = map[string]Record{}
	}
	if s.Verdicts == nil {
		s.Verdicts = map[string]Verdict{}
	}
	return s, nil
}

func (s *state) save() error {
	d, err := ConfigDir()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(d, "state.json")
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}
