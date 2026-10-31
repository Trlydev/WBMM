package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/sources"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeGame lays out a Steam library with WorldBox, two local mods and the
// Workshop cache, and points wbmm's settings at a temporary folder.
func fakeGame(t *testing.T) (*Manager, string) {
	t.Helper()
	lib := t.TempDir()
	gd := filepath.Join(lib, "steamapps", "common", "worldbox")
	os.MkdirAll(game.NMLDir(gd), 0o755)
	write(t, filepath.Join(gd, "Mods", "TUXXEGO_MODERNBOX", "mod.json"), `{"name":"ModernBox","author":"Tuxxego","version":"5.01","iconPath":"Icon.jpg"}`)
	write(t, filepath.Join(gd, "Mods", "TUXXEGO_MODERNBOX", "icon.jpg"), "jpg")
	write(t, filepath.Join(gd, "Mods", "Wrapped", "Inner", "mod.json"), `{"name":"Wrapped","author":"Me"}`)
	write(t, filepath.Join(gd, "Mods", "notamod", "readme.txt"), "")
	ws := filepath.Join(lib, "steamapps", "workshop", "content", game.AppID)
	write(t, filepath.Join(ws, "3387086485", "mod.json"), `{"name":"Optime","author":"ApexLite","version":"0.5.0"}`)
	write(t, filepath.Join(ws, "2686372543", "map.wbox"), "")                                        // a world, not a mod
	write(t, filepath.Join(ws, "3139755386", "mod.json"), `{"name":"ModernBox","author":"Tuxxego"}`) // same mod as the local copy

	t.Setenv("WBMM_GAME_DIR", gd)
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("AppData", cfg)
	t.Setenv("HOME", cfg)
	m, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return m, gd
}

func TestList(t *testing.T) {
	m, _ := fakeGame(t)
	mods, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Mod{}
	for _, md := range mods {
		got[md.Source+":"+md.Name] = md
	}
	if len(mods) != 4 {
		t.Fatalf("got %d mods: %+v", len(mods), mods)
	}
	mb := got["local:ModernBox"]
	if mb.UID != "TUXXEGO_MODERNBOX" || !mb.Enabled || !mb.HasIcon || mb.Problem != "" {
		t.Errorf("local ModernBox: %+v", mb)
	}
	if w := got["local:Wrapped"]; w.UID != "ME_WRAPPED" {
		t.Errorf("wrapped mod: %+v", w)
	}
	if o := got["workshop:Optime"]; o.WorkshopID != "3387086485" || o.UID != "APEXLITE_OPTIME" {
		t.Errorf("workshop Optime: %+v", o)
	}
	if d := got["workshop:ModernBox"]; d.Problem == "" {
		t.Error("the Workshop copy of a mod already in Mods isn't flagged")
	}
}

func TestEnableRemove(t *testing.T) {
	m, gd := fakeGame(t)
	gameRunning = func() bool { return true }
	if err := m.SetEnabled("APEXLITE_OPTIME", false); err != ErrGameRunning {
		t.Errorf("with the game running: %v", err)
	}
	gameRunning = func() bool { return false }
	defer func() { gameRunning = game.Running }()
	if err := m.SetEnabled("APEXLITE_OPTIME", false); err != nil {
		t.Fatal(err)
	}
	if off, _ := nml.Disabled(game.NMLDir(gd)); !off["APEXLITE_OPTIME"] {
		t.Error("not disabled in NML's records")
	}
	md, _ := m.Find("APEXLITE_OPTIME")
	if md.Enabled {
		t.Error("listed as on")
	}

	if err := m.Remove("APEXLITE_OPTIME"); err == nil {
		t.Error("removed a Workshop mod")
	}
	if err := m.Remove("TUXXEGO_MODERNBOX"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(gd, "Mods", "TUXXEGO_MODERNBOX")); !os.IsNotExist(err) {
		t.Error("folder still there")
	}
}

func TestReplaceDir(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "new"), filepath.Join(dir, "mod")
	write(t, filepath.Join(src, "a"), "new")
	write(t, filepath.Join(dst, "a"), "old")
	write(t, filepath.Join(dst, "stale"), "old")
	if err := replaceDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a")); string(b) != "new" {
		t.Errorf("a = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale")); !os.IsNotExist(err) {
		t.Error("old files left behind")
	}
	if _, err := os.Stat(dst + ".wbmm-old"); !os.IsNotExist(err) {
		t.Error("backup left behind")
	}
}

func TestFileLine(t *testing.T) {
	same := [][2]string{
		{"warbox010.zip", "warbox050.zip"},
		{"powerbox.zip", "powerbox_cfdfc.zip"},
		{"MyMod_v1.2.zip", "mymod-v2.0.7.7z"},
		{"modernbox-m5.zip", "modernbox-m5-2.zip"},
	}
	for _, c := range same {
		if fileLine(c[0]) != fileLine(c[1]) {
			t.Errorf("%s and %s should be one line (%q, %q)", c[0], c[1], fileLine(c[0]), fileLine(c[1]))
		}
	}
	different := [][2]string{
		{"modernbox-m1.zip", "modernbox-m5.zip"},
		{"modernbox-m1.zip", "modernbox-m1-new.zip"},
		{"ancientbox.zip", "medievalbox.zip"},
	}
	for _, c := range different {
		if fileLine(c[0]) == fileLine(c[1]) {
			t.Errorf("%s and %s should be different lines (%q)", c[0], c[1], fileLine(c[0]))
		}
	}
}

func TestNewerOnLine(t *testing.T) {
	files := []sources.GBFile{ // newest first, as GBFiles returns them
		{ID: 5, Name: "modernbox-m5-new.zip", Added: 500},
		{ID: 4, Name: "modernbox-m1-new.zip", Added: 400},
		{ID: 3, Name: "modernbox-m1.zip", Added: 300},
		{ID: 2, Name: "modernbox-m5.zip", Added: 200},
		{ID: 1, Name: "modernbox-m1.zip", Added: 100},
	}
	// m1 installed: the newer m1 file, not the newest file (m5).
	if f, ok := newerOnLine(files, Record{FileID: 1, FileName: "modernbox-m1.zip", FileAdded: 100}); !ok || f.ID != 3 {
		t.Errorf("m1: got %+v, %v", f, ok)
	}
	if _, ok := newerOnLine(files, Record{FileID: 3, FileName: "modernbox-m1.zip", FileAdded: 300}); ok {
		t.Error("newest of its line reported as outdated")
	}
}
