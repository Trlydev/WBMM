// Package gui is wbmm's window: a native Fyne app laid out like a mod
// manager. A sidebar (Play, My mods, Get mods, Worlds, Settings), a list in
// the middle, the selected item's details on the right, and a status bar
// for downloads and messages.
package gui

import (
	"errors"
	"fmt"
	"image/color"
	"log"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/desktop"
	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/popularity"
	"github.com/Trlydev/WBMM/internal/selfupdate"
)

const appID = "io.github.trlydev.wbmm"

type ui struct {
	a       fyne.App
	w       fyne.Window
	m       *manager.Manager
	version string

	nav     []*navItem
	pages   []fyne.CanvasObject
	current int
	notices *fyne.Container
	nmlLine *widget.Label
	runLine *widget.Label

	actText  *widget.Label
	actBar   *widget.ProgressBar
	actUntil time.Time

	// What the window knows about the game and NML.
	gameDir     string
	gameErr     error
	running     bool
	loader      *nml.LoaderStatus
	restartTo   string // a self-update is installed; restart to use it
	hideInstall bool

	ops       map[string]*op
	opButtons map[string][]*widget.Button
	prechecks map[int]*precheck // by GameBanana file ID
	release   *popularity.Game  // the current WorldBox version, once known

	mine   *myModsPage
	get    *getModsPage
	worlds *worldsPage
	packs  *modpacksPage
	set    *settingsPage
}

// Pages, in sidebar order.
const (
	pageMine = iota
	pageGet
	pageWorlds
	pageModpacks
	pageSettings
)

// Run opens the window and returns when it's closed. openPack is a .wbm
// file to show (wbmm started by opening one), or "".
func Run(m *manager.Manager, version, openPack string) error {
	if forwardToRunning(openPack) {
		return nil
	}
	selfupdate.Current = version
	selfupdate.Cleanup()
	if d, err := manager.ConfigDir(); err == nil {
		popularity.CacheDir = d
	}
	a := app.NewWithID(appID)
	a.Settings().SetTheme(wbTheme{})
	a.SetIcon(fyne.NewStaticResource("wbmm.png", desktop.IconPNG()))
	u := &ui{a: a, m: m, version: version}
	u.w = a.NewWindow("WorldBox Mod Manager")
	u.w.Resize(fyne.NewSize(1240, 800))
	u.build()
	if openPack != "" {
		u.packs.openPath(openPack)
	}
	stop := listenForOthers(func(pack string) {
		fyne.Do(func() {
			u.w.Show()
			u.w.RequestFocus()
			if pack != "" {
				u.packs.openPath(pack)
			}
		})
	})
	defer stop()
	go u.background()
	u.w.ShowAndRun()
	return nil
}

func (u *ui) build() {
	u.mine = newMyModsPage(u)
	u.get = newGetModsPage(u)
	u.worlds = newWorldsPage(u)
	u.packs = newModpacksPage(u)
	u.set = newSettingsPage(u)
	u.pages = []fyne.CanvasObject{u.mine.root, u.get.root, u.worlds.root, u.packs.root, u.set.root}

	u.notices = container.NewVBox()
	u.actText = widget.NewLabel("")
	u.actText.Truncation = fyne.TextTruncateEllipsis
	u.actBar = widget.NewProgressBar()
	u.actBar.Hide()
	statusBg := canvas.NewRectangle(pal().surface)
	status := container.NewStack(statusBg, container.New(layout.NewCustomPaddedLayout(0, 0, 12, 12),
		container.NewBorder(nil, nil, nil, fixedSize(u.actBar, 220, 20), u.actText)))
	statusLine := canvas.NewRectangle(pal().border)
	statusLine.SetMinSize(fyne.NewSize(1, 1))

	content := container.NewBorder(u.notices, nil, nil, nil, container.NewStack(u.pages...))
	main := container.NewBorder(nil, container.NewVBox(statusLine, status), u.sidebar(), nil,
		container.New(layout.NewCustomPaddedLayout(12, 4, 16, 16), content))
	u.w.SetContent(main)
	u.show(pageMine)
	u.say("Loading your mods…")
}

// sidebar: the name, Play, the pages, and the game's status at the bottom.
func (u *ui) sidebar() fyne.CanvasObject {
	p := pal()
	icon := canvas.NewImageFromResource(fyne.NewStaticResource("wbmm.png", desktop.IconPNG()))
	icon.FillMode = canvas.ImageFillContain
	icon.ScaleMode = canvas.ImageScalePixels // pixel art: keep the pixels sharp
	brand := container.NewHBox(fixedSize(icon, 32, 32),
		container.NewVBox(layout.NewSpacer(), text("WorldBox", p.text, sTitle, true), text("Mod Manager", p.muted, sSmall, false), layout.NewSpacer()))

	play := widget.NewButtonWithIcon("Play WorldBox", theme.MediaPlayIcon(), u.play)
	play.Importance = widget.HighImportance

	items := []struct {
		icon fyne.Resource
		name string
	}{
		{theme.ListIcon(), "My mods"},
		{theme.DownloadIcon(), "Get mods"},
		{theme.HomeIcon(), "Worlds"},
		{theme.StorageIcon(), "Modpacks"},
		{theme.SettingsIcon(), "Settings"},
	}
	navBox := container.NewVBox()
	for i, it := range items {
		i := i
		n := newNavItem(it.icon, it.name, func() { u.show(i) })
		u.nav = append(u.nav, n)
		navBox.Add(n)
	}

	u.nmlLine = line("NeoModLoader …", false, sSmall, widget.LowImportance)
	u.runLine = line("WorldBox is running", true, sSmall, widget.WarningImportance)
	u.runLine.Hide()
	foot := container.NewVBox(widget.NewSeparator(), u.runLine, u.nmlLine,
		line("wbmm "+u.version, false, sSmall, widget.LowImportance))

	bg := canvas.NewRectangle(p.surface)
	edge := canvas.NewRectangle(p.border)
	edge.SetMinSize(fyne.NewSize(1, 1))
	col := container.NewBorder(
		container.NewVBox(brand, layout.NewSpacer(), play, widget.NewSeparator()),
		foot, nil, nil, navBox)
	return container.NewBorder(nil, nil, nil, edge,
		container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(14, 10, 12, 12), fixedWidth(col, 196))))
}

// fixedWidth gives an object a fixed width and lets its height stretch.
func fixedWidth(o fyne.CanvasObject, w float32) fyne.CanvasObject {
	return container.New(&fixedWidthLayout{w}, o)
}

type fixedWidthLayout struct{ w float32 }

func (l *fixedWidthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(l.w, objs[0].MinSize().Height)
}
func (l *fixedWidthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Resize(fyne.NewSize(l.w, size.Height))
	objs[0].Move(fyne.NewPos(0, 0))
}

func (u *ui) show(i int) {
	u.current = i
	for j, pg := range u.pages {
		if j == i {
			pg.Show()
		} else {
			pg.Hide()
		}
	}
	for j, n := range u.nav {
		n.setActive(j == i)
	}
	switch i {
	case pageMine:
		u.mine.reload()
	case pageWorlds:
		u.worlds.reload()
	case pageModpacks:
		u.packs.reload()
	case pageSettings:
		u.set.refresh()
	}
}

// refreshViews redraws everything that shows mods (after an install, a
// subscription, a removal).
func (u *ui) refreshViews() {
	u.get.repaint()
	u.mine.reload()
	u.worlds.reload()
}

// ---------- status bar ----------

// say shows a message in the status bar.
func (u *ui) say(format string, a ...any) {
	u.actText.Importance = widget.MediumImportance
	u.actText.SetText(fmt.Sprintf(format, a...))
	u.actUntil = time.Now().Add(8 * time.Second)
}

// fail shows an error in the status bar and logs it.
func (u *ui) fail(err error) {
	log.Print(err)
	u.actText.Importance = widget.DangerImportance
	u.actText.SetText(err.Error())
	u.actUntil = time.Now().Add(15 * time.Second)
}

// progress shows (or with frac < 0, hides) the status bar's progress.
func (u *ui) progress(frac float64) {
	if frac < 0 {
		u.actBar.Hide()
		return
	}
	u.actBar.SetValue(frac)
	u.actBar.Show()
}

// idle is the status bar when nothing is happening.
func (u *ui) idle() {
	if time.Now().Before(u.actUntil) || u.actBar.Visible() {
		return
	}
	u.actText.Importance = widget.LowImportance
	n := len(u.mine.mods)
	switch {
	case u.gameErr != nil:
		u.actText.SetText("WorldBox not found. Set its folder in Settings.")
	case n == 1:
		u.actText.SetText("Ready · 1 mod")
	default:
		u.actText.SetText(fmt.Sprintf("Ready · %d mods", n))
	}
}

// ---------- status, notices ----------

// refreshStatus re-reads the game folder, whether the game runs and (with
// loader) NeoModLoader's status, then redraws what depends on them.
func (u *ui) refreshStatus(loader bool) {
	gd, gerr := u.m.GameDir()
	running := game.Running()
	var ls *nml.LoaderStatus
	if loader && gerr == nil {
		st, err := u.m.LoaderStatus()
		if err == nil {
			ls = &st
		}
	}
	fyne.Do(func() {
		changed := running != u.running
		u.gameDir, u.gameErr, u.running = gd, gerr, running
		if ls != nil {
			u.loader = ls
		}
		u.paintStatus()
		if changed {
			u.mine.paint()
		}
	})
}

func (u *ui) paintStatus() {
	if u.running {
		u.runLine.Show()
	} else {
		u.runLine.Hide()
	}
	switch l := u.loader; {
	case l == nil:
		u.nmlLine.SetText("NeoModLoader …")
		u.nmlLine.Importance = widget.LowImportance
	case !l.Installed:
		u.nmlLine.SetText("NeoModLoader missing")
		u.nmlLine.Importance = widget.DangerImportance
	case !l.UpToDate && l.CheckError == "":
		u.nmlLine.SetText("NeoModLoader update")
		u.nmlLine.Importance = widget.WarningImportance
	default:
		u.nmlLine.SetText("NeoModLoader ready")
		u.nmlLine.Importance = widget.SuccessImportance
	}
	u.nmlLine.Refresh()
	u.paintNotices()
	u.set.refresh()
}

// paintNotices shows the few things that need the person, slim, above the
// page.
func (u *ui) paintNotices() {
	p := pal()
	var bs []fyne.CanvasObject
	add := func(msg string, c color.NRGBA, buttons ...fyne.CanvasObject) {
		l := widget.NewLabel(msg)
		l.Wrapping = fyne.TextWrapWord
		row := container.NewBorder(nil, nil, nil, container.NewHBox(buttons...), l)
		bs = append(bs, container.New(layout.NewCustomPaddedLayout(0, 8, 0, 0), tint(row, withAlpha(c, 0x2c))))
	}
	if u.restartTo != "" {
		add(fmt.Sprintf("wbmm %s is installed. Restart to use it.", u.restartTo), p.grass,
			widget.NewButton("Restart", u.restart))
	}
	if u.gameErr != nil {
		add(u.gameErr.Error(), p.warn, widget.NewButton("Settings", func() { u.show(pageSettings) }))
	} else if u.loader != nil && !u.loader.Installed {
		add("NeoModLoader isn't installed. WorldBox mods need it to load.", p.warn,
			widget.NewButton("Install it", u.installLoader))
	}
	if !u.hideInstall && !desktop.IsInstalled() && u.version != "dev" {
		install := widget.NewButton("Install", u.installApp)
		install.Importance = widget.HighImportance
		add("Install WorldBox Mod Manager as an app: a menu entry, a shortcut, and updates in place.", p.sea,
			install, widget.NewButton("Not now", func() { u.hideInstall = true; u.paintNotices() }))
	}
	u.notices.Objects = bs
	u.notices.Refresh()
}

// play starts WorldBox through Steam, after a look at what would stop
// mods loading.
func (u *ui) play() {
	if u.running {
		u.say("WorldBox is already running.")
		return
	}
	start := func() {
		u.openURL("steam://rungameid/" + game.AppID)
		u.say("Starting WorldBox through Steam…")
	}
	if u.loader != nil && !u.loader.Installed {
		d := dialog.NewConfirm("NeoModLoader isn't installed", "Without it no mods load. Install it first?", func(install bool) {
			if install {
				u.installLoader()
				return
			}
			start()
		}, u.w)
		d.SetConfirmText("Install NeoModLoader")
		d.SetDismissText("Play without mods")
		d.Show()
		return
	}
	// A last safety check: Steam may have updated a Workshop mod since the
	// last one, and the game loads mods as it starts.
	u.say("Checking your mods before the game starts…")
	go func() {
		held, err := u.m.CheckAll()
		if err != nil {
			log.Printf("safety check: %v", err)
		}
		fyne.Do(func() {
			u.mine.paint()
			if len(held) > 0 {
				u.fail(errors.New(heldMessage(held)))
				u.openURL("steam://rungameid/" + game.AppID)
				return
			}
			start()
		})
	}()
}

func (u *ui) installLoader() {
	u.say("Installing NeoModLoader…")
	go func() {
		name, err := u.m.InstallLoader()
		fyne.Do(func() {
			if err != nil {
				u.fail(err)
				return
			}
			u.say("NeoModLoader %s installed. Turn on Experimental Mode in WorldBox's settings the first time.", name)
		})
		u.refreshStatus(true)
	}()
}

func (u *ui) installApp() {
	go func() {
		exe, err := desktop.Install(desktop.Options{DesktopShortcut: true, Version: u.version}, func(f string, a ...any) { log.Printf(f, a...) })
		fyne.Do(func() {
			if err != nil {
				u.fail(fmt.Errorf("installing the app: %w", err))
				return
			}
			u.hideInstall = true
			u.paintNotices()
			u.say("Installed. Starting the installed copy…")
		})
		if err == nil {
			time.Sleep(700 * time.Millisecond)
			stopListening()
			if desktop.Launch(exe, "gui") == nil {
				fyne.Do(u.a.Quit)
			}
		}
	}()
}

func (u *ui) restart() {
	stopListening()
	if err := desktop.Launch(desktop.StartedAs(), "gui"); err != nil {
		u.fail(fmt.Errorf("restarting: %w", err))
		return
	}
	u.a.Quit()
}

// ---------- background work ----------

// background keeps the status fresh and runs the automatic updates: wbmm
// itself and GameBanana mods, at start and every six hours.
func (u *ui) background() {
	go pruneImageCache()
	go desktop.Refresh()
	go u.loadGameRelease()
	u.refreshStatus(true)
	fyne.Do(func() { u.mine.reload() })
	time.Sleep(3 * time.Second)
	var lastApp, lastMods, lastCheck time.Time
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		// The safety check: cheap when nothing changed (file sizes and
		// times are compared first), so every minute catches Steam's
		// silent Workshop updates before the next game start.
		if time.Since(lastCheck) > time.Minute {
			lastCheck = time.Now()
			held, err := u.m.CheckAll()
			fyne.Do(func() {
				if len(held) > 0 {
					u.fail(errors.New(heldMessage(held)))
				}
				u.mine.paint()
			})
			if err != nil {
				log.Printf("safety check: %v", err)
			}
		}
		cfg := u.m.Config()
		if !cfg.NoAppUpdates && time.Since(lastApp) > 6*time.Hour {
			lastApp = time.Now()
			u.selfUpdate(false)
		}
		if time.Since(lastMods) > 6*time.Hour && !game.Running() {
			lastMods = time.Now()
			go u.loadGameRelease()
			if name, err := u.m.AutoUpdateLoader(); err != nil {
				log.Printf("NeoModLoader update: %v", err)
			} else if name != "" {
				fyne.Do(func() { u.say("Updated NeoModLoader to %s. Settings can take you back if mods misbehave.", name) })
				u.refreshStatus(true)
			}
			if !cfg.NoModUpdates {
				u.autoUpdateMods()
			}
		}
		<-tick.C
		u.refreshStatus(false)
		fyne.Do(u.idle)
	}
}

// selfUpdate installs a newer wbmm release, if any. force reports "up to
// date" too (the Setup button).
func (u *ui) selfUpdate(force bool) {
	if !strings.HasPrefix(u.version, "v") {
		if force {
			fyne.Do(func() { u.say("This is a development build; it doesn't update itself.") })
		}
		return
	}
	rel, err := selfupdate.Latest()
	if err != nil {
		if force {
			fyne.Do(func() { u.fail(fmt.Errorf("checking for wbmm updates: %w", err)) })
		}
		return
	}
	if !rel.Newer {
		if force {
			fyne.Do(func() { u.say("wbmm is up to date (%s).", u.version) })
		}
		return
	}
	fyne.Do(func() { u.say("Updating wbmm to %s…", rel.Version) })
	if err := selfupdate.Apply(rel, func(f string, a ...any) { log.Printf(f, a...) }); err != nil {
		fyne.Do(func() { u.fail(fmt.Errorf("updating wbmm: %w", err)) })
		return
	}
	fyne.Do(func() {
		u.restartTo = rel.Version
		u.paintNotices()
		u.say("wbmm %s is installed. Restart to use it.", rel.Version)
	})
}

func (u *ui) autoUpdateMods() {
	updated, held, err := u.m.AutoUpdate()
	fyne.Do(func() {
		switch {
		case len(updated) > 0:
			u.say("Updated %s.", strings.Join(updated, ", "))
			u.mine.reload()
		case len(held) > 0:
			u.say("Updates waiting for you under Installed: %s", strings.Join(held, ", "))
		}
		if err != nil {
			log.Printf("automatic mod updates: %v", err)
		}
	})
}

// ---------- opening things ----------

// reSteamLink matches the only steam:// links wbmm opens: starting
// WorldBox and showing a Workshop item. Links from mods and listings must
// be https (a steam:// link can start other games with arguments).
var reSteamLink = regexp.MustCompile(`^steam://(rungameid/1206560|url/CommunityFilePage/[0-9]{6,20})$`)

func (u *ui) openURL(link string) {
	p, err := url.Parse(link)
	if err != nil || !(p.Scheme == "https" || p.Scheme == "steam" && reSteamLink.MatchString(link)) {
		u.fail(fmt.Errorf("not opening %q: only https web pages", link))
		return
	}
	if err := u.a.OpenURL(p); err != nil {
		u.fail(err)
	}
}

func openFolder(p string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", p).Start()
	case "darwin":
		return exec.Command("open", p).Start()
	}
	return exec.Command("xdg-open", p).Start()
}
