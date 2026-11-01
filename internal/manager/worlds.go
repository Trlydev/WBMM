package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/Trlydev/WBMM/internal/game"
)

// World is a Workshop world (a saved map) Steam has downloaded. WorldBox
// lists subscribed worlds itself; they hold no code.
type World struct {
	WorkshopID string
	Folder     string
	Name       string // from map.meta; the Workshop title is usually better
	Preview    string // preview.png, if any
	Updated    int64
}

// Worlds returns the subscribed Workshop worlds.
func (m *Manager) Worlds() ([]World, error) {
	gd, err := m.GameDir()
	if err != nil {
		return nil, err
	}
	dir := game.WorkshopDir(gd)
	var out []World
	for _, id := range readDirs(dir) {
		folder := filepath.Join(dir, id)
		fi, err := os.Stat(filepath.Join(folder, "map.wbox"))
		if err != nil {
			continue
		}
		w := World{WorkshopID: id, Folder: folder, Updated: fi.ModTime().Unix()}
		if b, err := os.ReadFile(filepath.Join(folder, "map.meta")); err == nil {
			var meta struct {
				MapStats struct {
					Name string `json:"name"`
				} `json:"mapStats"`
			}
			if json.Unmarshal(b, &meta) == nil {
				w.Name = meta.MapStats.Name
			}
		}
		for _, p := range []string{"preview.png", "preview_small.png"} {
			if _, err := os.Stat(filepath.Join(folder, p)); err == nil {
				w.Preview = filepath.Join(folder, p)
				break
			}
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out, nil
}
