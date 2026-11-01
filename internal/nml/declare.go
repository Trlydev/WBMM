// Package nml reads and writes what NeoModLoader (NML), WorldBox's mod
// loader, reads: mod.json declarations, its mod on/off records and its own
// DLL. https://github.com/WorldBoxOpenMods/ModLoader
package nml

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Trlydev/WBMM/internal/scan"
)

// DeclFile is the file that makes a folder an NML mod.
const DeclFile = "mod.json"

// Declare is a mod.json. NML reads it with Newtonsoft.Json, which matches
// names case-insensitively, as encoding/json does.
type Declare struct {
	Name         string   `json:"name"`
	GUID         string   `json:"GUID"`
	Author       string   `json:"author"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	IconPath     string   `json:"iconPath"`
	RepoURL      string   `json:"RepoUrl"`
	Dependencies []string `json:"Dependencies"`
	Optional     []string `json:"OptionalDependencies"`
	Incompatible []string `json:"IncompatibleWith"`
	ModType      string   `json:"ModType"`
}

// UID is the ID NML keys the mod by: GUID, else "author.name", with every
// ASCII/Latin-1 character that isn't a letter or digit replaced by '_' and
// letters upper-cased (ModDependencyUtils.ParseDepenNameToPreprocessSymbol).
func (d Declare) UID() string {
	id := d.GUID
	if id == "" {
		id = d.Author + "." + d.Name
	}
	return Symbol(id)
}

// Symbol turns a mod ID as written in mod.json (a GUID, "author.name", or a
// dependency entry) into the UID NML compares. It follows NML's C# rule
// char by char, and C# chars are UTF-16 units: a unit at or below U+0100
// that isn't a letter or digit becomes '_', anything else is upper-cased,
// and the halves of a character outside the basic plane are kept as they
// are (char.ToUpper leaves surrogates alone).
func Symbol(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r > 0xffff {
			b.WriteRune(r)
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r <= 256 {
			b.WriteByte('_')
		} else {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// ReadDeclare parses a mod.json.
func ReadDeclare(path string) (Declare, error) {
	var d Declare
	b, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	// NML reads it with Newtonsoft.Json, which accepts comments, trailing
	// commas and UTF-16; so does this.
	err = json.Unmarshal(scan.LooseJSON(b), &d)
	// A field of an unexpected type (targetGameBuild as a string, say)
	// doesn't stop NML; the rest of the file still counts.
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		err = nil
	}
	if err == nil && d.Name == "" && d.GUID == "" {
		err = errors.New("mod.json has no name")
	}
	if d.Name == "" {
		d.Name = d.GUID
	}
	return d, err
}

// FindDeclare returns the mod.json NML would use for a mod folder: the
// shallowest one, searching level by level as NML does.
func FindDeclare(dir string) (string, bool) {
	level := []string{dir}
	for depth := 0; len(level) > 0 && depth < 32; depth++ {
		var next []string
		for _, d := range level {
			es, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, e := range es {
				if !e.IsDir() && e.Name() == DeclFile {
					return filepath.Join(d, e.Name()), true
				}
			}
			for _, e := range es {
				if e.IsDir() {
					next = append(next, filepath.Join(d, e.Name()))
				}
			}
		}
		level = next
	}
	return "", false
}

// ModRoots returns the folders inside an unpacked archive that each hold one
// mod: every folder with a mod.json that isn't inside another such folder.
func ModRoots(dir string) []string {
	var all []string
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && e.Name() == DeclFile {
			all = append(all, filepath.Dir(p))
		}
		return nil
	})
	// Sorted, a folder comes before everything inside it.
	sort.Strings(all)
	var roots []string
	for _, p := range all {
		nested := false
		for _, r := range roots {
			if strings.HasPrefix(p, r+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			roots = append(roots, p)
		}
	}
	return roots
}
