package nml

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Trlydev/WBMM/internal/game"
)

// What NeoModLoader wrote about each mod the last time the game started,
// read from Unity's Player.log. NML prefixes its lines with "[NML]: ".

// LogProblem is something NML reported about one mod.
type LogProblem struct {
	Mod     string // UID, or the mod's name for some messages
	Kind    string // compile, dependency, incompatible, outdated, disabled, no-entry
	Summary string // what it means, in plain words
	Details []string
}

// GameLog is what one game start said about mods.
type GameLog struct {
	Path     string
	Time     time.Time
	NML      bool            // NeoModLoader ran
	Loaded   map[string]bool // mod names that finished loading
	Problems []LogProblem
}

var (
	reLoaded      = regexp.MustCompile(`^\[NML\]: \[(.+)\]: Loaded\s*$`)
	reCompile     = regexp.MustCompile(`^\[NML\]: Failed to compile mod (\S+?):?\s*$`)
	reSkipped     = regexp.MustCompile(`^\[NML\]: Compile skipped for mod (\S+) because required dependencies failed to compile`)
	reMissing     = regexp.MustCompile(`Mod (\S+) has missing dependencies:`)
	reIncompat    = regexp.MustCompile(`Mod (\S+) (?:is incompatible|has incompatible)`)
	reOutdated    = regexp.MustCompile(`^\[NML\]: Compiled mod (\S+) out of date`)
	reDisabled    = regexp.MustCompile(`^\[NML\]: (.+) has been disabled due to an error`)
	reNoEntry     = regexp.MustCompile(`No valid mod component found in assembly .* for mod (\S+)`)
	reCSError     = regexp.MustCompile(`error (CS\d{4}):`)
	reNMLStarted  = regexp.MustCompile(`^\[NML\]: NeoModLoader Version`)
	reIndentEntry = regexp.MustCompile(`^\s{2,}(\S.*)$`)
)

// PlayerLogs lists where Unity keeps WorldBox's log on this PC; gameDir
// finds the Proton prefix of a Windows build run on Linux.
func PlayerLogs(gameDir string) []string {
	if runtime.GOOS == "darwin" { // macOS keeps logs apart from data
		home, _ := os.UserHomeDir()
		return []string{filepath.Join(home, "Library", "Logs", "mkarpenko", "WorldBox", "Player.log")}
	}
	var out []string
	for _, d := range game.UserDataDirs(gameDir) {
		out = append(out, filepath.Join(d, "Player.log"))
	}
	return out
}

// ReadLastRun reads the newest of the game's logs.
func ReadLastRun(gameDir string) (*GameLog, error) {
	var best string
	var bestTime time.Time
	for _, p := range PlayerLogs(gameDir) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(bestTime) {
			best, bestTime = p, fi.ModTime()
		}
	}
	if best == "" {
		return nil, os.ErrNotExist
	}
	f, err := os.Open(best)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gl := ParseLog(bufio.NewScanner(f))
	gl.Path, gl.Time = best, bestTime
	return gl, nil
}

// ParseLog reads a Player.log.
func ParseLog(sc *bufio.Scanner) *GameLog {
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	gl := &GameLog{Loaded: map[string]bool{}}
	var open *LogProblem // a problem whose detail lines follow
	add := func(p LogProblem) *LogProblem {
		gl.Problems = append(gl.Problems, p)
		return &gl.Problems[len(gl.Problems)-1]
	}
	for sc.Scan() {
		ln := strings.TrimRight(sc.Text(), "\r")
		if open != nil {
			// Detail lines: compiler errors, or indented dependency names.
			switch {
			case open.Kind == "compile" && strings.Contains(ln, ": error CS"):
				if len(open.Details) < 8 {
					open.Details = append(open.Details, strings.TrimSpace(ln))
				}
				if m := reCSError.FindStringSubmatch(ln); m != nil && open.Summary == compileSummary("") {
					open.Summary = compileSummary(m[1])
				}
				continue
			case (open.Kind == "dependency" || open.Kind == "incompatible") && reIndentEntry.MatchString(ln):
				open.Details = append(open.Details, strings.TrimSpace(ln))
				continue
			}
			open = nil
		}
		switch {
		case reNMLStarted.MatchString(ln):
			gl.NML = true
		case reLoaded.MatchString(ln):
			gl.Loaded[reLoaded.FindStringSubmatch(ln)[1]] = true
		case reCompile.MatchString(ln):
			open = add(LogProblem{Mod: reCompile.FindStringSubmatch(ln)[1], Kind: "compile", Summary: compileSummary("")})
		case reSkipped.MatchString(ln):
			open = add(LogProblem{Mod: reSkipped.FindStringSubmatch(ln)[1], Kind: "dependency",
				Summary: "Not loaded: a mod it needs failed to compile."})
		case reMissing.MatchString(ln):
			open = add(LogProblem{Mod: reMissing.FindStringSubmatch(ln)[1], Kind: "dependency",
				Summary: "Not loaded: it needs mods that aren't installed."})
		case reIncompat.MatchString(ln):
			open = add(LogProblem{Mod: reIncompat.FindStringSubmatch(ln)[1], Kind: "incompatible",
				Summary: "Not loaded: it can't run together with another installed mod."})
		case reOutdated.MatchString(ln):
			add(LogProblem{Mod: reOutdated.FindStringSubmatch(ln)[1], Kind: "outdated",
				Summary: "Its compiled copy was out of date; NeoModLoader rebuilds it next start. If this repeats, update or remove the mod."})
		case reDisabled.MatchString(ln):
			add(LogProblem{Mod: reDisabled.FindStringSubmatch(ln)[1], Kind: "disabled",
				Summary: "It crashed while starting, so NeoModLoader switched it off for this session."})
		case reNoEntry.MatchString(ln):
			add(LogProblem{Mod: reNoEntry.FindStringSubmatch(ln)[1], Kind: "no-entry",
				Summary: "It compiled, but has no mod class NeoModLoader can start: it may be for another mod loader."})
		}
	}
	return gl
}

// compileSummary explains a C# compiler error in plain words.
func compileSummary(code string) string {
	switch code {
	case "CS0246", "CS0234", "CS0012":
		return "Didn't compile: it uses something that isn't there (another mod it needs, or a different WorldBox or NeoModLoader version)."
	case "CS0117", "CS1061", "CS0103", "CS0122", "CS1501", "CS7036", "CS0115":
		return "Didn't compile: it was written for a different WorldBox or NeoModLoader version."
	case "CS0029", "CS1503", "CS0266":
		return "Didn't compile: the game's code changed in a way it doesn't expect (made for another WorldBox version)."
	}
	return "Didn't compile."
}

// ProblemsFor returns what the log says about a mod (by UID or name).
func (gl *GameLog) ProblemsFor(uid, name string) []LogProblem {
	var out []LogProblem
	for _, p := range gl.Problems {
		if p.Mod == uid || strings.EqualFold(p.Mod, name) {
			out = append(out, p)
		}
	}
	return out
}
