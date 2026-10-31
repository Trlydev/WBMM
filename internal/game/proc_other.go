//go:build !windows

package game

import (
	"os"
	"path/filepath"
	"strings"
)

func registrySteamPath() string { return "" }

// Running reports whether WorldBox is running (the native Linux build or the
// Windows build under Proton). NeoModLoader rewrites its records while the
// game runs, so mods are only switched on and off with the game closed.
func Running() bool {
	comms, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, c := range comms {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(string(b))) {
		case "worldbox", "worldbox.exe":
			return true
		}
	}
	return false
}
