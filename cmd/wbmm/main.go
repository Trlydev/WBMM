// Command wbmm is WorldBox Mod Manager: install and manage WorldBox mods
// from GameBanana and the Steam Workshop. Run without arguments it opens
// its window; the commands below do the same things from a terminal.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Trlydev/WBMM/internal/gui"
	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/sources"
)

// version is set at build time (-X main.version=v0.1.N).
var version = "dev"

const usage = `WorldBox Mod Manager (wbmm) %s

Usage:
  wbmm                          open the window
  wbmm list                     installed mods (Mods folder and Workshop)
  wbmm search [-workshop] WORDS search GameBanana (or the Steam Workshop)
  wbmm install gb:ID [-file N]  install a GameBanana mod (ID or link)
  wbmm remove UID               delete a mod from the Mods folder
  wbmm enable UID | disable UID turn a mod on or off (game closed)
  wbmm updates [-yes]           check GameBanana mods for updates
  wbmm nml [install]            NeoModLoader status, or install/update it
  wbmm version

Set WBMM_GAME_DIR to use a WorldBox folder Steam doesn't know about.
`

func main() {
	attachConsole()
	sources.UserAgent = fmt.Sprintf("wbmm/%s (+https://github.com/Trlydev/WBMM)", version)
	m, err := manager.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: couldn't read wbmm's settings:", err)
	}
	args := os.Args[1:]
	if len(args) == 0 {
		setupLog()
		if err := gui.Run(m, version); err != nil {
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
			items, _, err := sources.WSBrowse(q, "subs", 1)
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
		names, err := m.InstallGB(id, *file, *force)
		if err != nil {
			var nc *manager.NotCleanError
			if errors.As(err, &nc) {
				return fmt.Errorf("%w; add -force to install it anyway", err)
			}
			return err
		}
		fmt.Println("Installed:", strings.Join(names, ", "))

	case "remove", "enable", "disable":
		if len(args) != 1 {
			return fmt.Errorf("usage: wbmm %s UID (see wbmm list)", cmd)
		}
		uid := strings.ToUpper(args[0])
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
				if _, err := m.InstallGB(u.GBID, u.File.ID, false); err != nil {
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

// reorder puts flags before the mod reference, so "install gb:1 -file 2"
// works as well as "install -file 2 gb:1".
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && args[i] != "-force" && args[i] != "--force" && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}
