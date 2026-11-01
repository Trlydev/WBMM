package gui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/popularity"
	"github.com/Trlydev/WBMM/internal/scan"
	"github.com/Trlydev/WBMM/internal/sources"
)

// ---------- the safety check on the preview page ----------

// autoCheckSize: files up to this size are checked as soon as their
// preview opens; bigger ones when asked.
const autoCheckSize = 30 << 20

// precheck is the safety check of one GameBanana file, run or running.
type precheck struct {
	running bool
	pc      *manager.Precheck
	err     error
	deps    []manager.Dependency // where to get what it needs
}

// runPrecheck checks a GameBanana file before it's installed; then runs on
// the UI thread when it's done (the caller draws the "checking" state).
func (u *ui) runPrecheck(gbID int, f sources.GBFile, then func()) {
	if u.prechecks == nil {
		u.prechecks = map[int]*precheck{}
	}
	if p, ok := u.prechecks[f.ID]; ok && (p.running || p.err == nil) {
		return
	}
	u.prechecks[f.ID] = &precheck{running: true}
	go func() {
		pc, err := u.m.PrecheckGB(gbID, f.ID)
		var deps []manager.Dependency
		if err == nil && len(pc.Needs) > 0 {
			deps = manager.FindDependencies(pc.Needs)
		}
		fyne.Do(func() {
			u.prechecks[f.ID] = &precheck{pc: pc, err: err, deps: deps}
			then()
		})
	}()
}

// paintCheck shows the safety check of the file Install would install.
func (d *detailsView) paintCheck(gbID int, f sources.GBFile) fyne.CanvasObject {
	u, p := d.u, pal()
	box := container.NewVBox()
	repaint := func() {
		if d.has && d.card.Source == "gamebanana" && atoi(d.card.ID) == gbID {
			d.paintActions()
		}
	}
	pc := u.prechecks[f.ID]
	switch {
	case pc == nil && f.Size > autoCheckSize:
		b := widget.NewButtonWithIcon("Check it first", theme.SearchIcon(), func() { u.runPrecheck(gbID, f, repaint); repaint() })
		box.Add(container.NewHBox(b))
		box.Add(mutedLabel(fmt.Sprintf("Downloads it (%s) and runs the safety check without installing. Installing checks it either way.", size(f.Size))))
	case pc == nil:
		u.runPrecheck(gbID, f, repaint)
		return d.paintCheck(gbID, f)
	case pc.running:
		box.Add(container.NewHBox(badge("checking…", p.muted)))
		box.Add(mutedLabel("Downloading " + f.Name + " and checking it, without installing anything."))
	case pc.err != nil:
		box.Add(container.NewHBox(badge("couldn't check", p.warn),
			widget.NewButton("Try again", func() { delete(u.prechecks, f.ID); u.runPrecheck(gbID, f, repaint); repaint() })))
		box.Add(mutedLabel(pc.err.Error()))
	default:
		v := pc.pc.Verdict
		row := container.NewHBox()
		switch {
		case v.Max >= scan.Critical:
			row.Add(badge("serious findings", p.bad))
		case v.Max >= scan.High:
			row.Add(badge("worth a look", p.sand))
		default:
			row.Add(badge("passed", p.grass))
		}
		if v.Max >= scan.High {
			name := d.card.Name
			row.Add(widget.NewButton("Findings", func() { u.showFindings(name, v.Findings) }))
		}
		box.Add(row)
		switch {
		case v.Max >= scan.Critical:
			l := label("Its code does things malware does. Installing asks you first.")
			l.Importance = widget.DangerImportance
			box.Add(l)
		case v.Max >= scan.High:
			box.Add(mutedLabel("Nothing malware-like, but it can do things worth knowing about (see Findings)."))
		default:
			box.Add(mutedLabel("Nothing worrying in " + f.Name + "."))
		}
		if pc.pc.Problem != "" {
			l := label("It won't install as a mod: " + pc.pc.Problem + ".")
			l.Importance = widget.WarningImportance
			l.Wrapping = fyne.TextWrapWord
			box.Add(l)
		}
		var names []string
		for _, m := range pc.pc.Mods {
			names = append(names, m.Name)
		}
		if len(names) > 1 {
			box.Add(mutedLabel("Contains " + strings.Join(names, ", ") + "."))
		}
		if len(pc.deps) > 0 {
			box.Add(mutedLabel(depsSentence(pc.deps)))
		}
	}
	return section2("Safety check", box)
}

// depsSentence says what a mod needs and what wbmm will do about it.
func depsSentence(deps []manager.Dependency) string {
	var known, unknown []string
	for _, d := range deps {
		if d.GBID != 0 {
			known = append(known, d.Name)
		} else {
			unknown = append(unknown, d.Name)
		}
	}
	var s []string
	if len(known) > 0 {
		s = append(s, "Needs "+strings.Join(known, ", ")+": installed with it.")
	}
	if len(unknown) > 0 {
		s = append(s, "Needs "+strings.Join(unknown, ", ")+", which wbmm can't find on GameBanana: get it from the mod's page.")
	}
	return strings.Join(s, " ")
}

// ---------- dependencies ----------

// installDeps installs the mods forName needs, where wbmm knows them, each
// through the safety check like any install.
func (u *ui) installDeps(needs []string, forName string) {
	if len(needs) == 0 {
		return
	}
	go func() {
		deps := manager.FindDependencies(needs)
		fyne.Do(func() {
			var unknown []string
			for _, d := range deps {
				if d.GBID == 0 {
					unknown = append(unknown, d.ID)
					continue
				}
				u.installGBByID(d.GBID, d.Name)
			}
			if len(unknown) > 0 {
				u.fail(fmt.Errorf("%s needs %s, which wbmm can't find on GameBanana: get it from %s's page", forName, strings.Join(unknown, ", "), forName))
			}
		})
	}()
}

// installGBByID installs the newest file of a GameBanana mod.
func (u *ui) installGBByID(id int, name string) {
	key := u.opKey("gamebanana", strconv.Itoa(id))
	if _, busy := u.ops[key]; busy {
		return
	}
	u.setOp(key, "Checking…")
	go func() {
		files, err := sources.GBFiles(id)
		fyne.Do(func() {
			u.endOp(key)
			if err == nil && len(files) == 0 {
				err = errors.New("it has no files")
			}
			if err != nil {
				u.fail(fmt.Errorf("%s: %w", name, err))
				return
			}
			u.installGB(id, files[0], name, false)
		})
	}()
}

// ---------- maps ----------

// installMap copies a GameBanana map into a free save slot.
func (u *ui) installMap(id int, name string, force bool) {
	key := u.opKey("gamebanana", strconv.Itoa(id))
	if _, busy := u.ops[key]; busy {
		return
	}
	u.setOp(key, "Installing…")
	u.say("Downloading %s…", name)
	go func() {
		slots, err := u.m.InstallMap(id, 0, force)
		fyne.Do(func() {
			u.endOp(key)
			var nc *manager.NotCleanError
			if errors.As(err, &nc) {
				d := dialog.NewConfirm("Virus scan not passed", fmt.Sprintf("GameBanana's virus scan hasn't passed %s.\n\nMaps hold no code, and wbmm copies only the save files. Install it anyway?", nc.File.Name), func(ok bool) {
					if ok {
						u.installMap(id, name, true)
					}
				}, u.w)
				d.SetConfirmText("Install anyway")
				d.Show()
				return
			}
			if err != nil {
				u.fail(fmt.Errorf("%s: %w", name, err))
				return
			}
			u.say("Put %s into save slot %s. Load it from WorldBox's Saves.", name, strings.Join(slots, ", "))
		})
	}()
}

func (u *ui) removeMap(id int, name string) {
	dialog.ShowConfirm("Remove "+name+"?", "Its save slot is deleted. If you've saved over it in the game, that save is kept.", func(ok bool) {
		if !ok {
			return
		}
		kept, err := u.m.RemoveMap(id)
		if err != nil {
			u.fail(err)
			return
		}
		if kept > 0 {
			u.say("Removed %s from wbmm; its slot holds a save of yours now, so it stays.", name)
		} else {
			u.say("Removed %s.", name)
		}
		u.refreshViews()
	}, u.w)
}

// ---------- made for an older WorldBox ----------

// loadGameRelease learns the current WorldBox version from the popularity
// index, for the "made for an older WorldBox" notes.
func (u *ui) loadGameRelease() {
	ix, err := popularity.Fetch()
	if err != nil || ix.Game == nil {
		return
	}
	g := *ix.Game
	fyne.Do(func() {
		u.release = &g
		u.get.repaint()
		u.mine.paint()
	})
}

// outdated reports whether a mod last updated at t predates the current
// WorldBox version series.
func (u *ui) outdated(t int64) bool {
	return u.release != nil && t > 0 && t < u.release.Released
}

func (u *ui) outdatedNote() string {
	if u.release == nil {
		return ""
	}
	when := time.Unix(u.release.Released, 0).Format("Jan 2006")
	return fmt.Sprintf("Last updated before WorldBox %s came out (%s): it may not work with your game. Its page's comments usually say.", u.release.Version, when)
}
