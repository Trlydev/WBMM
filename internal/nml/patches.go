package nml

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Most WorldBox mods change the game with Harmony patches declared by
// attributes:
//
//	[HarmonyPatch(typeof(Actor), "getHit")]
//	class GetHitPatch { static bool Prefix(...) { ... } }
//
// A prefix returning bool can stop the game's code (and other mods'
// patches) from running; a transpiler rewrites it. Two mods doing either to
// the same method is where mods break each other. Patches made in code at
// run time (harmony.Patch(...)) can't be read without running the mod.

// Patch is one Harmony patch a mod's source declares.
type Patch struct {
	Target string // Type.method
	Kind   string // prefix, postfix, transpiler, finalizer
	Skips  bool   // a prefix returning bool: it can skip the original
	File   string
}

// Intrusive reports whether the patch replaces or can skip what it patches.
func (p Patch) Intrusive() bool { return p.Skips || p.Kind == "transpiler" }

var (
	reAttrOrDecl = regexp.MustCompile(`\[\s*((?:HarmonyLib\.)?Harmony[^\[\]]*)\]` +
		`|\bclass\s+\w+` +
		`|\b(bool|void|IEnumerable\s*<\s*CodeInstruction\s*>|Exception)\s+(\w+)\s*\(`)
	// One attribute inside a bracket: [HarmonyPatch(typeof(X), "m"), HarmonyPrefix].
	reAttr   = regexp.MustCompile(`(Harmony(?:Patch|Prefix|Postfix|Transpiler|Finalizer))\b\s*(?:\(((?:[^()]|\([^()]*\))*)\))?`)
	reTypeof = regexp.MustCompile(`typeof\(\s*([\w.]+)\s*\)`)
	reMethod = regexp.MustCompile(`"(\w+)"|nameof\(\s*([\w.]+)\s*\)`)
)

// Patches reads the Harmony patches declared in a mod folder's C# files.
func Patches(dir string) []Patch {
	var out []Patch
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".cs") {
			return nil
		}
		if fi, err := d.Info(); err != nil || fi.Size() > 4<<20 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, parsePatches(string(b), filepath.ToSlash(rel))...)
		return nil
	})
	return out
}

// skipDirs are the folders NML doesn't compile.
var skipDirs = map[string]bool{"bin": true, "obj": true, "Properties": true, "packages": true,
	"GameResources": true, "GameResourcesReplace": true, "AssetBundles": true}

func parsePatches(src, file string) []Patch {
	src = stripComments(src)
	var out []Patch
	var classType, classMethod string // from the attribute on the class
	var pendType, pendMethod, pendKind string
	pendPatch := false
	reset := func() { pendType, pendMethod, pendKind, pendPatch = "", "", "", false }
	for _, m := range reAttrOrDecl.FindAllStringSubmatch(src, -1) {
		switch {
		case m[1] != "":
			for _, a := range reAttr.FindAllStringSubmatch(m[1], -1) {
				switch a[1] {
				case "HarmonyPatch":
					pendPatch = true
					if t := reTypeof.FindStringSubmatch(a[2]); t != nil {
						pendType = last(t[1])
					}
					args := reTypeof.ReplaceAllString(a[2], "")
					if mm := reMethod.FindStringSubmatch(args); mm != nil {
						pendMethod = mm[1] + last(mm[2])
					}
				default:
					pendKind = strings.ToLower(strings.TrimPrefix(a[1], "Harmony"))
				}
			}
		case strings.HasPrefix(m[0], "class"):
			if pendPatch {
				classType, classMethod = pendType, pendMethod
			} else {
				classType, classMethod = "", ""
			}
			reset()
		default:
			ret, name := m[2], m[3]
			kind := pendKind
			if kind == "" {
				switch strings.ToLower(name) {
				case "prefix", "postfix", "transpiler", "finalizer":
					kind = strings.ToLower(name)
				}
			}
			typ, meth := pendType, pendMethod
			if typ == "" {
				typ = classType
			}
			if meth == "" {
				meth = classMethod
			}
			if kind != "" && typ != "" && meth != "" && (pendPatch || classType != "") {
				out = append(out, Patch{Target: typ + "." + meth, Kind: kind,
					Skips: kind == "prefix" && ret == "bool", File: file})
			}
			reset()
		}
	}
	return out
}

func last(dotted string) string {
	if i := strings.LastIndex(dotted, "."); i >= 0 {
		return dotted[i+1:]
	}
	return dotted
}

var reComment = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)

func stripComments(s string) string { return reComment.ReplaceAllString(s, "") }

// Overlap is a game method two mods both replace or can skip.
type Overlap struct {
	Target string
	Mods   []string // their UIDs, sorted
}

// Overlaps finds the methods more than one mod patches intrusively.
// patches is by mod UID.
func Overlaps(patches map[string][]Patch) []Overlap {
	by := map[string]map[string]bool{}
	for uid, ps := range patches {
		for _, p := range ps {
			if !p.Intrusive() {
				continue
			}
			if by[p.Target] == nil {
				by[p.Target] = map[string]bool{}
			}
			by[p.Target][uid] = true
		}
	}
	var out []Overlap
	for t, mods := range by {
		if len(mods) < 2 {
			continue
		}
		o := Overlap{Target: t}
		for uid := range mods {
			o.Mods = append(o.Mods, uid)
		}
		sort.Strings(o.Mods)
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}
