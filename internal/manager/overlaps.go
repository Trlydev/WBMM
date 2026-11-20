package manager

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Trlydev/WBMM/internal/nml"
)

// ModOverlap is another switched-on mod that replaces or can skip the same
// game code as a mod.
type ModOverlap struct {
	Other   string   // its name
	UID     string   // its UID
	Targets []string // the game methods, Type.method
}

type patchEntry struct {
	stamp   string
	patches []nml.Patch
}

var (
	patchMu    sync.Mutex
	patchCache = map[string]patchEntry{} // by mod folder
)

// patchesOf reads a mod folder's Harmony patches, again only when its C#
// files changed (names, sizes and times).
func patchesOf(folder string) []nml.Patch {
	var b strings.Builder
	filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".cs") {
			if fi, err := d.Info(); err == nil {
				fmt.Fprintf(&b, "%s %d %d\n", p, fi.Size(), fi.ModTime().UnixNano())
			}
		}
		return nil
	})
	stamp := b.String()
	patchMu.Lock()
	e, ok := patchCache[folder]
	patchMu.Unlock()
	if ok && e.stamp == stamp {
		return e.patches
	}
	ps := nml.Patches(folder)
	patchMu.Lock()
	patchCache[folder] = patchEntry{stamp, ps}
	patchMu.Unlock()
	return ps
}

// Overlaps returns, for each switched-on mod (by UID), the other
// switched-on mods that replace or can skip the same game code: Harmony
// prefixes that can stop the original, and transpilers. Only patches the
// source declares with attributes are seen.
func Overlaps(mods []Mod) map[string][]ModOverlap {
	all := map[string][]nml.Patch{}
	names := map[string]string{}
	for _, md := range mods {
		if md.Enabled && md.Problem == "" && md.UID != "" {
			all[md.UID] = patchesOf(md.Folder)
			names[md.UID] = md.Name
		}
	}
	pairs := map[[2]string][]string{}
	for _, o := range nml.Overlaps(all) {
		for _, a := range o.Mods {
			for _, b := range o.Mods {
				if a != b {
					pairs[[2]string{a, b}] = append(pairs[[2]string{a, b}], o.Target)
				}
			}
		}
	}
	out := map[string][]ModOverlap{}
	for k, targets := range pairs {
		out[k[0]] = append(out[k[0]], ModOverlap{Other: names[k[1]], UID: k[1], Targets: targets})
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].Other < list[j].Other })
	}
	return out
}
