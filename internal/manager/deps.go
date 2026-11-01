package manager

import (
	"strconv"
	"strings"

	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/popularity"
)

// Dependency is a mod something needs, and the GameBanana item that
// installs it if wbmm knows one (GBID 0: it doesn't).
type Dependency struct {
	ID   string // as the mod.json names it
	GBID int
	Name string // the GameBanana item's name, else ID
}

// FindDependencies looks up where to get mods by their NeoModLoader IDs:
// the popularity index lists the IDs in every GameBanana mod's files.
func FindDependencies(ids []string) []Dependency {
	ix, _ := popularity.Fetch()
	names := map[string]string{}
	if ix != nil {
		for _, it := range ix.Items {
			if it.Src == "gb" {
				names[it.Ref] = it.Name
			}
		}
	}
	var out []Dependency
	for _, id := range ids {
		d := Dependency{ID: id, Name: id}
		if ix != nil {
			if ref := ix.Provides[nml.Symbol(id)]; strings.HasPrefix(ref, "gb:") {
				d.GBID, _ = strconv.Atoi(strings.TrimPrefix(ref, "gb:"))
				if n := names[ref]; n != "" {
					d.Name = n
				}
			}
		}
		out = append(out, d)
	}
	return out
}
