package manager

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Trlydev/WBMM/internal/archive"
	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/sources"
)

// GameBanana maps are save slots: a folder with map.wbox (the world),
// usually map.meta and preview pictures. wbmm copies those files, and only
// those, into a free slot of the game's saves (save7, say), where WorldBox
// lists them under Saves. Nothing in a map runs.

// MapRecord is a GameBanana map wbmm put into a save slot.
type MapRecord struct {
	GBID      int       `json:"gbId"`
	FileID    int       `json:"fileId"`
	Name      string    `json:"name"`
	Slot      string    `json:"slot"`   // the save folder
	SHA256    string    `json:"sha256"` // of map.wbox as installed
	Installed time.Time `json:"installed"`
}

// mapFiles are the files a save slot holds.
var mapFiles = map[string]bool{"map.wbox": true, "map.meta": true, "preview.png": true, "preview_small.png": true, "map_stats.s3db": true}

const maxMapFile = 512 << 20

// InstallMap downloads a GameBanana map (fileID, or the newest when 0) and
// puts each world in it into a free save slot. force installs a file
// GameBanana's virus scan hasn't passed. It returns the slots used.
func (m *Manager) InstallMap(gbID, fileID int, force bool) ([]string, error) {
	gd, err := m.GameDir()
	if err != nil {
		return nil, err
	}
	data, err := game.UserDataDir(gd)
	if err != nil {
		return nil, err
	}
	mod, err := sources.GBGetMod(gbID)
	if err != nil {
		return nil, err
	}
	if !mod.IsMap() {
		return nil, errors.New("this is a mod, not a map")
	}
	files, err := sources.GBFiles(gbID)
	if err != nil {
		return nil, err
	}
	f, err := PickFile(files, fileID)
	if err != nil {
		return nil, err
	}
	if !f.Clean() && !force {
		return nil, &NotCleanError{File: f}
	}
	arc, _, err := downloadGB(f)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "wbmm-map-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := archive.Extract(arc, tmp); err != nil {
		return nil, fmt.Errorf("unpacking %s: %w", f.Name, err)
	}
	worlds := findWorlds(tmp)
	if len(worlds) == 0 {
		return nil, fmt.Errorf("%s has no map.wbox: it isn't a WorldBox save", f.Name)
	}
	saves := game.SavesDir(data)
	if err := os.MkdirAll(saves, 0o755); err != nil {
		return nil, err
	}
	var slots []string
	recs := map[string]MapRecord{}
	for _, w := range worlds {
		slot, err := freeSlot(saves)
		if err != nil {
			return slots, err
		}
		sha, err := copyWorld(w, slot)
		if err != nil {
			os.RemoveAll(slot)
			return slots, err
		}
		recs[slot] = MapRecord{GBID: gbID, FileID: f.ID, Name: mod.Name, Slot: slot, SHA256: sha, Installed: time.Now()}
		slots = append(slots, filepath.Base(slot))
	}
	err = m.update(func(st *state) {
		for k, r := range recs {
			st.Maps[k] = r
		}
	})
	if err == nil {
		os.Remove(arc)
	}
	return slots, err
}

// findWorlds returns the folders holding a map.wbox (ten at most).
func findWorlds(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= 10 {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "map.wbox") && d.Type().IsRegular() {
			out = append(out, filepath.Dir(p))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// freeSlot makes the first unused save folder (save1, save2, …).
func freeSlot(saves string) (string, error) {
	for n := 1; n < 1000; n++ {
		p := filepath.Join(saves, fmt.Sprintf("save%d", n))
		if err := os.Mkdir(p, 0o755); err == nil {
			return p, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", errors.New("no free save slot")
}

// copyWorld copies a world's save files into slot and returns the
// SHA-256 of its map.wbox.
func copyWorld(src, slot string) (string, error) {
	es, err := os.ReadDir(src)
	if err != nil {
		return "", err
	}
	for _, e := range es {
		name := strings.ToLower(e.Name())
		if !mapFiles[name] || !e.Type().IsRegular() {
			continue
		}
		if fi, err := e.Info(); err != nil || fi.Size() > maxMapFile {
			return "", fmt.Errorf("%s is too big for a save", e.Name())
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(slot, name)); err != nil {
			return "", err
		}
	}
	return sha256File(filepath.Join(slot, "map.wbox"))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(in, maxMapFile))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// InstalledMaps returns the GameBanana maps whose save slots are still
// there, by GameBanana ID.
func (m *Manager) InstalledMaps() map[int][]MapRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int][]MapRecord{}
	for _, r := range m.st.Maps {
		if _, err := os.Stat(filepath.Join(r.Slot, "map.wbox")); err == nil {
			out[r.GBID] = append(out[r.GBID], r)
		}
	}
	return out
}

// RemoveMap deletes the save slots a GameBanana map was put into. A slot
// the game has saved over since (its map.wbox changed) is left alone: it
// is someone's world now. kept counts those.
func (m *Manager) RemoveMap(gbID int) (kept int, err error) {
	err = m.update(func(st *state) {
		for k, r := range st.Maps {
			if r.GBID != gbID {
				continue
			}
			if sha, serr := sha256File(filepath.Join(r.Slot, "map.wbox")); serr == nil {
				if sha == r.SHA256 && filepath.Base(filepath.Dir(r.Slot)) == "saves" {
					os.RemoveAll(r.Slot)
				} else {
					kept++
				}
			}
			delete(st.Maps, k)
		}
	})
	return kept, err
}
