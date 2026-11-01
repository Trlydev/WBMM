package manager

import (
	"encoding/json"
	"fmt"
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
	// NoNMLUpdates stops updating NeoModLoader automatically.
	NoNMLUpdates bool `json:"noNmlUpdates,omitempty"`
	// SkipNML is the NeoModLoader release (its DLL's SHA-256) the person
	// went back from: automatic updates leave it alone.
	SkipNML string `json:"skipNml,omitempty"`
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
	// Skipped: GameBanana files (by mod ID) an automatic update couldn't
	// install; it doesn't retry them (the Update button still offers them).
	Skipped map[int]int `json:"skipped,omitempty"`
	// Verdicts of the malware scanner, by mod folder.
	Verdicts map[string]Verdict `json:"verdicts,omitempty"`
	// Maps put into save slots, by slot folder.
	Maps map[string]MapRecord `json:"maps,omitempty"`
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

func newState() *state {
	return &state{Records: map[string]Record{}, Verdicts: map[string]Verdict{}, Skipped: map[int]int{}, Maps: map[string]MapRecord{}}
}

func statePath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state.json"), nil
}

// loadState reads state.json. A file that can't be read as JSON is moved
// aside (state.json.bad-<time>) rather than overwritten by the next save.
func loadState() (*state, error) {
	s := newState()
	p, err := statePath()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		bad := fmt.Sprintf("%s.bad-%d", p, time.Now().Unix())
		os.Rename(p, bad)
		return newState(), fmt.Errorf("state.json was damaged; kept it as %s and started over", filepath.Base(bad))
	}
	if s.Records == nil {
		s.Records = map[string]Record{}
	}
	if s.Verdicts == nil {
		s.Verdicts = map[string]Verdict{}
	}
	if s.Skipped == nil {
		s.Skipped = map[int]int{}
	}
	if s.Maps == nil {
		s.Maps = map[string]MapRecord{}
	}
	return s, nil
}

func (s *state) save() error {
	p, err := statePath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// A temporary file of its own, so two writers never share one.
	f, err := os.CreateTemp(filepath.Dir(p), "state-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// update changes the state as it is on disk, under a lock shared with
// other wbmm processes (the window and a command run beside it), so neither
// undoes the other's changes.
func (m *Manager) update(fn func(st *state)) error {
	lk, err := LockState(15 * time.Second)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	fresh, _ := loadState()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st = fresh
	fn(fresh)
	return fresh.save()
}
