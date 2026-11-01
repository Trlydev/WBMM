package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Trlydev/WBMM/internal/modpack"
	"github.com/Trlydev/WBMM/internal/steam"
)

// ExportPack makes a modpack of the installed mods (and whether each is on).
func (m *Manager) ExportPack(name string) (modpack.Pack, error) {
	mods, err := m.List()
	if err != nil {
		return modpack.Pack{}, err
	}
	m.mu.Lock()
	recs := map[string]Record{}
	for _, r := range m.st.Records {
		recs[r.UID] = r
	}
	m.mu.Unlock()
	p := modpack.Pack{Name: name}
	seen := map[string]bool{}
	for _, md := range mods {
		if md.Problem != "" || seen[md.UID] {
			continue
		}
		seen[md.UID] = true
		e := modpack.Entry{Enabled: md.Enabled, Name: md.Name, Source: modpack.Local}
		switch md.Source {
		case "gamebanana":
			if r, ok := recs[md.UID]; ok {
				e.Source, e.ID, e.FileID = modpack.GameBanana, uint64(r.GBID), uint64(r.FileID)
			}
		case "workshop":
			if id, err := strconv.ParseUint(md.WorkshopID, 10, 64); err == nil {
				e.Source, e.ID = modpack.Workshop, id
			}
		}
		p.Entries = append(p.Entries, e)
	}
	return p, nil
}

// Step is one thing applying a pack does.
type Step struct {
	Entry modpack.Entry
	// What: "install" (GameBanana), "subscribe" (Workshop), "switch-on",
	// "switch-off", "ok" (already as the pack wants), "manual" (installed
	// by hand: can't be fetched), "update" (installed, other file).
	What string
	UID  string // the installed mod, if any
}

// PlanPack says what applying p would do. With exclusive, mods the pack
// doesn't list are switched off (the pack becomes a profile).
func (m *Manager) PlanPack(p modpack.Pack, exclusive bool) ([]Step, error) {
	mods, err := m.List()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	byGB := map[uint64][]Record{}
	for _, r := range m.st.Records {
		byGB[uint64(r.GBID)] = append(byGB[uint64(r.GBID)], r)
	}
	m.mu.Unlock()
	byWS, byName, byUID := map[string]Mod{}, map[string]Mod{}, map[string]Mod{}
	for _, md := range mods {
		if md.Problem != "" {
			continue
		}
		if md.WorkshopID != "" {
			byWS[md.WorkshopID] = md
		}
		byName[strings.ToLower(md.Name)] = md
		byUID[md.UID] = md
	}
	covered := map[string]bool{}
	var steps []Step
	toggle := func(e modpack.Entry, md Mod) Step {
		covered[md.UID] = true
		switch {
		case md.Enabled == e.Enabled:
			return Step{Entry: e, What: "ok", UID: md.UID}
		case e.Enabled:
			return Step{Entry: e, What: "switch-on", UID: md.UID}
		}
		return Step{Entry: e, What: "switch-off", UID: md.UID}
	}
	for _, e := range p.Entries {
		switch e.Source {
		case modpack.GameBanana:
			recs := byGB[e.ID]
			if len(recs) == 0 {
				steps = append(steps, Step{Entry: e, What: "install"})
				continue
			}
			for _, r := range recs {
				md, ok := byUID[r.UID]
				if !ok {
					continue
				}
				s := toggle(e, md)
				if e.FileID != 0 && uint64(r.FileID) != e.FileID {
					s.What = "update"
				}
				steps = append(steps, s)
			}
		case modpack.Workshop:
			md, ok := byWS[strconv.FormatUint(e.ID, 10)]
			if !ok {
				steps = append(steps, Step{Entry: e, What: "subscribe"})
				continue
			}
			steps = append(steps, toggle(e, md))
		default:
			if md, ok := byName[strings.ToLower(e.Name)]; ok {
				steps = append(steps, toggle(e, md))
			} else {
				steps = append(steps, Step{Entry: e, What: "manual"})
			}
		}
	}
	if exclusive {
		for _, md := range mods {
			if md.Problem == "" && md.Enabled && !covered[md.UID] {
				steps = append(steps, Step{Entry: modpack.Entry{Name: md.Name}, What: "switch-off", UID: md.UID})
				covered[md.UID] = true
			}
		}
	}
	return steps, nil
}

// ApplyPack carries out a plan: installs and subscribes first (each mod
// through the safety check, as always), then the switches. progress gets
// a line per step. It returns what failed, step by step.
func (m *Manager) ApplyPack(steps []Step, progress func(string)) []error {
	var errs []error
	fail := func(s Step, err error) { errs = append(errs, fmt.Errorf("%s: %w", s.Entry.Name, err)) }
	for _, s := range steps {
		switch s.What {
		case "install", "update":
			progress("Installing " + s.Entry.Name + "…")
			if _, err := m.InstallGB(int(s.Entry.ID), int(s.Entry.FileID), false, false); err != nil {
				fail(s, err)
			}
		case "subscribe":
			progress("Subscribing to " + s.Entry.Name + " through Steam…")
			id := strconv.FormatUint(s.Entry.ID, 10)
			v, err := m.Subscribe(id, func(steam.Progress) {})
			if err != nil {
				fail(s, err)
			} else if v != nil && v.Blocked() {
				fail(s, errors.New("the safety check is holding it: review it in My mods"))
			}
		}
	}
	// Switches last: the new mods are listed now, and NML's list is
	// written once things are in place.
	if gameRunning() {
		return append(errs, ErrGameRunning)
	}
	mods, _ := m.List()
	for _, s := range steps {
		want, set := true, false
		switch s.What {
		case "switch-on":
			set = true
		case "switch-off":
			want, set = false, true
		case "install", "subscribe", "update":
			want, set = s.Entry.Enabled, !s.Entry.Enabled
		}
		if !set {
			continue
		}
		uid := s.UID
		if uid == "" {
			uid = findUID(mods, s.Entry)
		}
		if uid == "" {
			continue
		}
		progress(fmt.Sprintf("Switching %s %s…", s.Entry.Name, map[bool]string{true: "on", false: "off"}[want]))
		if err := m.SetEnabled(uid, want); err != nil {
			fail(s, err)
		}
	}
	return errs
}

// findUID finds the installed mod a pack entry became.
func findUID(mods []Mod, e modpack.Entry) string {
	for _, md := range mods {
		switch {
		case e.Source == modpack.Workshop && md.WorkshopID == strconv.FormatUint(e.ID, 10):
			return md.UID
		case e.Source == modpack.GameBanana && uint64(md.GBID) == e.ID && md.GBID != 0:
			return md.UID
		}
	}
	return ""
}

// ---------- saved packs ----------

// PacksDir holds the modpacks saved in wbmm (profiles).
func PacksDir() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	d = filepath.Join(d, "modpacks")
	return d, os.MkdirAll(d, 0o755)
}

// SavedPack is a modpack file in PacksDir.
type SavedPack struct {
	Path string
	Pack modpack.Pack
}

// SavedPacks lists the saved modpacks, by name.
func SavedPacks() ([]SavedPack, error) {
	d, err := PacksDir()
	if err != nil {
		return nil, err
	}
	es, _ := os.ReadDir(d)
	var out []SavedPack
	for _, e := range es {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), modpack.Ext) {
			continue
		}
		p := filepath.Join(d, e.Name())
		if p2, err := ReadPackFile(p); err == nil {
			out = append(out, SavedPack{Path: p, Pack: p2})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Pack.Name) < strings.ToLower(out[j].Pack.Name) })
	return out, nil
}

// ReadPackFile reads a .wbm file.
func ReadPackFile(path string) (modpack.Pack, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return modpack.Pack{}, err
	}
	if fi.Size() > 1<<20 {
		return modpack.Pack{}, errors.New("that file is too big to be a modpack")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return modpack.Pack{}, err
	}
	return modpack.Decode(string(b))
}

// WritePackFile saves a pack as a .wbm file: the code, then the list in
// words for anyone reading the file.
func WritePackFile(path string, p modpack.Pack) error {
	var b strings.Builder
	b.WriteString(p.Encode() + "\n\n")
	fmt.Fprintf(&b, "%s: a WorldBox Mod Manager modpack (open it with wbmm: https://github.com/Trlydev/WBMM)\n", p.Name)
	for _, e := range p.Entries {
		src := map[int]string{modpack.GameBanana: "GameBanana", modpack.Workshop: "Workshop", modpack.Local: "installed by hand"}[e.Source]
		state := ""
		if !e.Enabled {
			state = " (off)"
		}
		fmt.Fprintf(&b, "- %s, %s%s\n", e.Name, src, state)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SavePack saves p in PacksDir under its name, returning the file.
func SavePack(p modpack.Pack) (string, error) {
	d, err := PacksDir()
	if err != nil {
		return "", err
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(p.Name))
	if name == "" {
		name = "modpack"
	}
	path := filepath.Join(d, name+modpack.Ext)
	return path, WritePackFile(path, p)
}
