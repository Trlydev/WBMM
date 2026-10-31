package scan

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// WorldBox mods are not People Playground mods. NeoModLoader (NML) compiles
// every .cs file of a mod with the whole .NET framework available, or loads
// its DLLs as they are, and runs them inside the game with no sandbox. So
// reflection, Harmony patches, file access, even network calls are normal
// in WorldBox mods (a calibration run over ~150 real mods found all of
// them). What malware needs and ordinary mods don't is narrower: secrets to
// steal, somewhere to send them, a way to spread (Workshop uploads, other
// mods' files), a way to run hidden code, and a way to stay. Those are
// CRITICAL here; ordinary capabilities are notes for a reviewer.
//
// Mod scans a mod folder (or an unpacked archive holding several) this way:
// SWG's checks run on every file, then each finding is weighed by whether
// NML would load the file it's in and by what it means in a WorldBox mod,
// and the WorldBox rules below are added.
func Mod(root string, opt Options) (*Report, error) {
	r, err := DirWith(root, opt)
	if err != nil {
		return r, err
	}
	roles := classify(root)
	copies := copiesOfGameCode(root, roles, opt)
	for i, f := range r.Findings {
		if f.Rule == "disguised-binary" && isNativeLibrary(filepath.Join(root, f.File)) {
			r.Findings[i].Detail = disguisedLibrary
		}
	}
	var kept []Finding
	for rel, what := range copies {
		if what == replacesGame {
			r.add(High, "replaces-game-code", rel, 0, what)
		}
	}
	for _, f := range r.Findings {
		if what, ok := copies[f.File]; ok {
			if f.Rule == "replaces-game-code" {
				kept = append(kept, f)
			} else if what != replacesGame && (f.Rule == "compiled-code" || f.Rule == "game-library") {
				f.Severity, f.Rule, f.Detail = Info, "game-library", what
				kept = append(kept, f)
			}
			continue
		}
		role := roles.of(f.File)
		sev, ok := worldboxSeverity(f, role)
		if !ok {
			continue
		}
		if role == inert && sev < f.Severity && f.Severity >= Medium {
			f.Detail = "not loaded by NeoModLoader: " + f.Detail
		}
		f.Severity = sev
		kept = append(kept, f)
	}
	r.Findings = kept
	// Libraries identified by hash (official releases, the game's own
	// files) are what they are: the game's own code uploads worlds to the
	// Workshop, for one. The text checks are for the mod's code.
	vouched := map[string]bool{}
	for rel := range copies {
		vouched[rel] = true
	}
	for _, f := range r.Findings {
		if f.Rule == "known-library" || f.Rule == "game-library" {
			vouched[f.File] = true
		}
	}
	for rel, role := range roles.files {
		if role == inert || vouched[rel] {
			continue
		}
		guard(r, rel, func() { indicators(r, root, rel) })
	}
	combine(r, roles)
	sort.SliceStable(r.Findings, func(i, j int) bool {
		if r.Findings[i].Severity != r.Findings[j].Severity {
			return r.Findings[i].Severity > r.Findings[j].Severity
		}
		return r.Findings[i].File < r.Findings[j].File
	})
	return r, nil
}

// ---------- what NML loads ----------

type role int

const (
	inert    role = iota // NML never compiles or loads it
	compiled             // a .cs file NML compiles
	loaded               // a DLL NML loads
)

type roleMap struct {
	files map[string]role // code files (rel path) and their role
	roots []string        // mod folders (rel, "." for root)
}

func (m roleMap) of(rel string) role {
	if r, ok := m.files[rel]; ok {
		return r
	}
	return inert
}

// Folders NML's compiler skips (Paths.CompileIgnoreSearchDirectories), and
// IDE output.
var nmlSkipDirs = map[string]bool{
	"bin": true, "obj": true, "properties": true, "packages": true,
	"gameresources": true, "gameresourcesreplace": true, "assetbundles": true,
}

// classify finds the mod folders (each has a mod.json) and decides, like
// NML, which code files in them run: a mod with DLLs at its top level is
// "precompiled" and NML loads every top-level DLL instead of compiling;
// otherwise it compiles every .cs file outside the skipped folders. DLLs in
// Assemblies/ are loaded either way. Without any mod.json (a loose
// download), every code file counts as running.
func classify(root string) roleMap {
	m := roleMap{files: map[string]role{}}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), "mod.json") {
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			m.roots = append(m.roots, rel)
		}
		return nil
	})
	if len(m.roots) == 0 {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if ext := strings.ToLower(filepath.Ext(p)); ext == ".cs" || ext == ".dll" {
					rel, _ := filepath.Rel(root, p)
					m.files[rel] = compiled
					if ext == ".dll" {
						m.files[rel] = loaded
					}
				}
			}
			return nil
		})
		return m
	}
	for _, mr := range m.roots {
		dir := filepath.Join(root, mr)
		join := func(name string) string { return filepath.Join(mr, name) }
		precompiled := false
		if es, err := os.ReadDir(dir); err == nil {
			for _, e := range es {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dll") {
					precompiled = true
					m.files[join(e.Name())] = loaded
				}
			}
		}
		if es, err := os.ReadDir(filepath.Join(dir, "Assemblies")); err == nil {
			for _, e := range es {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dll") {
					m.files[join(filepath.Join("Assemblies", e.Name()))] = loaded
				}
			}
		}
		if precompiled {
			continue
		}
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := strings.ToLower(d.Name())
			if d.IsDir() {
				if p != dir && (strings.HasPrefix(name, ".") || nmlSkipDirs[name]) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(name, ".cs") && !strings.HasPrefix(name, ".") {
				rel, _ := filepath.Rel(root, p)
				m.files[rel] = compiled
			}
			return nil
		})
	}
	return m
}

const replacesGame = "a whole replacement for the game's own code (an old-style mod, not an NML mod): it can do anything the game can, and its changes can't be told apart from the game's"

const disguisedLibrary = "a native library (DLL) under another file name"

// isNativeLibrary reports whether p is a Windows DLL or a Linux shared
// library (rather than a program).
func isNativeLibrary(p string) bool {
	b, err := readHead(p, 4096)
	if err != nil || len(b) < 64 {
		return false
	}
	if b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F' {
		return len(b) > 17 && b[16] == 3 // ET_DYN
	}
	pe := int(b[60]) | int(b[61])<<8 | int(b[62])<<16 | int(b[63])<<24
	if pe < 0 || pe+24 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
		return false
	}
	chars := int(b[pe+22]) | int(b[pe+23])<<8
	return chars&0x2000 != 0 // IMAGE_FILE_DLL
}

// copiesOfGameCode finds DLLs in a mod's Assemblies folder that are copies
// of the game's own code or of NeoModLoader (mod templates ship them as
// compile references, often from another game version, so their hashes
// don't match). NML loads Assemblies/ DLLs with Assembly.LoadFrom, which
// hands back the already-loaded assembly of the same name, so these never
// run as the mod's code. Top-level DLLs are loaded from their bytes
// instead, which always loads them, so a name proves nothing there and they
// are scanned like any other code.
func copiesOfGameCode(root string, roles roleMap, opt Options) map[string]string {
	out := map[string]string{}
	for rel, role := range roles.files {
		if role != loaded {
			continue
		}
		inAssemblies := strings.EqualFold(filepath.Base(filepath.Dir(rel)), "Assemblies")
		// Without a mod.json this isn't an NML mod; old-style mods replaced
		// the game's Assembly-CSharp.dll wholesale.
		if !inAssemblies && !(len(roles.roots) == 0 && strings.EqualFold(filepath.Base(rel), "Assembly-CSharp.dll")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		info, err := ReadCLR(b)
		if err != nil || info.Assembly == "" {
			continue
		}
		name := info.Assembly
		if !inAssemblies {
			if strings.EqualFold(name, "Assembly-CSharp") {
				out[rel] = replacesGame
			}
			continue
		}
		switch {
		case strings.EqualFold(name, "NeoModLoader"):
			out[rel] = "a copy of NeoModLoader (a compile reference; NML uses its own)"
		case strings.EqualFold(name, "Assembly-CSharp") || strings.HasPrefix(name, "UnityEngine"):
			out[rel] = "a copy of the game's own code (a compile reference; the game uses its own)"
		case opt.GameManaged != "":
			if _, err := os.Stat(filepath.Join(opt.GameManaged, name+".dll")); err == nil {
				out[rel] = "a copy of the game's " + name + " (a compile reference; the game uses its own)"
			}
		}
	}
	return out
}

// ---------- how much each finding matters in a WorldBox mod ----------

var worldboxRules = map[string]Severity{
	// Spreading and account abuse: no WorldBox mod in the calibration set
	// touches the Workshop or friends APIs (NML does uploads itself).
	"steam-ugc": Critical, "steamugc": Critical, "steam-friends": Critical, "steamfriends": Critical,
	"steam-auth": Critical, "steamuser": Critical,
	// Hidden programs and files pretending to be something else.
	"hidden-executable": Critical, "embedded-executable": Critical, "disguised-binary": Critical,
	"symlink": Critical,

	// Capabilities real mods use (AI text over HTTP, native file pickers,
	// opening a folder, dev-time reloaders): worth a look, not a verdict.
	"network": High, "httpclient": High, "webclient": High,
	"process-spawn": High, "processstartinfo": High,
	"native-interop": High, "dllimport": High, "kernel32": High, "user32": High, "native-code": High,
	"code-loader": High, "registry": High, "shell-string": High, "deserialization": High,
	"encoded-code": High, "xor-decoding": High, "xor-hidden-string": High, "split-strings": High,
	"char-code-strings": High, "unicode-escapes": High, "hidden-characters": High,
	"self-replication": High, "embedded-library": High,
	"writes-scripts": High, "steamworks-any": High, "steamworks": High, "worm-dll": High,
	"json-gadget": High, "scan-error": High,

	"file-delete": Medium, "mass-delete": Medium, "process-control": Medium, "base64": Medium,
	"encoded-blob": Medium, "encoded-string": Medium, "sensitive-string": Medium,
	"too-large-to-inspect": Medium, "nested-archive": Medium, "unsafe-code": Medium,
	"game-path-tamper": Medium, "disables-protection": Medium,

	// Everyday WorldBox modding.
	"reflection-invoke": Info, "reflection-enumerate": Info, "reflection-by-name": Info,
	"reflection-computed-type": Info, "reflection-sensitive-type": Info, "dynamic-code": Info,
	"threads": Info, "open-url": Info, "file-write": Info, "file-enumerate": Info, "namespace": Info,
	"compiled-code": Info, "known-library": Info, "game-library": Info, "env-paths": Info,
	"game-path-write": Info,
}

// Programs and scripts NML never runs, but a person might double-click.
var runnableExt = map[string]Severity{
	".exe": High, ".scr": High, ".com": High, ".msi": High, ".lnk": High, ".pif": High, ".hta": High,
	".vbs": High, ".vbe": High, ".js": High, ".jse": High, ".wsf": High, ".cpl": High, ".jar": High,
	".reg": High, ".bat": Medium, ".cmd": Medium, ".ps1": Medium, ".psm1": Medium, ".sh": Medium,
	".so": Medium, ".dylib": Medium,
}

func worldboxSeverity(f Finding, r role) (Severity, bool) {
	if strings.HasPrefix(f.Rule, "manifest-") {
		return 0, false // People Playground's mod.json format
	}
	if f.Rule == "executable-file" {
		if s, ok := runnableExt[strings.ToLower(filepath.Ext(f.File))]; ok {
			return s, true
		}
		return Info, true // a DLL: judged by what it contains
	}
	// A symlink is wrong anywhere. A file disguised as something else is
	// too, if it's a program; a native library under another name is a
	// trick some mods use to ship one (a top-level .dll would make NML treat
	// the mod as precompiled), so it is worth a look, not a verdict.
	switch f.Rule {
	case "symlink":
		return Critical, true
	case "disguised-binary":
		if f.Detail == disguisedLibrary {
			return High, true
		}
		return Critical, true
	}
	sev, ok := worldboxRules[f.Rule]
	if !ok {
		sev = f.Severity
	}
	if r == inert {
		return Info, true
	}
	return sev, true
}

// ---------- WorldBox rules ----------

// indicator is text that malware needs and mods have no use for, checked
// in the code NML runs: .cs source, the strings inside loaded DLLs, and
// what base64 literals in either decode to.
type indicator struct {
	rule   string
	sev    Severity
	needle string
	what   string
}

var indicators_ = []indicator{
	// Secrets: browser logins and cookies, Discord tokens, Steam sessions,
	// crypto wallets, Telegram sessions.
	{"steals-secrets", Critical, `login data`, "browser saved passwords"},
	{"steals-secrets", Critical, `\local state`, "the browser's password key"},
	{"steals-secrets", Critical, `/local state`, "the browser's password key"},
	{"steals-secrets", Critical, `network\cookies`, "browser cookies"},
	{"steals-secrets", Critical, `\user data\default`, "a browser profile"},
	{"steals-secrets", Critical, `logins.json`, "Firefox saved passwords"},
	{"steals-secrets", Critical, `key4.db`, "Firefox's password key"},
	{"steals-secrets", Critical, `cryptunprotectdata`, "Windows' stored-secret decryption (DPAPI)"},
	{"steals-secrets", Critical, `discord\local storage`, "Discord's login token store"},
	{"steals-secrets", Critical, `discord/local storage`, "Discord's login token store"},
	{"steals-secrets", Critical, `discordcanary`, "Discord's files"},
	{"steals-secrets", Critical, `[\w-]{24}\.[\w-]{6}\.[\w-]`, "the shape of a Discord token (a token grabber's search pattern)"},
	{"steals-secrets", Critical, `mfa\.[\w-]{84}`, "the shape of a Discord token (a token grabber's search pattern)"},
	{"steals-secrets", Critical, `loginusers.vdf`, "Steam's saved logins"},
	{"steals-secrets", Critical, `\ssfn`, "Steam's login session files"},
	{"steals-secrets", Critical, `/ssfn`, "Steam's login session files"},
	{"steals-secrets", Critical, `wallet.dat`, "a crypto wallet"},
	{"steals-secrets", Critical, `exodus.wallet`, "a crypto wallet"},
	{"steals-secrets", Critical, `nkbihfbeogaeaoehlefnkodbefgpgknn`, "the MetaMask wallet extension"},
	{"steals-secrets", Critical, `telegram desktop\tdata`, "Telegram's login session"},
	// Where stolen data goes.
	{"exfiltration", Critical, `discord.com/api/webhooks`, "a Discord webhook (a common way to send stolen data)"},
	{"exfiltration", Critical, `discordapp.com/api/webhooks`, "a Discord webhook (a common way to send stolen data)"},
	{"exfiltration", Critical, `api.telegram.org/bot`, "a Telegram bot (a common way to send stolen data)"},
	{"exfiltration", Critical, `webhook.site`, "a request-capture site"},
	{"exfiltration", Critical, `pastebin.com/raw`, "a raw paste (a common way to fetch the next stage)"},
	{"exfiltration", Critical, `transfer.sh`, "an anonymous file host"},
	{"exfiltration", Critical, `gofile.io`, "an anonymous file host"},
	{"exfiltration", Critical, `anonfiles`, "an anonymous file host"},
	{"exfiltration", Critical, `ngrok.io`, "a tunnel to someone's computer"},
	{"exfiltration", Critical, `ngrok-free.app`, "a tunnel to someone's computer"},
	// Staying on the computer.
	{"persistence", Critical, `currentversion\run`, "the Windows autostart key"},
	{"persistence", Critical, `\start menu\programs\startup`, "the Windows Startup folder"},
	{"persistence", Critical, `shell:startup`, "the Windows Startup folder"},
	{"persistence", Critical, `schtasks`, "Windows scheduled tasks"},
	{"persistence", Critical, `/.config/autostart`, "the Linux autostart folder"},
	{"persistence", Critical, `/.config/systemd/user`, "Linux user services"},
	{"persistence", Critical, `crontab`, "Linux scheduled jobs"},
	// Running other programs the way droppers do.
	{"dropper", Critical, `powershell -e`, "an encoded PowerShell command"},
	{"dropper", Critical, `-encodedcommand`, "an encoded PowerShell command"},
	{"dropper", Critical, `-executionpolicy bypass`, "PowerShell with its safety switched off"},
	{"dropper", Critical, `certutil -urlcache`, "a Windows tool misused to download files"},
	{"dropper", Critical, `bitsadmin /transfer`, "a Windows tool misused to download files"},
	{"dropper", Critical, `mshta`, "a Windows script host malware uses"},
	{"dropper", Critical, `regsvr32 /s /n /u`, "a Windows tool misused to run code"},
	{"dropper", High, `cmd.exe /c`, "a Windows command line"},
	{"dropper", High, `/bin/sh -c`, "a Linux shell command"},
	{"dropper", High, `bash -c`, "a Linux shell command"},
	// Spreading through the Workshop or Steam chat, as the 2026 People
	// Playground and PlateUp! worms did.
	{"workshop-upload", Critical, `submititemupdate`, "uploading to the Steam Workshop"},
	{"workshop-upload", Critical, `startitemupdate`, "uploading to the Steam Workshop"},
	{"workshop-upload", Critical, `setitemcontent`, "uploading to the Steam Workshop"},
	{"steam-messages", Critical, `replytofriendmessage`, "sending Steam chat messages"},
	{"steam-messages", Critical, `sendclanchatmessage`, "sending Steam group chat messages"},
	// Other mods and the loader. Naming them is ordinary (compatibility
	// checks); writing to them is how a worm spreads (see combine).
	{"names-loader-files", High, `neomodloader.dll`, "NeoModLoader's own program"},
	{"names-loader-files", High, `mod_compile_records`, "NeoModLoader's mod on/off list"},
	{"names-loader-files", High, `compiledmods`, "NeoModLoader's compiled mods"},
	{"names-loader-files", High, `workshop/content/1206560`, "other WorldBox Workshop mods"},
	{"names-loader-files", High, `workshop\content\1206560`, "other WorldBox Workshop mods"},
	{"names-loader-files", High, `_data/managed`, "the game's own code folder"},
	{"names-loader-files", High, `_data\managed`, "the game's own code folder"},
}

var reB64Quoted = regexp.MustCompile(`"([A-Za-z0-9+/]{16,}={0,2})"`)

// indicators checks one code file NML runs.
func indicators(r *Report, root, rel string) {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return
	}
	isDLL := strings.EqualFold(filepath.Ext(rel), ".dll")
	var texts [][]byte
	if isDLL {
		low := bytes.ToLower(b)
		texts = append(texts, low, utf16Lower(low))
		// Libraries carved out of it (compressed resources hide their text).
		for _, pl := range r.payloads[rel] {
			pl = bytes.ToLower(pl)
			texts = append(texts, pl, utf16Lower(pl))
		}
	} else {
		src := strings.ToLower(decodeText(b))
		// Also as the strings read once C# escapes are undone ("\\" is
		// one backslash).
		texts = append(texts, []byte(src), []byte(strings.ReplaceAll(src, `\\`, `\`)))
		// What base64 literals decode to.
		for _, m := range reB64Quoted.FindAllStringSubmatch(decodeText(b), -1) {
			if d, err := base64.StdEncoding.DecodeString(m[1]); err == nil {
				texts = append(texts, bytes.ToLower(d))
			}
		}
	}
	seen := map[string]bool{}
	for _, in := range indicators_ {
		if seen[in.rule+in.what] {
			continue
		}
		for i, t := range texts {
			if containsWord(t, in.needle) {
				seen[in.rule+in.what] = true
				where := "names"
				if !isDLL && i > 1 {
					where = "hides in base64"
				}
				r.add(in.sev, in.rule, rel, 0, fmt.Sprintf("%s %s: %q", where, in.what, in.needle))
				break
			}
		}
	}
}

// containsWord reports whether needle occurs in t as a word of its own: a
// needle that starts with a letter or digit must not be preceded by one,
// and a word-like needle ("transfer.sh") must not be followed by one either,
// so it doesn't match "transfer.should". Prefixes of URLs and commands
// ("api.telegram.org/bot", "powershell -e") may run on.
func containsWord(t []byte, needle string) bool {
	n := []byte(needle)
	alnum := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' }
	prefix := strings.ContainsAny(needle, " /\\")
	for off := 0; ; {
		i := bytes.Index(t[off:], n)
		if i < 0 {
			return false
		}
		i += off
		end := i + len(n)
		if (!alnum(n[0]) || i == 0 || !alnum(t[i-1])) && (prefix || !alnum(n[len(n)-1]) || end == len(t) || !alnum(t[end])) {
			return true
		}
		off = i + 1
	}
}

// utf16Lower reads lowercased DLL bytes as UTF-16LE, the way .NET stores
// string literals: an ASCII character followed by a zero byte becomes that
// character, anything else a line break, so ASCII needles match directly.
func utf16Lower(b []byte) []byte {
	out := make([]byte, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if b[i+1] == 0 && b[i] != 0 {
			out = append(out, b[i])
		} else {
			out = append(out, '\n')
		}
	}
	return out
}

// combine adds the findings that come from capabilities together. Within
// one file (one class of code) they are what droppers and worms look like;
// spread across a mod's files (a bundled tool here, an AI client there)
// they are worth a look.
func combine(r *Report, m roleMap) {
	type caps struct{ network, runs, writes, loader bool }
	perFile := map[string]*caps{}
	for _, f := range r.Findings {
		if m.of(f.File) == inert {
			continue
		}
		c := perFile[f.File]
		if c == nil {
			c = &caps{}
			perFile[f.File] = c
		}
		switch f.Rule {
		case "network", "httpclient", "webclient":
			c.network = true
		case "code-loader", "process-spawn", "processstartinfo", "hidden-executable", "dropper":
			c.runs = true
		case "file-write", "file-delete", "mass-delete", "writes-scripts", "self-replication", "game-path-tamper":
			c.writes = true
		case "names-loader-files":
			c.loader = true
		}
	}
	modOf := func(file string) string {
		best := "."
		for _, mr := range m.roots {
			if mr != "." && (file == mr || strings.HasPrefix(file, mr+string(filepath.Separator))) && len(mr) > len(best) {
				best = mr
			}
		}
		return best
	}
	perMod := map[string]*caps{}
	for file, c := range perFile {
		if c.network && c.runs {
			r.add(Critical, "download-and-run", file, 0, "connects to the internet and runs or loads programs in the same code: the pattern of a dropper that fetches its payload")
		}
		if c.loader && c.writes {
			r.add(Critical, "tampers-with-mods", file, 0, "writes files and names NeoModLoader's, the game's or other mods' files in the same code: the way a worm spreads into other mods")
		}
		mod := modOf(file)
		pm := perMod[mod]
		if pm == nil {
			pm = &caps{}
			perMod[mod] = pm
		}
		pm.network = pm.network || c.network
		pm.runs = pm.runs || c.runs
		pm.writes = pm.writes || c.writes
		pm.loader = pm.loader || c.loader
	}
	for mod, c := range perMod {
		if c.network && c.runs {
			r.add(High, "network-and-run", mod, 0, "parts of this mod connect to the internet and other parts run or load programs: check what each is for")
		}
	}
}
