package gui

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Trlydev/WBMM/internal/manager"
)

// openWindow shows u in an app-style window (no tabs or address bar) using
// a separate browser profile, or in the default browser as a fallback.
func openWindow(u string) error {
	profile := ""
	if d, err := manager.ConfigDir(); err == nil {
		profile = filepath.Join(d, "window-profile")
	}
	for _, b := range browsers() {
		args := []string{"--app=" + u, "--window-size=1180,800", "--no-first-run", "--no-default-browser-check"}
		if profile != "" {
			args = append(args, "--user-data-dir="+profile)
		}
		if err := exec.Command(b, args...).Start(); err == nil {
			return nil
		}
	}
	return openURL(u)
}

// browsers lists installed Chromium-based browsers, which support --app.
func browsers() []string {
	var out []string
	add := func(p string) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	if p := os.Getenv("WBMM_BROWSER"); p != "" {
		add(p)
	}
	switch runtime.GOOS {
	case "windows":
		for _, rel := range []string{
			`Microsoft\Edge\Application\msedge.exe`,
			`Google\Chrome\Application\chrome.exe`,
			`BraveSoftware\Brave-Browser\Application\brave.exe`,
			`Vivaldi\Application\vivaldi.exe`,
			`Chromium\Application\chrome.exe`,
		} {
			for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
				if base != "" {
					add(filepath.Join(base, rel))
				}
			}
		}
	case "darwin":
		for _, app := range []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Vivaldi", "Chromium"} {
			add(filepath.Join("/Applications", app+".app", "Contents", "MacOS", app))
		}
	default:
		for _, name := range []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge",
			"microsoft-edge-stable", "brave-browser", "brave", "vivaldi", "vivaldi-stable",
		} {
			if p, err := exec.LookPath(name); err == nil {
				add(p)
			}
		}
	}
	return out
}

// openURL opens a web page or a steam:// link with the system's handler.
func openURL(u string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		c = exec.Command("open", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	return c.Start()
}

func openFolder(p string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", p).Start()
	case "darwin":
		return exec.Command("open", p).Start()
	}
	return exec.Command("xdg-open", p).Start()
}

// Only one wbmm window runs at a time: a second start opens the first
// one's page instead.

func runningFile() (string, error) {
	d, err := manager.ConfigDir()
	return filepath.Join(d, "running.json"), err
}

func saveRunning(u string) {
	if p, err := runningFile(); err == nil {
		b, _ := json.Marshal(map[string]string{"url": u})
		os.WriteFile(p, b, 0o600)
	}
}

func clearRunning() {
	if p, err := runningFile(); err == nil {
		os.Remove(p)
	}
}

// existingWindow returns the page of a wbmm that is still serving.
func existingWindow() (string, bool) {
	p, err := runningFile()
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	var r struct{ URL string }
	if json.Unmarshal(b, &r) != nil {
		return "", false
	}
	pu, err := url.Parse(r.URL)
	if err != nil || pu.Hostname() != "127.0.0.1" {
		return "", false
	}
	req, _ := http.NewRequest("POST", pu.Scheme+"://"+pu.Host+"/api/ping", nil)
	req.Header.Set("X-Key", pu.Query().Get("k"))
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return "", false
	}
	resp.Body.Close()
	return r.URL, resp.StatusCode == 200
}
