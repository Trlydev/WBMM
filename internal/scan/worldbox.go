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
		if f.Rule == "symlink" && linkStaysInside(root, f.File) {
			r.Findings[i].Detail = symlinkInside
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
	dataIndicators(r, root, roles)
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

// Folders NML's compiler skips (Paths.CompileIgnoreSearchDirectories). It
// compares names exactly: a folder called "Bin" is compiled.
var nmlSkipDirs = map[string]bool{
	"bin": true, "obj": true, "Properties": true, "packages": true, "packages.config": true,
	"packages-lock.json": true, "packages-lock.xml": true,
	"GameResources": true, "GameResourcesReplace": true, "AssetBundles": true,
}

// classify finds the mod folders (each has a mod.json) and decides, like
// NML, which code files in them run: top-level DLLs (NML loads them all and
// treats the mod as precompiled), DLLs in Assemblies/, and every .cs file
// outside the folders NML skips. Where NML's behaviour depends on the
// platform (whether "X.DLL" counts as a DLL, so whether the .cs files are
// compiled), both readings count. A BepInEx-type mod is linked into
// BepInEx/plugins, which loads DLLs from every subfolder, so all its DLLs
// count. Without any mod.json (a loose download), every code file counts.
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
		if bepinexMod(filepath.Join(dir, "mod.json")) {
			filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".dll") {
					rel, _ := filepath.Rel(root, p)
					m.files[rel] = loaded
				}
				return nil
			})
		}
		if es, err := os.ReadDir(dir); err == nil {
			for _, e := range es {
				if e.IsDir() && strings.EqualFold(e.Name(), "Assemblies") {
					if as, err := os.ReadDir(filepath.Join(dir, e.Name())); err == nil {
						for _, a := range as {
							if !a.IsDir() && strings.EqualFold(filepath.Ext(a.Name()), ".dll") {
								m.files[join(filepath.Join(e.Name(), a.Name()))] = loaded
							}
						}
					}
				}
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dll") {
					m.files[join(e.Name())] = loaded
				}
			}
		}
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != dir && (strings.HasPrefix(name, ".") || nmlSkipDirs[name]) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(name, ".cs") && !strings.HasPrefix(name, ".") {
				rel, _ := filepath.Rel(root, p)
				if _, isDLL := m.files[rel]; !isDLL {
					m.files[rel] = compiled
				}
			}
			return nil
		})
	}
	return m
}

// bepinexMod reports whether a mod.json declares a BepInEx-type mod.
func bepinexMod(declPath string) bool {
	b, err := os.ReadFile(declPath)
	if err != nil {
		return false
	}
	low := strings.ToLower(decodeText(b))
	i := strings.Index(low, `"modtype"`)
	return i >= 0 && strings.Contains(low[i:min(len(low), i+60)], "bepinex")
}

const replacesGame = "a whole replacement for the game's own code (an old-style mod, not an NML mod): it can do anything the game can, and its changes can't be told apart from the game's"

const symlinkInside = "a symbolic link to another file of the mod (archives made on Linux or macOS sometimes have them)"

// linkStaysInside reports whether the symlink rel points inside root.
func linkStaysInside(root, rel string) bool {
	target, err := os.Readlink(filepath.Join(root, rel))
	if err != nil || filepath.IsAbs(target) {
		return false
	}
	dest := filepath.Clean(filepath.Join(root, filepath.Dir(rel), target))
	r, err := filepath.Rel(root, dest)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

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
// of the game's own code (mod templates ship Assembly-CSharp as a compile
// reference, often from another game version, so hashes don't match). NML
// loads Assemblies/ DLLs with Assembly.LoadFrom, which hands back the
// already-loaded assembly of the same identity; Assembly-CSharp's identity
// is always the same (no version, no key) and the game has loaded it, so a
// copy never runs. Anything else must match by hash (known libraries, the
// game's own files, NML's releases); a name proves nothing. Top-level DLLs
// are loaded from their bytes, which always loads them, so they're never
// vouched for by name. A loose download (no mod.json) with a top-level
// Assembly-CSharp.dll is an old-style mod replacing the game's code.
func copiesOfGameCode(root string, roles roleMap, opt Options) map[string]string {
	out := map[string]string{}
	for rel, role := range roles.files {
		if role != loaded {
			continue
		}
		inAssemblies := strings.EqualFold(filepath.Base(filepath.Dir(rel)), "Assemblies")
		if !inAssemblies && !(len(roles.roots) == 0 && strings.EqualFold(filepath.Base(rel), "Assembly-CSharp.dll")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		info, err := ReadCLR(b)
		if err != nil || !strings.EqualFold(info.Assembly, "Assembly-CSharp") {
			continue
		}
		if inAssemblies {
			out[rel] = "a copy of the game's own code (a compile reference; the game uses its own)"
		} else {
			out[rel] = replacesGame
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
	"json-gadget": High, "scan-error": High, "nml-workshop": High, "uses-mods-folder": Info,
	"workshop-upload": Critical,

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
		if f.Detail == symlinkInside {
			return High, true
		}
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
	// literal: a bare word that counts only inside a string, not in a
	// comment or a name.
	literal bool
}

var indicators_ = []indicator{
	// Secrets: browser logins and cookies, Discord tokens, Steam sessions,
	// crypto wallets, Telegram sessions.
	{"steals-secrets", Critical, `login data`, "browser saved passwords", false},
	{"steals-secrets", Critical, `\local state`, "the browser's password key", false},
	{"steals-secrets", Critical, `/local state`, "the browser's password key", false},
	{"steals-secrets", Critical, `network\cookies`, "browser cookies", false},
	{"steals-secrets", Critical, `\user data\default`, "a browser profile", false},
	{"steals-secrets", Critical, `logins.json`, "Firefox saved passwords", false},
	{"steals-secrets", Critical, `key4.db`, "Firefox's password key", false},
	{"steals-secrets", Critical, `cryptunprotectdata`, "Windows' stored-secret decryption (DPAPI)", false},
	{"steals-secrets", Critical, `discord\local storage`, "Discord's login token store", false},
	{"steals-secrets", Critical, `discord/local storage`, "Discord's login token store", false},
	{"steals-secrets", Critical, `discordcanary`, "Discord's files", false},
	{"steals-secrets", Critical, `[\w-]{24}\.[\w-]{6}\.[\w-]`, "the shape of a Discord token (a token grabber's search pattern)", false},
	{"steals-secrets", Critical, `mfa\.[\w-]{84}`, "the shape of a Discord token (a token grabber's search pattern)", false},
	{"steals-secrets", Critical, `loginusers.vdf`, "Steam's saved logins", false},
	{"steals-secrets", Critical, `\ssfn`, "Steam's login session files", false},
	{"steals-secrets", Critical, `/ssfn`, "Steam's login session files", false},
	{"steals-secrets", Critical, `wallet.dat`, "a crypto wallet", false},
	{"steals-secrets", Critical, `exodus.wallet`, "a crypto wallet", false},
	{"steals-secrets", Critical, `nkbihfbeogaeaoehlefnkodbefgpgknn`, "the MetaMask wallet extension", false},
	{"steals-secrets", Critical, `telegram desktop\tdata`, "Telegram's login session", false},
	// Where stolen data goes.
	{"exfiltration", Critical, `discord.com/api/webhooks`, "a Discord webhook (a common way to send stolen data)", false},
	{"exfiltration", Critical, `discordapp.com/api/webhooks`, "a Discord webhook (a common way to send stolen data)", false},
	{"exfiltration", Critical, `api.telegram.org/bot`, "a Telegram bot (a common way to send stolen data)", false},
	{"exfiltration", Critical, `webhook.site`, "a request-capture site", false},
	{"exfiltration", Critical, `pastebin.com/raw`, "a raw paste (a common way to fetch the next stage)", false},
	{"exfiltration", Critical, `transfer.sh`, "an anonymous file host", false},
	{"exfiltration", Critical, `gofile.io`, "an anonymous file host", false},
	{"exfiltration", Critical, `anonfiles`, "an anonymous file host", false},
	{"exfiltration", Critical, `ngrok.io`, "a tunnel to someone's computer", false},
	{"exfiltration", Critical, `ngrok-free.app`, "a tunnel to someone's computer", false},
	// Staying on the computer.
	{"persistence", Critical, `currentversion\run`, "the Windows autostart key", false},
	{"persistence", Critical, `\start menu\programs\startup`, "the Windows Startup folder", false},
	{"persistence", Critical, `shell:startup`, "the Windows Startup folder", false},
	{"persistence", Critical, `schtasks`, "Windows scheduled tasks", true},
	{"persistence", Critical, `/.config/autostart`, "the Linux autostart folder", false},
	{"persistence", Critical, `/.config/systemd/user`, "Linux user services", false},
	{"persistence", Critical, `crontab`, "Linux scheduled jobs", true},
	// Running other programs the way droppers do.
	{"dropper", Critical, `powershell -e`, "an encoded PowerShell command", false},
	{"dropper", Critical, `-encodedcommand`, "an encoded PowerShell command", false},
	{"dropper", Critical, `-executionpolicy bypass`, "PowerShell with its safety switched off", false},
	{"dropper", Critical, `certutil -urlcache`, "a Windows tool misused to download files", false},
	{"dropper", Critical, `bitsadmin /transfer`, "a Windows tool misused to download files", false},
	{"dropper", Critical, `mshta`, "a Windows script host malware uses", true},
	{"dropper", Critical, `regsvr32 /s /n /u`, "a Windows tool misused to run code", false},
	{"dropper", High, `cmd.exe /c`, "a Windows command line", false},
	{"dropper", High, `/bin/sh -c`, "a Linux shell command", false},
	{"dropper", High, `bash -c`, "a Linux shell command", false},
	// Spreading through the Workshop or Steam chat, as the 2026 People
	// Playground and PlateUp! worms did.
	{"workshop-upload", Critical, `submititemupdate`, "uploading to the Steam Workshop", false},
	{"workshop-upload", Critical, `startitemupdate`, "uploading to the Steam Workshop", false},
	{"workshop-upload", Critical, `setitemcontent`, "uploading to the Steam Workshop", false},
	{"steam-messages", Critical, `replytofriendmessage`, "sending Steam chat messages", false},
	{"steam-messages", Critical, `sendclanchatmessage`, "sending Steam group chat messages", false},
	// Other mods and the loader. Naming them is ordinary (compatibility
	// checks); writing to them is how a worm spreads (see combine).
	{"names-loader-files", High, `neomodloader.dll`, "NeoModLoader's own program", false},
	{"names-loader-files", High, `mod_compile_records`, "NeoModLoader's mod on/off list", false},
	{"names-loader-files", High, `compiledmods`, "NeoModLoader's compiled mods", false},
	{"names-loader-files", High, `workshop/content/1206560`, "other WorldBox Workshop mods", false},
	{"names-loader-files", High, `workshop\content\1206560`, "other WorldBox Workshop mods", false},
	{"names-loader-files", High, `_data/managed`, "the game's own code folder", false},
	{"names-loader-files", High, `_data\managed`, "the game's own code folder", false},
}

var reB64Quoted = regexp.MustCompile(`"([A-Za-z0-9+/]{16,}={0,2})"`)

var (
	// "a" + "b" and $"a" + @"b": one string.
	reConcat = regexp.MustCompile(`"\s*\+\s*[@$]{0,2}"`)
	// Literal arguments next to each other, as in Path.Combine(dir, "a",
	// "b"): joined as a path.
	reArgs   = regexp.MustCompile(`"\s*,\s*[@$]{0,2}"`)
	reEscape = regexp.MustCompile(`\\(x[0-9a-fA-F]{1,4}|u[0-9a-fA-F]{4}|U[0-9a-fA-F]{8}|\\)`)
)

// joinLiterals rewrites C# source so strings built from pieces read as the
// whole: concatenated literals joined, adjacent literal arguments joined
// with a path separator, and escapes (\\, \x41, \u0041) decoded.
func joinLiterals(src string) string {
	src = reConcat.ReplaceAllString(src, "")
	src = reArgs.ReplaceAllString(src, `\`)
	return reEscape.ReplaceAllStringFunc(src, func(e string) string {
		if e == `\\` {
			return `\`
		}
		var r rune
		fmt.Sscanf(e[2:], "%x", &r)
		if r == 0 || r > 0x10ffff {
			return e
		}
		return string(r)
	})
}

// inLiteral reports whether needle occurs inside a string literal of
// (lowercased) C# source: an odd number of quotes before it on its line.
func inLiteral(t []byte, needle string) bool {
	n := []byte(needle)
	for off := 0; ; {
		i := bytes.Index(t[off:], n)
		if i < 0 {
			return false
		}
		i += off
		start := bytes.LastIndexByte(t[:i], '\n') + 1
		if bytes.Count(t[start:i], []byte{'"'})%2 == 1 {
			return true
		}
		off = i + 1
	}
}

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
		src := decodeText(b)
		texts = append(texts, []byte(strings.ToLower(src)), []byte(strings.ToLower(joinLiterals(src))))
		// What base64 literals decode to.
		for _, m := range reB64Quoted.FindAllStringSubmatch(joinLiterals(src), -1) {
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
				if in.literal && !isDLL && !inLiteral(t, in.needle) {
					continue
				}
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

// Text files a mod's code can read at run time: a webhook address or a list
// of secret paths kept there instead of in the code is still the mod's.
var dataExt = map[string]bool{".json": true, ".txt": true, ".xml": true, ".cfg": true, ".ini": true,
	".yml": true, ".yaml": true, ".csv": true, ".config": true, ".dat": true}

// dataIndicators checks the text files of each mod folder for stolen-data
// destinations and secret paths (HIGH: data alone does nothing).
func dataIndicators(r *Report, root string, m roleMap) {
	for _, mr := range m.roots {
		filepath.WalkDir(filepath.Join(root, mr), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !dataExt[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			if fi, err := d.Info(); err != nil || fi.Size() > 4<<20 {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			low := []byte(strings.ToLower(joinLiterals(decodeText(b))))
			rel, _ := filepath.Rel(root, p)
			seen := map[string]bool{}
			for _, in := range indicators_ {
				if (in.rule == "exfiltration" || in.rule == "steals-secrets") && !seen[in.what] && containsWord(low, in.needle) {
					seen[in.what] = true
					r.add(High, in.rule, rel, 0, fmt.Sprintf("a data file names %s: %q (the mod's code may read it)", in.what, in.needle))
				}
			}
			return nil
		})
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
	type caps struct{ network, runs, writes, loader, userFiles bool }
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
		case "env-paths":
			c.userFiles = true
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
		if c.userFiles && c.network {
			r.add(High, "reads-and-sends", file, 0, "reads your user folders and connects to the internet in the same code: check what it sends")
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
