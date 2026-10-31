package manager

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Trlydev/WBMM/internal/sources"
)

// Update is a newer GameBanana file for an installed mod.
type Update struct {
	UID  string         `json:"uid"`
	Name string         `json:"name"`
	GBID int            `json:"gbId"`
	File sources.GBFile `json:"file"`
}

var (
	// GameBanana appends _<5 hex> to a file name it already has.
	reGBSuffix = regexp.MustCompile(`_[0-9a-f]{5}$`)
	// A version after a separator at the end of a name: "_v1.2", "-2.0.1".
	reSepVersion = regexp.MustCompile(`[-_. ]+v?\d+(?:[._]\d+)*$`)
	// Digits stuck to the last word: "warbox010", "m5".
	reGlued = regexp.MustCompile(`([a-z]+)\d+$`)
)

// fileLine is the name a mod's files share across versions, so an update
// stays on the line the person picked: "warbox010.zip" and "warbox050.zip"
// are one line, "modernbox-m1.zip" and "modernbox-m5.zip" are two.
func fileLine(name string) string {
	s := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	s = reGBSuffix.ReplaceAllString(s, "")
	s = reSepVersion.ReplaceAllString(s, "")
	// A number on a word is its version ("warbox010"), but on a one- or
	// two-letter label it names a variant ("m5", "mk2"... "v2" excepted).
	if m := reGlued.FindStringSubmatchIndex(s); m != nil {
		word := s[m[2]:m[3]]
		if len(word) > 2 || word == "v" {
			s = s[:m[3]]
			if word == "v" {
				s = strings.TrimRight(s[:m[2]], "-_. ")
			}
		}
	}
	return s
}

// newerOnLine returns the newest file on the same line as the installed
// one, if it is newer.
func newerOnLine(files []sources.GBFile, r Record) (sources.GBFile, bool) {
	line := fileLine(r.FileName)
	for _, f := range files { // newest first
		if f.ID == r.FileID {
			return sources.GBFile{}, false
		}
		if f.Added > r.FileAdded && fileLine(f.Name) == line {
			return f, true
		}
	}
	return sources.GBFile{}, false
}

// Updates checks GameBanana for newer files of installed mods, on the
// same file line as the one installed.
func (m *Manager) Updates() ([]Update, error) {
	m.mu.Lock()
	recs := make([]Record, 0, len(m.st.Records))
	for _, r := range m.st.Records {
		recs = append(recs, r)
	}
	m.mu.Unlock()
	var out []Update
	var errs []string
	checked := map[int][]sources.GBFile{}
	for _, r := range recs {
		files, ok := checked[r.GBID]
		if !ok {
			var err error
			files, err = sources.GBFiles(r.GBID)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", r.Name, err))
				continue
			}
			checked[r.GBID] = files
		}
		if f, ok := newerOnLine(files, r); ok {
			out = append(out, Update{UID: r.UID, Name: r.Name, GBID: r.GBID, File: f})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(errs) > 0 {
		return out, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}

// AutoUpdate installs every update that passed GameBanana's virus scan.
// It does nothing while the game runs. It returns what it updated and what
// it held back.
func (m *Manager) AutoUpdate() (updated, held []string, err error) {
	if gameRunning() {
		return nil, nil, ErrGameRunning
	}
	ups, err := m.Updates()
	for _, u := range ups {
		if !u.File.Clean() {
			held = append(held, u.Name+" (virus scan not passed)")
			continue
		}
		if _, ierr := m.InstallGB(u.GBID, u.File.ID, false); ierr != nil {
			held = append(held, fmt.Sprintf("%s (%v)", u.Name, ierr))
			continue
		}
		updated = append(updated, u.Name)
	}
	return updated, held, err
}
