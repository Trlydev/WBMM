package manager

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/steam"
)

// Subscribe subscribes to a Workshop mod through the Steam client, waits
// until Steam has downloaded it, and checks it with the malware scanner
// before the game can load it. The verdict is nil for items that aren't
// mods (worlds).
func (m *Manager) Subscribe(id string, progress func(steam.Progress)) (*Verdict, error) {
	gd, err := m.GameDir()
	if err != nil {
		return nil, err
	}
	if err := steam.Subscribe(gd, id, progress); err != nil {
		return nil, err
	}
	mods, err := m.List()
	if err != nil {
		return nil, err
	}
	for _, md := range mods {
		if md.WorkshopID == id && md.Problem == "" {
			v, err := m.Check(md)
			return &v, err
		}
	}
	return nil, nil
}

// Unsubscribe unsubscribes from a Workshop mod. Steam deletes its files;
// this waits a little for that so the list is right afterwards.
func (m *Manager) Unsubscribe(id string) error {
	gd, err := m.GameDir()
	if err != nil {
		return err
	}
	if err := steam.Unsubscribe(gd, id); err != nil {
		return err
	}
	dir := filepath.Join(game.WorkshopDir(gd), id)
	for i := 0; i < 40; i++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}
