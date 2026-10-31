// Package game locates the WorldBox install, its Mods folders and the Steam
// Workshop cache on Windows, Linux and macOS.
package game

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// AppID is WorldBox's Steam app ID.
const AppID = "1206560"

var reVDFPath = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// SteamRoots returns plausible Steam install folders for this OS.
func SteamRoots() []string {
	var roots []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if v := os.Getenv(env); v != "" {
				roots = append(roots, filepath.Join(v, "Steam"))
			}
		}
		roots = append(roots, `C:\Program Files (x86)\Steam`)
		if p := registrySteamPath(); p != "" {
			roots = append(roots, p)
		}
	case "darwin":
		roots = append(roots, filepath.Join(home, "Library", "Application Support", "Steam"))
	default:
		roots = append(roots,
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
			filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),
		)
	}
	return existing(roots)
}

// Libraries returns every Steam library folder (each holds a steamapps dir).
func Libraries() []string {
	seen := map[string]bool{}
	var libs []string
	add := func(p string) {
		if abs, err := filepath.EvalSymlinks(p); err == nil {
			p = abs
		}
		if !seen[p] {
			seen[p] = true
			libs = append(libs, p)
		}
	}
	for _, root := range SteamRoots() {
		add(root)
		b, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, m := range reVDFPath.FindAllStringSubmatch(string(b), -1) {
			p := strings.ReplaceAll(m[1], `\\`, `\`)
			if _, err := os.Stat(filepath.Join(p, "steamapps")); err == nil {
				add(p)
			}
		}
	}
	return libs
}

// FindGameDir returns the WorldBox install folder. override (from settings
// or the WBMM_GAME_DIR environment variable) wins when set.
func FindGameDir(override string) (string, error) {
	if override == "" {
		override = os.Getenv("WBMM_GAME_DIR")
	}
	if override != "" {
		if !IsGameDir(override) {
			return "", errors.New("that folder doesn't look like a WorldBox install (no worldbox_Data inside)")
		}
		return override, nil
	}
	for _, lib := range Libraries() {
		p := filepath.Join(lib, "steamapps", "common", "worldbox")
		if IsGameDir(p) {
			return p, nil
		}
	}
	return "", errors.New("WorldBox not found; set the game folder in Setup")
}

// IsGameDir reports whether dir holds a WorldBox install.
func IsGameDir(dir string) bool {
	st, err := os.Stat(StreamingAssets(dir))
	return err == nil && st.IsDir()
}

// StreamingAssets is the game's StreamingAssets folder.
func StreamingAssets(gameDir string) string {
	if runtime.GOOS == "darwin" {
		if app, _ := filepath.Glob(filepath.Join(gameDir, "*.app")); len(app) > 0 {
			return filepath.Join(app[0], "Contents", "Resources", "Data", "StreamingAssets")
		}
	}
	return filepath.Join(gameDir, "worldbox_Data", "StreamingAssets")
}

// ManagedDir holds the game's own .NET libraries.
func ManagedDir(gameDir string) string {
	return filepath.Join(filepath.Dir(StreamingAssets(gameDir)), "Managed")
}

// ModsDir is where NeoModLoader looks for local mods (one folder per mod).
func ModsDir(gameDir string) string { return filepath.Join(gameDir, "Mods") }

// NativeModsDir is the game's own mods folder, where NeoModLoader.dll lives.
func NativeModsDir(gameDir string) string { return filepath.Join(StreamingAssets(gameDir), "mods") }

// NMLDir holds NeoModLoader's cache and its mod on/off records.
func NMLDir(gameDir string) string { return filepath.Join(NativeModsDir(gameDir), "NML") }

// WorkshopDir is the Workshop cache NeoModLoader loads subscribed mods from:
// <library>/steamapps/workshop/content/1206560, next to common/worldbox.
func WorkshopDir(gameDir string) string {
	return filepath.Clean(filepath.Join(gameDir, "..", "..", "workshop", "content", AppID))
}

func existing(ps []string) []string {
	var out []string
	for _, p := range ps {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
