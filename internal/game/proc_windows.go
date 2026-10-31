//go:build windows

package game

import (
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return c
}

var reRegSZ = regexp.MustCompile(`SteamPath\s+REG_SZ\s+(.+)`)

// registrySteamPath reads Steam's install folder from the registry, for
// Steam installed outside Program Files.
func registrySteamPath() string {
	out, err := hidden("reg", "query", `HKCU\Software\Valve\Steam`, "/v", "SteamPath").Output()
	if err != nil {
		return ""
	}
	if m := reRegSZ.FindStringSubmatch(string(out)); m != nil {
		return strings.ReplaceAll(strings.TrimSpace(m[1]), "/", `\`)
	}
	return ""
}

// Running reports whether WorldBox is running. NeoModLoader rewrites its
// records while the game runs, so mods are only switched on and off with
// the game closed.
func Running() bool {
	out, err := hidden("tasklist", "/FI", "IMAGENAME eq worldbox.exe", "/NH").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "worldbox.exe")
}
