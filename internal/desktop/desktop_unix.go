//go:build !windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func home() string { h, _ := os.UserHomeDir(); return h }

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local", "share")
}

// InstallPath is ~/.local/bin/wbmm.
func InstallPath() string { return filepath.Join(home(), ".local", "bin", "wbmm") }

func desktopDir() string {
	if out, err := exec.Command("xdg-user-dir", "DESKTOP").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" && d != home() {
			return d
		}
	}
	return filepath.Join(home(), "Desktop")
}

func entry(exe string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
GenericName=WorldBox mod manager
Comment=Install and update WorldBox mods from GameBanana and the Steam Workshop
Exec=%s gui
Icon=wbmm
Terminal=false
Categories=Game;Utility;
Keywords=WorldBox;mods;workshop;GameBanana;NeoModLoader;
StartupNotify=true
`, AppName, execQuote(exe))
}

// execQuote quotes a path for a desktop entry's Exec key, as the desktop
// entry spec asks: in double quotes with " ` $ \ escaped by a backslash,
// then backslashes doubled (the string escaping applied first) and % as %%.
func execQuote(p string) string {
	var b strings.Builder
	for _, c := range p {
		if strings.ContainsRune("\"`$\\", c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	q := `"` + b.String() + `"`
	return strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), "%", "%%")
}

func paths() (menu, desk, png, svg string) {
	d := dataHome()
	return filepath.Join(d, "applications", "wbmm.desktop"),
		filepath.Join(desktopDir(), "wbmm.desktop"),
		filepath.Join(d, "icons", "hicolor", "256x256", "apps", "wbmm.png"),
		filepath.Join(d, "icons", "hicolor", "scalable", "apps", "wbmm.svg")
}

// Install copies the program to ~/.local/bin and adds an app-menu entry, an
// icon and optionally a desktop shortcut. It returns the installed path.
func Install(o Options, logf func(string, ...any)) (string, error) {
	exe := InstallPath()
	if err := copyExe(exe); err != nil {
		return "", err
	}
	logf("installed %s", exe)
	menu, desk, png, svg := paths()
	for p, b := range map[string][]byte{png: iconPNG, svg: iconSVG} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return exe, err
		}
	}
	os.MkdirAll(filepath.Dir(menu), 0o755)
	if err := os.WriteFile(menu, []byte(entry(exe)), 0o755); err != nil {
		return exe, err
	}
	logf("added %s to the application menu", AppName)
	if o.DesktopShortcut {
		if err := os.MkdirAll(filepath.Dir(desk), 0o755); err == nil {
			if err := os.WriteFile(desk, []byte(entry(exe)), 0o755); err == nil {
				// GNOME only launches desktop files marked trusted.
				exec.Command("gio", "set", desk, "metadata::trusted", "true").Run()
				logf("added a desktop shortcut")
			}
		}
	}
	exec.Command("update-desktop-database", filepath.Dir(menu)).Run()
	exec.Command("gtk-update-icon-cache", "-q", filepath.Join(dataHome(), "icons", "hicolor")).Run()
	return exe, nil
}

// Uninstall removes the program, menu entry, icons and desktop shortcut.
// Mods and wbmm's data folder are left alone.
func Uninstall(logf func(string, ...any)) error {
	menu, desk, png, svg := paths()
	for _, p := range []string{menu, desk, png, svg, InstallPath(), InstallPath() + ".old"} {
		if err := os.Remove(p); err == nil {
			logf("removed %s", p)
		}
	}
	exec.Command("update-desktop-database", filepath.Dir(menu)).Run()
	return nil
}

// Launch starts exe detached from this process.
func Launch(exe string, args ...string) error {
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}
