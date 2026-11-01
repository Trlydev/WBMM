package manager

import (
	"archive/zip"
	"bytes"
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
		{"flagbox_v8_13-09-2023.zip", "flagbox_v9_02-11-2023.zip"},
		{"mod-1-2-3.zip", "mod-1-3-0.zip"},
		{"Mod (1.2).zip", "Mod (1.3).zip"},
		{"mod_v1.0b.zip", "mod_v1.1.zip"},
		{"1.9.zip", "2.0.zip"},
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

// A Workshop mod whose code names a Discord webhook (a harmless sample:
// nothing runs it) is held: switched off, refused by SetEnabled, and
// allowed only through Accept. If the game is running when it's found, it
// is switched off at the next check after the game closes.
func TestSafetyHold(t *testing.T) {
	m, gd := fakeGame(t)
	ws := filepath.Join(gd, "..", "..", "workshop", "content", game.AppID, "3999999999")
	write(t, filepath.Join(ws, "mod.json"), `{"name":"Sneaky","author":"Bad","version":"1"}`)
	write(t, filepath.Join(ws, "Code", "A.cs"), `class A { string u = "https://discord.com/api/webhooks/1/x"; }`)
	md, err := m.Find("BAD_SNEAKY")
	if err != nil {
		t.Fatal(err)
	}

	gameRunning = func() bool { return true }
	v, err := m.Check(md)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Blocked() || v.Held {
		t.Fatalf("while the game runs: blocked %v, held %v", v.Blocked(), v.Held)
	}
	gameRunning = func() bool { return false }
	defer func() { gameRunning = game.Running }()
	held, err := m.CheckAll()
	if err != nil || len(held) != 1 {
		t.Fatalf("after the game closed: held %v, %v", held, err)
	}
	if again, _ := m.CheckAll(); len(again) != 0 {
		t.Errorf("an already held mod was reported again: %v", again)
	}
	if off, _ := nml.Disabled(game.NMLDir(gd)); !off["BAD_SNEAKY"] {
		t.Error("not switched off in NML's list")
	}
	if err := m.SetEnabled("BAD_SNEAKY", true); err == nil {
		t.Error("a held mod was switched on")
	}
	if err := m.Accept(md); err != nil {
		t.Fatal(err)
	}
	if off, _ := nml.Disabled(game.NMLDir(gd)); off["BAD_SNEAKY"] {
		t.Error("still off after Accept")
	}

	// A changed mod is checked again; the acceptance was for the old files.
	write(t, filepath.Join(ws, "Code", "B.cs"), `class B { string u = "https://api.telegram.org/bot1/x"; }`)
	if held, _ := m.CheckAll(); len(held) != 1 {
		t.Errorf("changed mod not held again: %v", held)
	}

	// The everyday mods in the fake game pass.
	for _, uid := range []string{"TUXXEGO_MODERNBOX", "APEXLITE_OPTIME"} {
		md, _ := m.Find(uid)
		if v, ok := m.Verdict(md.Folder); !ok || v.Blocked() {
			t.Errorf("%s: %+v, %v", uid, v, ok)
		}
	}
}

func TestDependencies(t *testing.T) {
	m, gd := fakeGame(t)
	write(t, filepath.Join(gd, "Mods", "Addon", "mod.json"),
		`{"name":"Addon","author":"Me","Dependencies":["Tuxxego.ModernBox","someone.missing_lib"],"IncompatibleWith":["ApexLite.Optime"]}`)
	md, err := m.Find("ME_ADDON")
	if err != nil {
		t.Fatal(err)
	}
	if len(md.Needs) != 1 || md.Needs[0] != "someone.missing_lib" {
		t.Errorf("needs: %v", md.Needs)
	}
	if len(md.Clashes) != 1 || md.Clashes[0] != "Optime" {
		t.Errorf("clashes: %v", md.Clashes)
	}
}

func TestWorlds(t *testing.T) {
	m, gd := fakeGame(t)
	ws := filepath.Join(gd, "..", "..", "workshop", "content", game.AppID)
	write(t, filepath.Join(ws, "2686372543", "map.meta"), `{"saveVersion":16,"mapStats":{"name":"Pangaea"}}`)
	write(t, filepath.Join(ws, "2686372543", "preview.png"), "png")
	worlds, err := m.Worlds()
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) != 1 || worlds[0].Name != "Pangaea" || worlds[0].Preview == "" {
		t.Errorf("worlds: %+v", worlds)
	}
}

// A held mod another enabled mod needs: NML would switch it back on, so
// the mod that needs it is switched off, and a held mod found on again is
// switched off again.
func TestHeldDependency(t *testing.T) {
	m, gd := fakeGame(t)
	gameRunning = func() bool { return false }
	defer func() { gameRunning = game.Running }()
	write(t, filepath.Join(gd, "Mods", "Lib", "mod.json"), `{"name":"Lib","author":"X"}`)
	write(t, filepath.Join(gd, "Mods", "Lib", "A.cs"), `class A { string u = "https://discord.com/api/webhooks/1/x"; }`)
	write(t, filepath.Join(gd, "Mods", "App", "mod.json"), `{"name":"App","author":"X","Dependencies":["X.Lib"]}`)
	if _, err := m.CheckAll(); err != nil {
		t.Fatal(err)
	}
	off, _ := nml.Disabled(game.NMLDir(gd))
	if !off["X_LIB"] || !off["X_APP"] {
		t.Fatalf("off: %v", off)
	}
	// Something switches the held mod back on (NML, the in-game window).
	nml.SetDisabled(game.NMLDir(gd), "X_LIB", false)
	if held, _ := m.CheckAll(); len(held) != 1 {
		t.Errorf("re-held: %v", held)
	}
	if off, _ := nml.Disabled(game.NMLDir(gd)); !off["X_LIB"] {
		t.Error("held mod left on")
	}
	// Changed files: enabling checks them first.
	write(t, filepath.Join(gd, "Mods", "Lib", "B.cs"), `class B {}`)
	if err := m.SetEnabled("X_LIB", true); err == nil {
		t.Error("a held mod with changed files was switched on unchecked")
	}
}

// The window and a command line run share state.json: neither undoes the
// other. A damaged file is kept aside, not overwritten.
func TestStateSharedAndDamaged(t *testing.T) {
	m1, _ := fakeGame(t)
	m2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := m1.SetUpdates(false, true); err != nil {
		t.Fatal(err)
	}
	if err := m2.SetGameDir(""); err != nil {
		t.Fatal(err)
	}
	m3, _ := Open()
	if c := m3.Config(); !c.NoModUpdates {
		t.Errorf("one process undid the other's change: %+v", c)
	}

	p, _ := statePath()
	os.WriteFile(p, []byte(`{"config":{"noModUpd`), 0o600)
	m4, err := Open()
	if err == nil {
		t.Error("damaged state.json not reported")
	}
	if c := m4.Config(); c.NoModUpdates {
		t.Errorf("damaged file read: %+v", c)
	}
	bad, _ := filepath.Glob(p + ".bad-*")
	if len(bad) != 1 {
		t.Errorf("damaged file not kept: %v", bad)
	}
}

// NML also loads StreamingAssets/mods folders and unpacks zips dropped in
// Mods: both are checked; a risky zip is renamed so NML skips it.
func TestOtherPlacesNMLLoads(t *testing.T) {
	m, gd := fakeGame(t)
	gameRunning = func() bool { return false }
	defer func() { gameRunning = game.Running }()
	write(t, filepath.Join(game.NativeModsDir(gd), "Native", "mod.json"), `{"name":"Native","author":"Y"}`)
	if md, err := m.Find("Y_NATIVE"); err != nil || md.Source != "native" {
		t.Errorf("native mod: %+v, %v", md, err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("Bad/mod.json")
	w.Write([]byte(`{"name":"Bad","author":"Z"}`))
	w, _ = zw.Create("Bad/A.cs")
	w.Write([]byte(`class A { string u = "https://api.telegram.org/bot1/x"; }`))
	zw.Close()
	zp := filepath.Join(gd, "Mods", "bad.zip")
	os.WriteFile(zp, buf.Bytes(), 0o644)
	held, err := m.CheckAll()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(zp + ".wbmm-held"); err != nil || len(held) == 0 {
		t.Errorf("risky zip not held: %v, %v", held, err)
	}
}
