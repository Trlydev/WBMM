package nml

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLiveInstall downloads the real NeoModLoader release. It runs only
// with WBMM_LIVE=1, since it needs the network.
func TestLiveInstall(t *testing.T) {
	if os.Getenv("WBMM_LIVE") == "" {
		t.Skip("set WBMM_LIVE=1 to run")
	}
	dir := t.TempDir()
	if st := Status(dir); st.Installed || st.CheckError != "" {
		t.Fatalf("before: %+v", st)
	}
	name, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := Status(dir)
	if !st.Installed || !st.UpToDate {
		t.Fatalf("after installing %s: %+v", name, st)
	}
	if _, err := os.Stat(filepath.Join(dir, "NeoModLoader.pdb")); err != nil {
		t.Error(err)
	}
	// A changed DLL is no longer up to date; installing again keeps it as
	// the previous version, and Restore brings it back.
	os.WriteFile(filepath.Join(dir, DLLName), []byte("x"), 0o644)
	if Status(dir).UpToDate {
		t.Error("tampered DLL reported up to date")
	}
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if !HasBackup(dir) {
		t.Fatal("no backup kept")
	}
	if err := Restore(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, DLLName)); string(b) != "x" {
		t.Error("Restore didn't bring back the previous version")
	}
}
