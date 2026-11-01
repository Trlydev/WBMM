package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMapSlots(t *testing.T) {
	m, _ := fakeGame(t)
	src := t.TempDir()
	world := filepath.Join(src, "save2")
	write(t, filepath.Join(world, "map.wbox"), "world")
	write(t, filepath.Join(world, "map.meta"), "{}")
	write(t, filepath.Join(world, "Preview.PNG"), "png")
	write(t, filepath.Join(world, "payload.dll"), "MZ")                  // not a save file
	write(t, filepath.Join(src, "notes", "map.wbox.txt"), "not a world") // not map.wbox
	worlds := findWorlds(src)
	if len(worlds) != 1 || worlds[0] != world {
		t.Fatalf("findWorlds = %v", worlds)
	}

	saves := filepath.Join(t.TempDir(), "saves") // RemoveMap only deletes inside a saves folder
	write(t, filepath.Join(saves, "save1", "map.wbox"), "theirs")
	install := func() string {
		slot, err := freeSlot(saves)
		if err != nil {
			t.Fatal(err)
		}
		sha, err := copyWorld(world, slot)
		if err != nil {
			t.Fatal(err)
		}
		m.update(func(st *state) { st.Maps[slot] = MapRecord{GBID: 7, Slot: slot, SHA256: sha} })
		return slot
	}
	a, b := install(), install()
	if filepath.Base(a) != "save2" || filepath.Base(b) != "save3" {
		t.Fatalf("slots %s %s, want save2 save3 (save1 is taken)", a, b)
	}
	if _, err := os.Stat(filepath.Join(a, "payload.dll")); err == nil {
		t.Error("copied a file that isn't part of a save")
	}
	if _, err := os.Stat(filepath.Join(a, "preview.png")); err != nil {
		t.Error("preview not copied:", err)
	}
	if got := m.InstalledMaps()[7]; len(got) != 2 {
		t.Fatalf("InstalledMaps = %v", got)
	}

	// The game saved over b: removing the map keeps it.
	write(t, filepath.Join(b, "map.wbox"), "someone's world now")
	kept, err := m.RemoveMap(7)
	if err != nil || kept != 1 {
		t.Fatalf("RemoveMap = %d, %v", kept, err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Error("untouched slot not removed")
	}
	if _, err := os.Stat(filepath.Join(b, "map.wbox")); err != nil {
		t.Error("removed a slot the game saved over")
	}
	if _, err := os.Stat(filepath.Join(saves, "save1", "map.wbox")); err != nil {
		t.Error("touched someone else's save")
	}
	if len(m.InstalledMaps()) != 0 {
		t.Error("records left")
	}
}
