package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/nml"
)

// myModsPage is the installed mods: one list with a switch per mod, and
// the selected mod's details beside it.
type myModsPage struct {
	u       *ui
	root    fyne.CanvasObject
	filter  *widget.Entry
	list    *fyne.Container
	panel   *fyne.Container
	mods    []manager.Mod
	updates map[string]manager.Update
	err     error
	lastRun *nml.GameLog // what NeoModLoader said at the last game start

	selected string // mod folder
}

func newMyModsPage(u *ui) *myModsPage {
	v := &myModsPage{u: u, updates: map[string]manager.Update{}}
	v.filter = widget.NewEntry()
	v.filter.SetPlaceHolder("Filter")
	v.filter.OnChanged = func(string) { v.paint() }
	check := widget.NewButtonWithIcon("Check for updates", theme.ViewRefreshIcon(), func() { v.checkUpdates(true) })
	folder := widget.NewButtonWithIcon("Mods folder", theme.FolderOpenIcon(), func() {
		if gd, err := u.m.GameDir(); err == nil {
			if err := openFolder(game.ModsDir(gd)); err != nil {
				u.fail(err)
			}
		}
	})
	v.list = container.NewVBox()
	v.panel = container.NewStack(placeholder("Pick a mod to see it here."))
	left := container.NewBorder(v.filter, mutedLabel("Switches apply the next time WorldBox starts."), nil, nil,
		container.NewVScroll(v.list))
	split := container.NewHSplit(left, v.panel)
	split.Offset = 0.56
	v.root = container.NewBorder(pageHeader("My mods", check, folder), nil, nil, nil, split)
	return v
}

// reload re-reads the installed mods.
func (v *myModsPage) reload() {
	go func() {
		mods, err := v.u.m.List()
		sort.SliceStable(mods, func(i, j int) bool { return strings.ToLower(mods[i].Name) < strings.ToLower(mods[j].Name) })
		lastRun, _ := v.u.m.LastRun()
		fyne.Do(func() {
			v.mods, v.err, v.lastRun = mods, err, lastRun
			if len(mods) > 0 {
				v.u.nav[pageMine].setCount(fmt.Sprint(len(mods)))
			} else {
				v.u.nav[pageMine].setCount("")
			}
			v.paint()
			v.u.idle()
		})
	}()
}

func (v *myModsPage) checkUpdates(loud bool) {
	v.u.say("Checking GameBanana for updates…")
	go func() {
		ups, err := v.u.m.Updates()
		fyne.Do(func() {
			v.updates = map[string]manager.Update{}
			for _, up := range ups {
				v.updates[up.UID] = up
			}
			switch {
			case err != nil:
				v.u.fail(fmt.Errorf("some mods couldn't be checked: %w", err))
			case len(ups) > 0:
				v.u.say("%d update(s) available.", len(ups))
			case loud:
				v.u.say("Everything from GameBanana is up to date. Steam keeps Workshop mods updated.")
			}
			v.paint()
		})
	}()
}

func (v *myModsPage) paint() {
	v.list.Objects = nil
	switch {
	case v.err != nil:
		v.list.Add(label(v.err.Error()))
	case len(v.mods) == 0:
		v.list.Add(container.NewVBox(label("No mods yet."),
			widget.NewButtonWithIcon("Get mods", theme.DownloadIcon(), func() { v.u.show(pageGet) })))
	default:
		f := strings.ToLower(strings.TrimSpace(v.filter.Text))
		for _, md := range v.mods {
			if f == "" || strings.Contains(strings.ToLower(md.Name+" "+md.Author), f) {
				v.list.Add(v.row(md))
			}
		}
	}
	v.list.Refresh()
	v.paintPanel()
}

func (v *myModsPage) icon(md manager.Mod, size float32) fyne.CanvasObject {
	p := pal()
	if md.Icon != "" {
		img := localImage(md.Icon, int(size*2))
		img.CornerRadius = 8
		return fixedSize(img, size, size)
	}
	initial := "?"
	if md.Name != "" {
		initial = strings.ToUpper(md.Name[:1])
	}
	bg := canvas.NewRectangle(p.surface2)
	bg.CornerRadius = 8
	return fixedSize(container.NewStack(bg, container.NewCenter(text(initial, p.muted, sTitle, true))), size, size)
}

func sourceName(md manager.Mod) string {
	switch md.Source {
	case "gamebanana":
		return "GameBanana"
	case "workshop":
		return "Workshop"
	case "native":
		return "game's mods folder"
	}
	return "added by hand"
}

func (v *myModsPage) row(md manager.Mod) fyne.CanvasObject {
	u, p := v.u, pal()
	sub := []string{}
	if md.Author != "" {
		sub = append(sub, md.Author)
	}
	sub = append(sub, sourceName(md))
	if md.Version != "" {
		sub = append(sub, md.Version)
	}
	title := line(md.Name, true, theme.SizeNameText, widget.MediumImportance)
	if !md.Enabled {
		title.Importance = widget.LowImportance
	}
	texts := tight(title, line(strings.Join(sub, " · "), false, sSmall, widget.LowImportance))

	badges := container.NewHBox()
	verdict, current := u.m.Verdict(md.Folder)
	run := v.runResult(md)
	switch {
	case current && verdict.Blocked():
		badges.Add(badge("held", p.bad))
	case run.failed:
		badges.Add(badge("didn't load", p.bad))
	case md.Problem != "" || len(md.Needs) > 0 || len(md.Clashes) > 0:
		badges.Add(badge("check", p.warn))
	case current && verdict.Max >= 2 && !verdict.Accepted:
		badges.Add(badge("worth a look", p.sand))
	}
	if _, ok := v.updates[md.UID]; ok {
		badges.Add(badge("update", p.sea))
	}

	sw := newToggle(md.Enabled, nil)
	sw.OnChanged = func(on bool) {
		if err := u.m.SetEnabled(md.UID, on); err != nil {
			u.fail(err)
			sw.Set(!on)
			return
		}
		for i := range v.mods {
			if v.mods[i].Folder == md.Folder {
				v.mods[i].Enabled = on
			}
		}
		if on {
			u.say("%s loads the next time WorldBox starts.", md.Name)
		} else {
			u.say("%s is off from the next start.", md.Name)
		}
		v.paint()
	}
	sw.Disabled = md.UID == "" || md.Problem != "" || u.running || (current && verdict.Blocked())

	right := container.NewHBox(container.NewCenter(badges), container.NewCenter(sw))
	content := container.NewBorder(nil, nil, v.icon(md, 38), right, container.NewVBox(layout.NewSpacer(), texts, layout.NewSpacer()))
	return listRow(content, md.Folder == v.selected, func() {
		v.selected = md.Folder
		v.paint()
	})
}

// paintPanel shows the selected mod.
func (v *myModsPage) paintPanel() {
	var md *manager.Mod
	for i := range v.mods {
		if v.mods[i].Folder == v.selected {
			md = &v.mods[i]
		}
	}
	if md == nil {
		v.panel.Objects = []fyne.CanvasObject{placeholder("Pick a mod to see it here.")}
		v.panel.Refresh()
		return
	}
	u, p := v.u, pal()
	head := container.NewBorder(nil, nil, v.icon(*md, 64), nil, container.NewVBox(layout.NewSpacer(),
		tight(line(md.Name, true, sTitle, widget.MediumImportance),
			line(strings.TrimSpace(md.Version+"  ·  "+sourceName(*md)), false, sSmall, widget.LowImportance),
			line(func() string {
				if md.Author != "" {
					return "by " + md.Author
				}
				return ""
			}(), false, sSmall, widget.LowImportance)),
		layout.NewSpacer()))

	status := container.NewVBox()
	note := func(s string, imp widget.Importance) {
		l := label(s)
		l.Importance = imp
		status.Add(l)
	}
	if md.Problem != "" {
		note(md.Problem, widget.WarningImportance)
	}
	if len(md.Needs) > 0 {
		note("Needs "+strings.Join(md.Needs, ", ")+", which isn't installed. NeoModLoader won't load it without.", widget.WarningImportance)
		needs, name := md.Needs, md.Name
		get := widget.NewButtonWithIcon("Install what it needs", theme.DownloadIcon(), func() { u.installDeps(needs, name) })
		get.Importance = widget.HighImportance
		status.Add(container.NewHBox(get))
	}
	if len(md.Clashes) > 0 {
		note("Doesn't work together with "+strings.Join(md.Clashes, ", ")+". Switch one of them off.", widget.WarningImportance)
	}
	if run := v.runResult(*md); !run.loaded && u.outdated(md.FileAdded) {
		note(u.outdatedNote(), widget.WarningImportance)
	}
	verdict, current := u.m.Verdict(md.Folder)
	safety := container.NewHBox()
	switch {
	case verdict == nil || !current:
		safety.Add(badge("checking…", p.muted))
	case verdict.Blocked():
		b := widget.NewButton("Review", func() { u.review(*md, verdict) })
		b.Importance = widget.DangerImportance
		safety.Add(badge("held: switched off", p.bad))
		safety.Add(b)
	case verdict.Accepted:
		safety.Add(badge("allowed by you", p.sand))
		safety.Add(widget.NewButton("Findings", func() { u.showFindings(md.Name, verdict.Findings) }))
	case verdict.Max >= 2:
		safety.Add(badge("worth a look", p.sand))
		safety.Add(widget.NewButton("Findings", func() { u.showFindings(md.Name, verdict.Findings) }))
	default:
		safety.Add(badge("passed", p.grass))
	}

	acts := container.NewHBox()
	if up, ok := v.updates[md.UID]; ok {
		b := widget.NewButtonWithIcon("Update", theme.DownloadIcon(), func() {
			u.installGB(up.GBID, up.File, md.Name, false)
			delete(v.updates, md.UID)
		})
		b.Importance = widget.HighImportance
		u.bindOp(u.opKey("gamebanana", fmt.Sprint(up.GBID)), b)
		acts.Add(b)
	}
	if c, ok := cardFromMod(*md); ok {
		acts.Add(widget.NewButtonWithIcon("Details", theme.InfoIcon(), func() {
			u.show(pageGet)
			u.get.open(c)
		}))
	}
	if md.Problem == "" {
		folder := md.Folder
		acts.Add(widget.NewButtonWithIcon("Folder", theme.FolderOpenIcon(), func() {
			if err := openFolder(folder); err != nil {
				u.fail(err)
			}
		}))
	}
	if md.Source == "workshop" {
		b := widget.NewButtonWithIcon("Unsubscribe", theme.DeleteIcon(), func() { u.unsubscribe(md.WorkshopID, md.Name) })
		u.bindOp(u.opKey("workshop", md.WorkshopID), b)
		acts.Add(b)
	} else if md.UID != "" && md.Problem == "" {
		mod := *md
		acts.Add(widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() { u.remove(mod) }))
	}

	desc := md.Description
	if strings.TrimSpace(desc) == "" {
		desc = "No description in its mod.json."
	}
	run := v.runResult(*md)
	runBox := container.NewVBox()
	switch {
	case !run.known:
		runBox.Add(mutedLabel("No game start since it was installed or changed."))
	case run.failed:
		for _, pr := range run.problems {
			l := label(pr.Summary)
			l.Importance = widget.DangerImportance
			runBox.Add(l)
			if len(pr.Details) > 0 {
				d := widget.NewLabel(strings.Join(pr.Details, "\n"))
				d.Wrapping = fyne.TextWrapBreak
				d.TextStyle.Monospace = true
				d.SizeName = sSmall
				runBox.Add(tint(d, withAlpha(p.text, 0x0a)))
			}
		}
	case run.loaded:
		runBox.Add(badge("loaded", p.grass))
	case md.Enabled:
		runBox.Add(mutedLabel("NeoModLoader didn't mention it: it may not have loaded."))
	default:
		runBox.Add(mutedLabel("Switched off at that start."))
	}
	runTitle := "Last game start"
	if v.lastRun != nil {
		runTitle += " (" + v.lastRun.Time.Format("Jan 2, 15:04") + ")"
	}
	body := container.NewVBox(head, acts, status,
		section2(runTitle, runBox),
		section2("Safety check", safety),
		section2("About", label(desc)),
		section2("Folder", mutedLabel(md.Folder)))
	v.panel.Objects = []fyne.CanvasObject{container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 12, 12, 8), body))}
	v.panel.Refresh()
}

// cardFromMod lets an installed GameBanana or Workshop mod open its page.
func cardFromMod(md manager.Mod) (modCard, bool) {
	switch md.Source {
	case "gamebanana":
		return modCard{Source: "gamebanana", Kind: "mod", ID: fmt.Sprint(md.GBID), Name: md.Name, Author: md.Author, URL: md.URL}, true
	case "workshop":
		return modCard{Source: "workshop", Kind: "mod", ID: md.WorkshopID, Name: md.Name, Author: md.Author, URL: md.URL}, true
	}
	return modCard{}, false
}

// runInfo is what the last game start said about one mod.
type runInfo struct {
	known, loaded, failed bool
	problems              []nml.LogProblem
}

// runResult reads the last game start for md, unless the mod changed since
// (the log is about older files then).
func (v *myModsPage) runResult(md manager.Mod) runInfo {
	gl := v.lastRun
	if gl == nil || !gl.NML {
		return runInfo{}
	}
	if fi, err := os.Stat(filepath.Join(md.Folder, nml.DeclFile)); err == nil && fi.ModTime().After(gl.Time) {
		return runInfo{}
	}
	r := runInfo{known: true, loaded: gl.Loaded[md.Name], problems: gl.ProblemsFor(md.UID, md.Name)}
	for _, pr := range r.problems {
		if pr.Kind != "outdated" {
			r.failed = true
		}
	}
	return r
}
