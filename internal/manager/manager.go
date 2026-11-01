// Package manager installs, lists, updates and switches WorldBox mods. It
// works with NeoModLoader's layout: local mods are folders in
// <game>/Mods, Workshop mods load straight from Steam's Workshop cache, and
// on/off state lives in NML's records.
package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Trlydev/WBMM/internal/archive"
	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/scan"
	"github.com/Trlydev/WBMM/internal/sources"
)

// MaxDownload is the largest mod archive wbmm downloads. The biggest
// WorldBox mods (ModernBox) are around 100 MB.
const MaxDownload = 1 << 30

var (
	ErrNoGame      = errors.New("WorldBox wasn't found; set its folder in Setup")
	ErrGameRunning = errors.New("close WorldBox first: NeoModLoader rewrites its mod list while the game runs")
)

// gameRunning is game.Running, replaced in tests.
var gameRunning = game.Running

// NotCleanError means GameBanana's virus scan hasn't passed the file.
type NotCleanError struct{ File sources.GBFile }

func (e *NotCleanError) Error() string {
	res := e.File.AVResult
	if res == "" {
		res = e.File.AVState
	}
	if res == "" {
		res = "not scanned yet"
	}
	return fmt.Sprintf("GameBanana's virus scan hasn't passed %s (%s)", e.File.Name, res)
}

// Mod is one mod NeoModLoader will see.
type Mod struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	Author      string `json:"author"`
	Version     string `json:"version"`
	Description string `json:"description"`
	// Source is "gamebanana", "workshop" or "local" (put there by hand or
	// by another tool).
	Source     string `json:"source"`
	Folder     string `json:"folder"`
	WorkshopID string `json:"workshopId,omitempty"`
	GBID       int    `json:"gbId,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url,omitempty"`
	Icon       string `json:"-"`
	HasIcon    bool   `json:"hasIcon"`
	// Problem explains why NML won't load it as listed.
	Problem string `json:"problem,omitempty"`
	// Needs lists required mods (UIDs) that aren't installed and switched
	// on; Clashes, mods it says it can't run with that are.
	Needs   []string `json:"needs,omitempty"`
	Clashes []string `json:"clashes,omitempty"`

	deps, incompatible []string
}

type Manager struct {
	mu      sync.Mutex
	st      *state
	gameDir string
	gameErr error
}

// Open loads wbmm's state and finds the game.
func Open() (*Manager, error) {
	st, err := loadState()
	m := &Manager{st: st}
	m.findGame()
	return m, err
}

func (m *Manager) findGame() {
	m.gameDir, m.gameErr = game.FindGameDir(m.st.Config.GameDir)
}

// GameDir returns the WorldBox folder, or ErrNoGame.
func (m *Manager) GameDir() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.game()
}

func (m *Manager) game() (string, error) {
	if m.gameDir == "" {
		if m.gameErr != nil {
			return "", m.gameErr
		}
		return "", ErrNoGame
	}
	return m.gameDir, nil
}

// SetGameDir saves the game folder ("" goes back to finding it through
// Steam).
func (m *Manager) SetGameDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir != "" && !game.IsGameDir(dir) {
		return errors.New("that folder doesn't look like a WorldBox install (no worldbox_Data inside)")
	}
	return m.update(func(st *state) {
		st.Config.GameDir = dir
		m.findGame()
	})
}

func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.Config
}

// SetUpdates saves the automatic update settings.
func (m *Manager) SetUpdates(mods, app bool) error {
	return m.update(func(st *state) {
		st.Config.NoModUpdates, st.Config.NoAppUpdates = !mods, !app
	})
}

// List returns every mod NML will find: local mods first (NML loads them
// first), then subscribed Workshop mods.
func (m *Manager) List() ([]Mod, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	gd, err := m.game()
	if err != nil {
		return nil, err
	}
	disabled, _ := nml.Disabled(game.NMLDir(gd))
	byFolder := map[string]Record{}
	for _, r := range m.st.Records {
		byFolder[r.Folder] = r
	}
	seen := map[string]bool{}
	var out []Mod
	add := func(dir string) (Mod, bool) {
		decl, ok := nml.FindDeclare(dir)
		if !ok {
			return Mod{}, false
		}
		md := Mod{Folder: dir, Name: filepath.Base(dir)}
		d, err := nml.ReadDeclare(decl)
		if err != nil {
			md.Problem = "mod.json can't be read: " + err.Error()
			return md, true
		}
		md.UID, md.Name, md.Author, md.Version, md.Description = d.UID(), d.Name, d.Author, d.Version, d.Description
		md.deps, md.incompatible = d.Dependencies, d.Incompatible
		md.Enabled = !disabled[md.UID]
		md.URL = d.RepoURL
		md.Icon = findIcon(filepath.Dir(decl), d.IconPath)
		md.HasIcon = md.Icon != ""
		if seen[md.UID] {
			md.Problem = "another copy of this mod is already loaded; NML skips this one"
		}
		seen[md.UID] = true
		return md, true
	}
	for _, e := range readDirs(game.ModsDir(gd)) {
		md, ok := add(filepath.Join(game.ModsDir(gd), e))
		if !ok {
			continue
		}
		md.Source = "local"
		if r, ok := byFolder[e]; ok && r.UID == md.UID {
			md.Source, md.GBID, md.FileName = "gamebanana", r.GBID, r.FileName
			md.URL = fmt.Sprintf("https://gamebanana.com/mods/%d", r.GBID)
		}
		out = append(out, md)
	}
	// NML also loads mod folders from the game's own mods folder.
	for _, e := range readDirs(game.NativeModsDir(gd)) {
		if e == "NML" {
			continue
		}
		md, ok := add(filepath.Join(game.NativeModsDir(gd), e))
		if !ok {
			continue
		}
		md.Source = "native"
		out = append(out, md)
	}
	for _, e := range readDirs(game.WorkshopDir(gd)) {
		md, ok := add(filepath.Join(game.WorkshopDir(gd), e))
		if !ok {
			continue // a world map or a loader, not a mod
		}
		md.Source, md.WorkshopID = "workshop", e
		md.URL = "https://steamcommunity.com/sharedfiles/filedetails/?id=" + e
		out = append(out, md)
	}
	checkDependencies(out)
	return out, nil
}

// checkDependencies fills in what each switched-on mod needs and clashes
// with, the way NML resolves them: by UID, among mods that will load.
func checkDependencies(mods []Mod) {
	installed := map[string]bool{} // NML switches on what an enabled mod needs
	on := map[string]string{}      // UID -> name
	for _, md := range mods {
		if md.Problem == "" {
			installed[md.UID] = true
			if md.Enabled {
				on[md.UID] = md.Name
			}
		}
	}
	for i := range mods {
		md := &mods[i]
		if !md.Enabled || md.Problem != "" {
			continue
		}
		for _, d := range md.deps {
			if uid := nml.Symbol(d); uid != "" && !installed[uid] {
				md.Needs = append(md.Needs, d)
			}
		}
		for _, d := range md.incompatible {
			if name := on[nml.Symbol(d)]; name != "" {
				md.Clashes = append(md.Clashes, name)
			}
		}
	}
}

// Find returns the listed mod with uid, preferring the copy NML loads.
func (m *Manager) Find(uid string) (Mod, error) {
	mods, err := m.List()
	if err != nil {
		return Mod{}, err
	}
	for _, md := range mods {
		if md.UID == uid && md.Problem == "" {
			return md, nil
		}
	}
	return Mod{}, fmt.Errorf("no installed mod %q", uid)
}

// Subscribed returns the Workshop IDs in Steam's Workshop cache.
func (m *Manager) Subscribed() map[string]bool {
	gd, err := m.GameDir()
	out := map[string]bool{}
	if err != nil {
		return out
	}
	for _, e := range readDirs(game.WorkshopDir(gd)) {
		out[e] = true
	}
	return out
}

// InstalledGB returns the GameBanana mods wbmm installed.
func (m *Manager) InstalledGB() map[int]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int]bool{}
	for _, r := range m.st.Records {
		out[r.GBID] = true
	}
	return out
}

// InstalledFiles returns the GameBanana file IDs of a mod that are
// installed (one per mod inside it).
func (m *Manager) InstalledFiles(gbID int) map[int]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int]bool{}
	for _, r := range m.st.Records {
		if r.GBID == gbID {
			out[r.FileID] = true
		}
	}
	return out
}

// SetEnabled turns a mod on or off for the game's next start. A mod the
// safety check holds is only switched on through Accept.
func (m *Manager) SetEnabled(uid string, enabled bool) error {
	if gameRunning() {
		return ErrGameRunning
	}
	if enabled {
		if md, err := m.Find(uid); err == nil {
			// Check its current files first (a changed mod's old verdict
			// says nothing about what would run now).
			v, _, err := m.check(md)
			if err != nil {
				return fmt.Errorf("couldn't check %s: %w", md.Name, err)
			}
			if v.Blocked() {
				return fmt.Errorf("the safety check is holding %s: review it first", md.Name)
			}
		}
	}
	gd, err := m.GameDir()
	if err != nil {
		return err
	}
	return nml.SetDisabled(game.NMLDir(gd), uid, !enabled)
}

// Remove deletes a local mod. Workshop mods are removed by unsubscribing
// in Steam.
func (m *Manager) Remove(uid string) error {
	md, err := m.Find(uid)
	if err != nil {
		return err
	}
	if md.Source == "workshop" {
		return errors.New("Workshop mods are removed by unsubscribing in Steam")
	}
	gd, err := m.GameDir()
	if err != nil {
		return err
	}
	if filepath.Dir(md.Folder) != game.ModsDir(gd) {
		return fmt.Errorf("%s is not in the Mods folder", md.Folder)
	}
	if err := os.RemoveAll(md.Folder); err != nil {
		return err
	}
	return m.update(func(st *state) {
		delete(st.Records, uid)
		delete(st.Verdicts, md.Folder)
	})
}

// PickFile returns the file to install: fileID, or the newest when 0.
func PickFile(files []sources.GBFile, fileID int) (sources.GBFile, error) {
	if len(files) == 0 {
		return sources.GBFile{}, errors.New("this GameBanana mod has no files")
	}
	if fileID == 0 {
		return files[0], nil
	}
	for _, f := range files {
		if f.ID == fileID {
			return f, nil
		}
	}
	return sources.GBFile{}, fmt.Errorf("file %d is not part of this mod", fileID)
}

// InstallGB downloads a GameBanana mod file (the newest when fileID is 0),
// checks it with the malware scanner, and puts each mod inside it into the
// Mods folder, replacing an older copy of the same mod. force installs a
// file GameBanana's virus scan hasn't passed; acceptRisk one wbmm's scanner
// found CRITICAL things in (a *RiskError otherwise). It returns the names
// of the installed mods.
func (m *Manager) InstallGB(gbID, fileID int, force, acceptRisk bool) ([]string, error) {
	gd, err := m.GameDir()
	if err != nil {
		return nil, err
	}
	mod, err := sources.GBGetMod(gbID)
	if err != nil {
		return nil, err
	}
	if mod.IsMap() {
		return nil, errors.New("this is a world map, not a mod; download it from its page")
	}
	files, err := sources.GBFiles(gbID)
	if err != nil {
		return nil, err
	}
	f, err := PickFile(files, fileID)
	if err != nil {
		return nil, err
	}
	if !f.Clean() && !force {
		return nil, &NotCleanError{File: f}
	}

	// Stage next to Mods (same disk, so the final move is a rename) but
	// outside it, so NML never loads a half-unpacked mod.
	stage, err := os.MkdirTemp(gd, ".wbmm-staging-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	arc := filepath.Join(stage, "download")
	sum, err := sources.Download(f.DownloadURL, arc, MaxDownload)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", f.Name, err)
	}
	unpacked := filepath.Join(stage, "files")
	if err := os.Mkdir(unpacked, 0o755); err != nil {
		return nil, err
	}
	if err := archive.Extract(arc, unpacked); err != nil {
		return nil, fmt.Errorf("unpacking %s: %w", f.Name, err)
	}
	roots := nml.ModRoots(unpacked)
	if len(roots) == 0 {
		return nil, noModError(unpacked, f.Name)
	}
	verdict := m.scanStaged(unpacked)
	if verdict.Blocked() && !acceptRisk {
		return nil, &RiskError{Name: mod.Name, Findings: verdict.Findings}
	}

	modsDir := game.ModsDir(gd)
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return nil, err
	}
	var names []string
	recs := map[string]Record{}
	verdicts := map[string]Verdict{}
	for _, root := range roots {
		d, err := nml.ReadDeclare(filepath.Join(root, nml.DeclFile))
		if err != nil {
			return names, fmt.Errorf("%s: mod.json can't be read: %w", f.Name, err)
		}
		uid := d.UID()
		m.mu.Lock()
		folder := m.folderFor(modsDir, uid)
		m.mu.Unlock()
		dst := filepath.Join(modsDir, folder)
		if err := replaceDir(root, dst); err != nil {
			return names, err
		}
		v := verdict
		v.Fingerprint, v.Accepted = fingerprint(dst), verdict.Blocked()
		verdicts[dst] = v
		recs[uid] = Record{
			UID: uid, Folder: folder, Name: d.Name, GBID: gbID,
			FileID: f.ID, FileName: f.Name, FileAdded: f.Added, SHA256: sum,
			Installed: time.Now(),
		}
		names = append(names, d.Name)
	}
	err = m.update(func(st *state) {
		// Mods an older file of this GameBanana item installed that this
		// file no longer has (renamed, or dropped) go, or NML would load
		// both and the update would be offered for ever.
		for uid, r := range st.Records {
			if _, kept := recs[uid]; r.GBID == gbID && !kept {
				old := filepath.Join(modsDir, r.Folder)
				if r.Folder != "" && filepath.Dir(old) == modsDir {
					os.RemoveAll(old)
				}
				delete(st.Records, uid)
				delete(st.Verdicts, old)
			}
		}
		for uid, r := range recs {
			st.Records[uid] = r
		}
		for k, v := range verdicts {
			st.Verdicts[k] = v
		}
		delete(st.Skipped, gbID)
	})
	return names, err
}

// folderFor returns the Mods subfolder for uid: the one an installed copy
// already uses, else the UID itself (as NML names folders it unzips).
func (m *Manager) folderFor(modsDir, uid string) string {
	if r, ok := m.st.Records[uid]; ok && r.Folder != "" {
		return r.Folder
	}
	for _, e := range readDirs(modsDir) {
		if decl, ok := nml.FindDeclare(filepath.Join(modsDir, e)); ok {
			if d, err := nml.ReadDeclare(decl); err == nil && d.UID() == uid {
				return e
			}
		}
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, uid)
	if archive.WindowsUnsafe(name) || name == "" {
		name = "mod_" + name
	}
	return name
}

// replaceDir moves src to dst, replacing what's there only once the new
// copy is in place.
func replaceDir(src, dst string) error {
	old := dst + ".wbmm-old"
	os.RemoveAll(old)
	hadOld := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("can't replace %s (is the game or a file manager using it?): %w", filepath.Base(dst), err)
		}
		hadOld = true
	}
	if err := os.Rename(src, dst); err != nil {
		if hadOld {
			os.Rename(old, dst)
		}
		return err
	}
	if hadOld {
		os.RemoveAll(old)
	}
	return nil
}

// noModError explains a file without a mod.json.
func noModError(dir, name string) error {
	var dll, wbox bool
	filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			switch strings.ToLower(filepath.Ext(p)) {
			case ".dll":
				dll = true
			case ".wbox":
				wbox = true
			}
		}
		return nil
	})
	switch {
	case wbox:
		return fmt.Errorf("%s is a world save, not a mod", name)
	case dll:
		return fmt.Errorf("%s has no mod.json: it's for another loader (BepInEx or NCMS). Follow the instructions on its page", name)
	}
	return fmt.Errorf("%s has no mod.json, so NeoModLoader can't load it", name)
}

// ScanOptions tells the scanner where the game's own libraries are, so a
// mod's copy of one is recognised.
func (m *Manager) ScanOptions() scan.Options {
	gd, err := m.GameDir()
	if err != nil {
		return scan.Options{}
	}
	return scan.Options{GameManaged: game.ManagedDir(gd)}
}

// LoaderStatus reports the NeoModLoader install.
func (m *Manager) LoaderStatus() (nml.LoaderStatus, error) {
	gd, err := m.GameDir()
	if err != nil {
		return nml.LoaderStatus{}, err
	}
	return nml.Status(game.NativeModsDir(gd)), nil
}

// InstallLoader installs or updates NeoModLoader.
func (m *Manager) InstallLoader() (string, error) {
	if gameRunning() {
		return "", errors.New("close WorldBox first")
	}
	gd, err := m.GameDir()
	if err != nil {
		return "", err
	}
	return nml.Install(game.NativeModsDir(gd))
}

func readDirs(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasSuffix(e.Name(), ".wbmm-old") {
			out = append(out, e.Name())
		}
	}
	return out
}

// findIcon resolves mod.json's iconPath (case-insensitively: mods made on
// Windows often differ in case), falling back to icon.png/icon.jpg.
func findIcon(dir, iconPath string) string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	want := []string{}
	if iconPath != "" && !strings.ContainsAny(iconPath, `/\`) {
		want = append(want, iconPath)
	}
	want = append(want, "icon.png", "icon.jpg", "icon.jpeg")
	for _, w := range want {
		for _, e := range es {
			if !e.IsDir() && strings.EqualFold(e.Name(), w) {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return ""
}
