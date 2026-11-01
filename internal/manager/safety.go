package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Trlydev/WBMM/internal/archive"
	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/scan"
)

// Every mod is checked by the malware scanner (internal/scan) before it
// can run: GameBanana mods before they reach the Mods folder, Workshop
// mods after Steam downloads them and before the game starts, and every
// installed mod again whenever its files change (Steam updates Workshop
// mods by itself, which is how the 2026 People Playground worm spread).
// A CRITICAL finding holds the mod (switched off in NML's list) until the
// person accepts it; the acceptance is for that version of the files only.

// Verdict is what the scanner said about one version of a mod.
type Verdict struct {
	Fingerprint string         `json:"fingerprint"`
	Scanned     time.Time      `json:"scanned"`
	Max         scan.Severity  `json:"max"` // -1: nothing found
	Findings    []scan.Finding `json:"findings,omitempty"`
	// Accepted: the person allowed this version despite CRITICAL findings.
	Accepted bool `json:"accepted,omitempty"`
	// Held: wbmm switched the mod off because of this verdict.
	Held bool `json:"held,omitempty"`
}

// Blocked reports whether the verdict keeps the mod from running.
func (v *Verdict) Blocked() bool { return v != nil && v.Max >= scan.Critical && !v.Accepted }

// RiskError is returned by installs the scanner stopped.
type RiskError struct {
	Name     string
	Findings []scan.Finding
}

func (e *RiskError) Error() string {
	var rules []string
	seen := map[string]bool{}
	for _, f := range e.Findings {
		if f.Severity >= scan.Critical && !seen[f.Rule] {
			seen[f.Rule] = true
			rules = append(rules, f.Rule)
		}
	}
	return fmt.Sprintf("the safety check stopped %s: %s", e.Name, strings.Join(rules, ", "))
}

// fingerprint identifies a version of a mod folder: the contents of its
// code files (.cs, .dll, mod.json) and the names and sizes of the rest. A
// mod writing its own config files doesn't change it; code rewritten in
// place, even with its size and time kept, does.
func fingerprint(dir string) string {
	h := sha256.New()
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		switch strings.ToLower(filepath.Ext(p)) {
		case ".cs", ".dll", ".json":
			if strings.EqualFold(filepath.Ext(p), ".json") && !strings.EqualFold(d.Name(), "mod.json") {
				break
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
			io.Copy(h, io.LimitReader(f, 256<<20))
			f.Close()
			return nil
		}
		if fi, err := d.Info(); err == nil {
			fmt.Fprintf(h, "%s\x00%d\n", filepath.ToSlash(rel), fi.Size())
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// keep trims a report to what's worth storing and showing.
func keep(r *scan.Report) Verdict {
	v := Verdict{Scanned: time.Now(), Max: r.Max()}
	for _, f := range r.Findings {
		if f.Severity >= scan.Medium {
			v.Findings = append(v.Findings, f)
		}
	}
	sort.SliceStable(v.Findings, func(i, j int) bool { return v.Findings[i].Severity > v.Findings[j].Severity })
	if len(v.Findings) > 60 {
		v.Findings = v.Findings[:60]
	}
	return v
}

// Verdict returns the stored verdict of a mod folder, if it is for the
// folder's current files.
func (m *Manager) Verdict(folder string) (*Verdict, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.st.Verdicts[folder]
	if !ok {
		return nil, false
	}
	return &v, v.Fingerprint == fingerprint(folder)
}

// Check scans a mod folder now (if its files changed since the last scan)
// and stores the verdict. A newly blocked mod is switched off in NML's list.
func (m *Manager) Check(md Mod) (Verdict, error) {
	v, _, err := m.check(md)
	return v, err
}

// check is Check that also reports whether it just switched the mod off.
func (m *Manager) check(md Mod) (Verdict, bool, error) {
	fp := fingerprint(md.Folder)
	m.mu.Lock()
	v, had := m.st.Verdicts[md.Folder]
	m.mu.Unlock()
	if had && v.Fingerprint == fp && v.Held && v.Blocked() && md.Enabled && md.UID != "" && !gameRunning() {
		// Held, but on again: NML switches on what an enabled mod needs,
		// and the in-game Mods window can too. Off again.
		if gd, err := m.GameDir(); err == nil {
			nml.SetDisabled(game.NMLDir(gd), md.UID, true)
		}
		return v, true, nil
	}
	if had && v.Fingerprint == fp && (v.Held || !v.Blocked()) {
		return v, false, nil
	}
	if !had || v.Fingerprint != fp {
		rep, err := scan.Mod(md.Folder, m.ScanOptions())
		if err != nil {
			return Verdict{}, false, err
		}
		v = keep(rep)
		v.Fingerprint = fp
	}
	// Switch it off now, or (the game rewrites NML's list while it runs)
	// at the next check after the game closes.
	newly := false
	if v.Blocked() && !v.Held && md.UID != "" && !gameRunning() {
		if gd, err := m.GameDir(); err == nil {
			if nml.SetDisabled(game.NMLDir(gd), md.UID, true) == nil {
				v.Held, newly = true, true
			}
		}
	}
	err := m.update(func(st *state) { st.Verdicts[md.Folder] = v })
	return v, newly, err
}

// CheckAll checks every installed mod whose files changed and returns the
// ones it newly holds.
func (m *Manager) CheckAll() (held []Mod, err error) {
	mods, err := m.List()
	if err != nil {
		return nil, err
	}
	blocked := map[string]bool{}
	for _, md := range mods {
		v, newly, cerr := m.check(md)
		if cerr != nil {
			err = cerr
			continue
		}
		if newly {
			held = append(held, md)
		}
		if v.Blocked() && md.UID != "" {
			blocked[md.UID] = true
		}
	}
	if h, aerr := m.checkArchives(); aerr != nil {
		err = aerr
	} else {
		held = append(held, h...)
	}
	// NML switches on a held mod if an enabled mod needs it: switch off
	// what needs a held mod (it couldn't load without it anyway).
	if len(blocked) > 0 && !gameRunning() {
		if gd, gerr := m.GameDir(); gerr == nil {
			for _, md := range mods {
				if !md.Enabled || blocked[md.UID] {
					continue
				}
				for _, d := range md.deps {
					if blocked[nml.Symbol(d)] {
						nml.SetDisabled(game.NMLDir(gd), md.UID, true)
						break
					}
				}
			}
		}
	}
	return held, err
}

// Accept allows the current version of a held mod and switches it back on.
func (m *Manager) Accept(md Mod) error {
	fp := fingerprint(md.Folder)
	m.mu.Lock()
	v, ok := m.st.Verdicts[md.Folder]
	if !ok || v.Fingerprint != fp {
		m.mu.Unlock()
		return fmt.Errorf("%s changed since it was checked; check it again first", md.Name)
	}
	m.mu.Unlock()
	v.Accepted, v.Held = true, false
	if err := m.update(func(st *state) { st.Verdicts[md.Folder] = v }); err != nil {
		return err
	}
	return m.SetEnabled(md.UID, true)
}

// scanStaged checks unpacked mod folders before they are installed.
func (m *Manager) scanStaged(dir string) Verdict {
	rep, err := scan.Mod(dir, m.ScanOptions())
	if err != nil {
		return Verdict{Max: scan.Critical, Findings: []scan.Finding{{Severity: scan.Critical, Rule: "scan-error", Detail: err.Error()}}}
	}
	return keep(rep)
}

// Archives NML unpacks and installs at start: zip files (any of these
// names) dropped straight into the Mods folder.
var nmlArchiveExt = []string{".zip", ".7z", ".rar", ".tar", ".tar.gz", ".mod"}

// checkArchives scans archives waiting in the Mods folder. A risky one is
// renamed to <name>.wbmm-held, which NML ignores.
func (m *Manager) checkArchives() (held []Mod, err error) {
	gd, err := m.GameDir()
	if err != nil {
		return nil, nil
	}
	dir := game.ModsDir(gd)
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		name := strings.ToLower(e.Name())
		isArchive := false
		for _, ext := range nmlArchiveExt {
			if strings.HasSuffix(name, ext) {
				isArchive = true
			}
		}
		if e.IsDir() || !isArchive {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if head, _ := os.ReadFile(p); len(head) < 4 || string(head[:2]) != "PK" {
			continue // NML only unpacks zip files
		}
		tmp, terr := os.MkdirTemp("", "wbmm-archive-")
		if terr != nil {
			return held, terr
		}
		var v Verdict
		if xerr := archive.Extract(p, tmp); xerr != nil {
			v = Verdict{Max: scan.Critical, Findings: []scan.Finding{{Severity: scan.Critical, Rule: "scan-error",
				File: e.Name(), Detail: "couldn't be unpacked to check: " + xerr.Error()}}}
		} else {
			v = m.scanStaged(tmp)
		}
		os.RemoveAll(tmp)
		if !v.Blocked() {
			continue
		}
		if rerr := os.Rename(p, p+".wbmm-held"); rerr != nil {
			err = rerr
			continue
		}
		held = append(held, Mod{Name: e.Name(), Folder: p + ".wbmm-held", Problem: "archive held by the safety check"})
	}
	return held, err
}
