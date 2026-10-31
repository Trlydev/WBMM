package nml

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUID(t *testing.T) {
	for _, c := range []struct {
		d    Declare
		want string
	}{
		// Keys NML itself wrote for these two mods.
		{Declare{Name: "ModernBox", Author: "Tuxxego"}, "TUXXEGO_MODERNBOX"},
		{Declare{Name: "Optime", Author: "ApexLite"}, "APEXLITE_OPTIME"},
		{Declare{Name: "X", Author: "Y", GUID: "my-mod.v2"}, "MY_MOD_V2"},
		{Declare{Name: "Mod Name!", Author: "a b"}, "A_B_MOD_NAME_"},
		{Declare{Name: "世界", Author: "x"}, "X_世界"},
	} {
		if got := c.d.UID(); got != c.want {
			t.Errorf("UID(%+v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadDeclare(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mod.json")
	// BOM, different key case, and targetGameBuild of an unexpected type.
	write(t, p, "\xef\xbb\xbf{\"Name\":\"A\",\"AUTHOR\":\"B\",\"version\":\"1.0\",\"targetGameBuild\":\"558\"}")
	d, err := ReadDeclare(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "A" || d.Author != "B" || d.Version != "1.0" {
		t.Errorf("got %+v", d)
	}
	write(t, p, `{"author":"B"}`)
	if _, err := ReadDeclare(p); err == nil {
		t.Error("mod.json without a name was accepted")
	}
}

func TestFindDeclareAndModRoots(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "wrap", "Inner", "mod.json"), `{"name":"a"}`)
	if p, ok := FindDeclare(filepath.Join(dir, "wrap")); !ok || p != filepath.Join(dir, "wrap", "Inner", "mod.json") {
		t.Errorf("FindDeclare = %q, %v", p, ok)
	}

	arc := t.TempDir()
	write(t, filepath.Join(arc, "ModA", "mod.json"), `{"name":"a"}`)
	write(t, filepath.Join(arc, "ModA", "Code", "mod.json"), `{"name":"nested"}`) // inside ModA: not its own mod
	write(t, filepath.Join(arc, "ModA Extra", "mod.json"), `{"name":"b"}`)
	write(t, filepath.Join(arc, "readme.txt"), "hi")
	roots := ModRoots(arc)
	want := []string{filepath.Join(arc, "ModA"), filepath.Join(arc, "ModA Extra")}
	if len(roots) != 2 || roots[0] != want[0] || roots[1] != want[1] {
		t.Errorf("ModRoots = %q, want %q", roots, want)
	}
}

func TestSetDisabled(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, RecordsFile), `{
  "TUXXEGO_MODERNBOX": {"dependencies": [], "disabled": false, "mod_id": "TUXXEGO_MODERNBOX",
    "optional_dependencies": [], "compile_environment": "NML_LINUX", "timestamp": 639290707727935690,
    "compile_timestamp": 639290707910346130}
}`)
	if err := SetDisabled(dir, "TUXXEGO_MODERNBOX", true); err != nil {
		t.Fatal(err)
	}
	if err := SetDisabled(dir, "NEW_MOD", true); err != nil {
		t.Fatal(err)
	}
	off, err := Disabled(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !off["TUXXEGO_MODERNBOX"] || !off["NEW_MOD"] || len(off) != 2 {
		t.Errorf("Disabled = %v", off)
	}

	// Everything NML wrote is kept, timestamps to the digit.
	b, _ := os.ReadFile(filepath.Join(dir, RecordsFile))
	var recs map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &recs); err != nil {
		t.Fatal(err)
	}
	r := recs["TUXXEGO_MODERNBOX"]
	if string(r["timestamp"]) != "639290707727935690" || string(r["compile_environment"]) != `"NML_LINUX"` {
		t.Errorf("record changed: %s", b)
	}
	if string(recs["NEW_MOD"]["mod_id"]) != `"NEW_MOD"` {
		t.Errorf("new record: %s", b)
	}

	if err := SetDisabled(dir, "TUXXEGO_MODERNBOX", false); err != nil {
		t.Fatal(err)
	}
	if off, _ := Disabled(dir); off["TUXXEGO_MODERNBOX"] {
		t.Error("still disabled")
	}
}

func TestSetDisabledLegacyList(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, legacyDisabledFile), "A_MOD\nB_MOD\n")
	if off, _ := Disabled(dir); !off["A_MOD"] || !off["B_MOD"] {
		t.Fatalf("legacy list not read: %v", off)
	}
	if err := SetDisabled(dir, "A_MOD", false); err != nil {
		t.Fatal(err)
	}
	if off, _ := Disabled(dir); off["A_MOD"] || !off["B_MOD"] {
		t.Errorf("after enabling A_MOD: %v", off)
	}
}
