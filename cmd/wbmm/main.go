// Command wbmm is WorldBox Mod Manager: install and manage WorldBox mods
// from GameBanana and the Steam Workshop. Run without arguments it opens
// its window; the commands below do the same things from a terminal.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Trlydev/WBMM/internal/archive"
	"github.com/Trlydev/WBMM/internal/desktop"
	"github.com/Trlydev/WBMM/internal/gui"
	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/modpack"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/scan"
	"github.com/Trlydev/WBMM/internal/selfupdate"
	"github.com/Trlydev/WBMM/internal/sources"
	"github.com/Trlydev/WBMM/internal/steam"
)

// version is set at build time (-X main.version=v0.1.N).
var version = "dev"

const usage = `WorldBox Mod Manager (wbmm) %s

Usage:
  wbmm                          open the window (also: wbmm gui)
  wbmm list                     installed mods (Mods folder and Workshop)
  wbmm search [-workshop] WORDS search GameBanana (or the Steam Workshop)
  wbmm install gb:ID [-file N]  install a GameBanana mod (ID or link)
  wbmm subscribe ws:ID          subscribe to a Workshop mod through Steam
  wbmm unsubscribe ws:ID        unsubscribe (Steam removes it)
  wbmm remove UID               delete a mod from the Mods folder
  wbmm enable UID | disable UID turn a mod on or off (game closed)
  wbmm updates [-yes]           check GameBanana mods for updates
  wbmm nml [install]            NeoModLoader status, or install/update it
  wbmm scan PATH [-json]        check a mod folder or archive for malware patterns
  wbmm check                    check every installed mod; hold risky ones
  wbmm accept UID|ws:ID         allow a mod the safety check holds
  wbmm pack save NAME [FILE]    save the installed mods as a modpack (.wbm)
  wbmm pack apply FILE|CODE [-only]  install a modpack (-only: switch off the rest)
  wbmm FILE.wbm                 open a modpack in the window
  wbmm self-update              update wbmm to the newest release
  wbmm install-app [no-desktop] install as an app (menu entry, shortcut)
  wbmm uninstall-app            remove the app (mods and settings stay)
  wbmm version

Set WBMM_GAME_DIR to use a WorldBox folder Steam doesn't know about.
`

func main() {
	args := os.Args[1:]
	// The Steam helper is a separate process started by the window; it
	// must not touch settings or consoles.
	if len(args) > 0 && args[0] == "steam-helper" {
		os.Exit(steam.RunHelper(args[1:]))
	}
	if len(args) > 0 {
		attachConsole()
	}
	sources.UserAgent = fmt.Sprintf("wbmm/%s (+https://github.com/Trlydev/WBMM)", version)
	selfupdate.Current = version
	m, err := manager.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: couldn't read wbmm's settings:", err)
	}
	// Opening a .wbm file starts wbmm with its path.
	openPack := ""
	if len(args) == 1 && strings.EqualFold(filepath.Ext(args[0]), modpack.Ext) {
		openPack, args = args[0], nil
	}
	if len(args) == 0 || args[0] == "gui" {
		setupLog()
		if err := gui.Run(m, version, openPack); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(m, args[0], args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func setupLog() {
	d, err := manager.ConfigDir()
	if err != nil {
		return
	}
	p := filepath.Join(d, "wbmm.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 2<<20 {
		os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.Printf("wbmm %s starting", version)
}

func run(m *manager.Manager, cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	switch cmd {
	case "help", "-h", "--help":
		fmt.Printf(usage, version)
	case "version", "--version":
		fmt.Println(version)

	case "list":
		mods, err := m.List()
		if err != nil {
			return err
		}
		if len(mods) == 0 {
			fmt.Println("No mods installed.")
		}
		for _, md := range mods {
			state := "on "
			if !md.Enabled {
				state = "off"
			}
			fmt.Printf("[%s] %-32s %-10s %-10s %s\n", state, md.Name, md.Version, md.Source, md.UID)
			if md.Problem != "" {
				fmt.Println("      !", md.Problem)
			}
		}

	case "search":
		ws := fs.Bool("workshop", false, "search the Steam Workshop")
		if err := fs.Parse(args); err != nil {
			return err
		}
		q := strings.Join(fs.Args(), " ")
		if *ws {
			items, _, err := sources.WSBrowse(q, "subs", sources.TagMod, 1)
			if err != nil {
				return err
			}
			for _, it := range items {
				fmt.Printf("ws:%-12s %-40s %7d subs\n", it.ID, it.Title, it.Subs)
			}
			fmt.Println("\nWorkshop mods install through Steam: open the item's page and click Subscribe.")
			return nil
		}
		mods, _, err := sources.GBList(q, "popular", 1)
		if err != nil {
			return err
		}
		for _, md := range mods {
			kind := ""
			if md.IsMap() {
				kind = " (map)"
			}
			fmt.Printf("gb:%-8d %-40s by %s%s\n", md.ID, md.Name, md.Submitter.Name, kind)
		}

	case "install":
		file := fs.Int("file", 0, "GameBanana file ID (default: newest)")
		force := fs.Bool("force", false, "install even if GameBanana's virus scan hasn't passed it")
		accept := fs.Bool("accept-risk", false, "install even if wbmm's safety check found CRITICAL things")
		if err := fs.Parse(reorder(args)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: wbmm install gb:ID [-file N]")
		}
		if _, ok := sources.ParseWSRef(fs.Arg(0)); ok {
			return errors.New("Workshop mods install through Steam: open the item's page and click Subscribe")
		}
		id, ok := sources.ParseGBRef(fs.Arg(0))
		if !ok {
			return fmt.Errorf("%q is not a GameBanana mod ID or link", fs.Arg(0))
		}
		names, err := m.InstallGB(id, *file, *force, *accept)
		if err != nil {
			var nc *manager.NotCleanError
			if errors.As(err, &nc) {
				return fmt.Errorf("%w; add -force to install it anyway", err)
			}
			var re *manager.RiskError
			if errors.As(err, &re) {
				for _, f := range re.Findings {
					if f.Severity >= scan.High {
						fmt.Printf("%-8s %-20s %s  %s\n", f.Severity, f.Rule, f.File, f.Detail)
					}
				}
				return fmt.Errorf("%w\nRead the findings above; add -accept-risk to install it anyway", err)
			}
			return err
		}
		fmt.Println("Installed:", strings.Join(names, ", "))

	case "subscribe", "unsubscribe":
		if len(args) != 1 {
			return fmt.Errorf("usage: wbmm %s ws:ID", cmd)
		}
		id, ok := sources.ParseWSRef(args[0])
		if !ok {
			id, ok = sources.ParseWSRef("ws:" + args[0])
		}
		if !ok {
			return fmt.Errorf("%q is not a Workshop ID or link", args[0])
		}
		if cmd == "unsubscribe" {
			if err := m.Unsubscribe(id); err != nil {
				return err
			}
			fmt.Println("Unsubscribed; Steam removes the files.")
			return nil
		}
		last := ""
		v, err := m.Subscribe(id, func(p steam.Progress) {
			line := "waiting for Steam…"
			if p.Phase == "downloading" && p.Total > 0 {
				line = fmt.Sprintf("downloading %d%%", p.Done*100/p.Total)
			}
			if line != last {
				fmt.Println(line)
				last = line
			}
		})
		if err != nil {
			return err
		}
		fmt.Println("Subscribed and downloaded.")
		if v != nil && v.Blocked() {
			for _, f := range v.Findings {
				if f.Severity >= scan.High {
					fmt.Printf("%-8s %-20s %s  %s\n", f.Severity, f.Rule, f.File, f.Detail)
				}
			}
			fmt.Println("The safety check found CRITICAL things above and switched the mod off. Unsubscribe, or run: wbmm accept " + id)
		}

	case "scan":
		asJSON := fs.Bool("json", false, "print the report as JSON")
		if err := fs.Parse(reorder(args)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: wbmm scan PATH [-json]")
		}
		rep, err := scanPath(m, fs.Arg(0))
		if err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(rep)
		}
		if len(rep.Findings) == 0 {
			fmt.Printf("%d files, %d scripts: nothing found.\n", rep.Files, rep.Scripts)
			return nil
		}
		for _, f := range rep.Findings {
			loc := f.File
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			fmt.Printf("%-8s %-22s %s  %s\n", f.Severity, f.Rule, loc, f.Detail)
		}

	case "pack":
		if len(args) < 1 {
			return errors.New("usage: wbmm pack save NAME [FILE] | wbmm pack apply FILE|CODE [-only]")
		}
		switch args[0] {
		case "save":
			if len(args) < 2 {
				return errors.New("usage: wbmm pack save NAME [FILE]")
			}
			pk, err := m.ExportPack(args[1])
			if err != nil {
				return err
			}
			path := ""
			if len(args) > 2 {
				path = args[2]
				err = manager.WritePackFile(path, pk)
			} else {
				path, err = manager.SavePack(pk)
			}
			if err != nil {
				return err
			}
			fmt.Printf("%s (%d mods) saved to %s\n\n%s\n", pk.Name, len(pk.Entries), path, pk.Encode())
		case "apply":
			only := len(args) > 2 && strings.TrimLeft(args[2], "-") == "only"
			if len(args) < 2 {
				return errors.New("usage: wbmm pack apply FILE|CODE [-only]")
			}
			var pk modpack.Pack
			var err error
			if strings.Contains(args[1], modpack.Prefix) {
				pk, err = modpack.Decode(args[1])
			} else {
				pk, err = manager.ReadPackFile(args[1])
			}
			if err != nil {
				return err
			}
			steps, err := m.PlanPack(pk, only)
			if err != nil {
				return err
			}
			errs := m.ApplyPack(steps, func(s string) { fmt.Println(s) })
			for _, e := range errs {
				fmt.Println("  not done:", e)
			}
			fmt.Printf("Applied %s.\n", pk.Name)
		default:
			return fmt.Errorf("unknown pack command %q", args[0])
		}

	case "self-update":
		rel, err := selfupdate.Latest()
		if err != nil {
			return err
		}
		if !rel.Newer {
			fmt.Printf("wbmm %s is up to date (newest: %s).\n", version, rel.Version)
			return nil
		}
		return selfupdate.Apply(rel, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })

	case "install-app":
		exe, err := desktop.Install(desktop.Options{DesktopShortcut: !(len(args) > 0 && args[0] == "no-desktop"), Version: version},
			func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
		if err != nil {
			return err
		}
		fmt.Println("Installed:", exe)

	case "uninstall-app":
		return desktop.Uninstall(func(f string, a ...any) { fmt.Printf(f+"\n", a...) })

	case "check":
		held, err := m.CheckAll()
		mods, _ := m.List()
		for _, md := range mods {
			if v, ok := m.Verdict(md.Folder); ok && v.Max >= scan.High {
				state := "worth a look"
				if v.Blocked() {
					state = "HELD (switched off)"
				} else if v.Accepted {
					state = "allowed by you"
				}
				fmt.Printf("%s: %s, worst finding %s\n", md.Name, state, v.Max)
			}
		}
		fmt.Printf("Checked %d mods; %d newly held.\n", len(mods), len(held))
		return err

	case "accept":
		if len(args) != 1 {
			return errors.New("usage: wbmm accept UID|ws:ID")
		}
		mods, err := m.List()
		if err != nil {
			return err
		}
		ref := strings.TrimPrefix(args[0], "ws:")
		for _, md := range mods {
			if md.UID == nml.Symbol(ref) || md.WorkshopID == ref {
				return m.Accept(md)
			}
		}
		return fmt.Errorf("no installed mod %q", args[0])

	case "remove", "enable", "disable":
		if len(args) != 1 {
			return fmt.Errorf("usage: wbmm %s UID (see wbmm list)", cmd)
		}
		uid := nml.Symbol(args[0]) // "author.name" or the UID itself
		switch cmd {
		case "remove":
			return m.Remove(uid)
		case "enable":
			return m.SetEnabled(uid, true)
		default:
			return m.SetEnabled(uid, false)
		}

	case "updates":
		yes := fs.Bool("yes", false, "install the updates")
		if err := fs.Parse(args); err != nil {
			return err
		}
		ups, err := m.Updates()
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
		if len(ups) == 0 {
			fmt.Println("GameBanana mods are up to date. (Steam updates Workshop mods itself.)")
			return nil
		}
		for _, u := range ups {
			fmt.Printf("%s: %s (%s)\n", u.Name, u.File.Name, u.File.AddedTime().Format(time.DateOnly))
			if *yes {
				if _, err := m.InstallGB(u.GBID, u.File.ID, false, false); err != nil {
					fmt.Println("  not updated:", err)
				} else {
					fmt.Println("  updated")
				}
			}
		}
		if !*yes {
			fmt.Println("\nRun wbmm updates -yes to install them.")
		}

	case "nml":
		if len(args) > 0 && args[0] == "install" {
			name, err := m.InstallLoader()
			if err != nil {
				return err
			}
			fmt.Printf("NeoModLoader %s installed. Turn on Experimental Mode in WorldBox's settings.\n", name)
			return nil
		}
		st, err := m.LoaderStatus()
		if err != nil {
			return err
		}
		switch {
		case !st.Installed:
			fmt.Println("NeoModLoader is not installed. Run: wbmm nml install")
		case st.CheckError != "":
			fmt.Println("NeoModLoader is installed (couldn't check for updates:", st.CheckError+")")
		case !st.UpToDate:
			fmt.Printf("NeoModLoader is installed; %s is out. Run: wbmm nml install\n", st.Latest)
		default:
			fmt.Printf("NeoModLoader is up to date (%s).\n", st.Latest)
		}

	default:
		fmt.Fprintf(os.Stderr, usage, version)
		return fmt.Errorf("unknown command %q", cmd)
	}
	return nil
}

// scanPath scans a mod folder, or an archive unpacked to a temporary folder.
func scanPath(m *manager.Manager, p string) (*scan.Report, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	opt := m.ScanOptions()
	if st.IsDir() {
		return scan.Mod(p, opt)
	}
	tmp, err := os.MkdirTemp("", "wbmm-scan-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := archive.Extract(p, tmp); err != nil {
		return nil, err
	}
	return scan.Mod(tmp, opt)
}

// Flags that take no value.
var boolFlags = map[string]bool{"force": true, "accept-risk": true, "json": true, "yes": true, "workshop": true}

// reorder puts flags before the mod reference, so "install gb:1 -file 2"
// works as well as "install -file 2 gb:1".
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && !boolFlags[strings.TrimLeft(args[i], "-")] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}
