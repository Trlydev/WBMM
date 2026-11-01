package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Trlydev/WBMM/internal/archive"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/sources"
)

// downloadDir keeps GameBanana files between the safety check on the
// preview page and the install (a day at most).
func downloadDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	d = filepath.Join(d, "wbmm", "downloads")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	es, _ := os.ReadDir(d)
	for _, e := range es {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(d, e.Name()))
		}
	}
	return d, nil
}

// downloadGB downloads a GameBanana file, or reuses the copy the preview's
// safety check downloaded, and makes sure it is the file GameBanana lists
// (its MD5 checksum). It returns the file and its SHA-256.
func downloadGB(f sources.GBFile) (path, sha string, err error) {
	if !f.FromGameBanana() {
		return "", "", fmt.Errorf("%s doesn't download from GameBanana itself (%s), so wbmm won't fetch it", f.Name, f.DownloadURL)
	}
	dir, err := downloadDir()
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(dir, strconv.Itoa(f.ID))
	if f.MD5 != "" && f.Verify(path) == nil {
		if sha, err := sha256File(path); err == nil {
			return path, sha, nil
		}
	}
	tmp, err := os.CreateTemp(dir, strconv.Itoa(f.ID)+"-*.part")
	if err != nil {
		return "", "", err
	}
	tmp.Close()
	sha, err = sources.Download(f.DownloadURL, tmp.Name(), MaxDownload)
	if err == nil {
		err = f.Verify(tmp.Name())
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("downloading %s: %w", f.Name, err)
	}
	return path, sha, nil
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Precheck is the safety check of a GameBanana file run before installing
// it, from its preview page.
type Precheck struct {
	File    sources.GBFile
	Verdict Verdict
	Mods    []nml.Declare // the mods inside
	// Problem says why it can't be installed as a mod, if it can't.
	Problem string
	// Needs: mods they need (NML IDs) that aren't installed or inside.
	Needs []string
}

// PrecheckGB downloads a GameBanana file (fileID, or the newest when 0),
// unpacks it into a temporary folder and runs the safety check on it,
// without installing anything. Installing the file next reuses the
// download.
func (m *Manager) PrecheckGB(gbID, fileID int) (*Precheck, error) {
	files, err := sources.GBFiles(gbID)
	if err != nil {
		return nil, err
	}
	f, err := PickFile(files, fileID)
	if err != nil {
		return nil, err
	}
	arc, _, err := downloadGB(f)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "wbmm-check-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	pc := &Precheck{File: f}
	if err := archive.Extract(arc, tmp); err != nil {
		pc.Problem = "it can't be unpacked: " + err.Error()
		pc.Verdict = Verdict{Max: -1}
		return pc, nil
	}
	pc.Verdict = m.scanStaged(tmp)
	roots := nml.ModRoots(tmp)
	if len(roots) == 0 {
		pc.Problem = noModError(tmp, f.Name).Error()
	}
	var deps []string
	inside := map[string]bool{}
	for _, r := range roots {
		if d, err := nml.ReadDeclare(filepath.Join(r, nml.DeclFile)); err == nil {
			pc.Mods = append(pc.Mods, d)
			inside[d.UID()] = true
			deps = append(deps, d.Dependencies...)
		}
	}
	for _, d := range m.Missing(deps) {
		if !inside[nml.Symbol(d)] {
			pc.Needs = append(pc.Needs, d)
		}
	}
	return pc, nil
}

// Missing returns the mod IDs among ids that no installed mod has.
func (m *Manager) Missing(ids []string) []string {
	mods, _ := m.List()
	have := map[string]bool{}
	for _, md := range mods {
		if md.Problem == "" {
			have[md.UID] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		sym := nml.Symbol(id)
		if sym != "" && !have[sym] && !seen[sym] {
			seen[sym] = true
			out = append(out, id)
		}
	}
	return out
}

// NeedsOf returns what the mods installed from GameBanana item gbID need
// that isn't installed.
func (m *Manager) NeedsOf(gbID int) []string {
	mods, _ := m.List()
	var deps []string
	for _, md := range mods {
		if md.GBID == gbID && md.Problem == "" {
			deps = append(deps, md.deps...)
		}
	}
	return m.Missing(deps)
}
