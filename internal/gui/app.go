// Package gui is wbmm's window: a native Fyne app with a header of tabs
// (Browse, Installed, Setup), banners for things that need the person, and
// an activity bar along the bottom for downloads and messages.
package gui

import (
	"fmt"
	"image/color"
	"log"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/desktop"
	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/selfupdate"
)

const appID = "io.github.trlydev.wbmm"

type ui struct {
	a       fyne.App
	w       fyne.Window
	m       *manager.Manager
	version string

	tabBtns  []*widget.Button
	views    []fyne.CanvasObject
	body     *fyne.Container
	current  int
	banners  *fyne.Container
	nmlChip  *fyne.Container
	runChip  fyne.CanvasObject
	countTxt *canvas.Text

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

	br   *browseView
	inst *installedView
	set  *setupView
}

// Run opens the window and returns when it's closed.
func Run(m *manager.Manager, version string) error {
	if forwardToRunning() {
		return nil
	}
	selfupdate.Current = version
	selfupdate.Cleanup()
	a := app.NewWithID(appID)
	a.Settings().SetTheme(wbTheme{})
	a.SetIcon(fyne.NewStaticResource("wbmm.png", desktop.IconPNG()))
	u := &ui{a: a, m: m, version: version}
	u.w = a.NewWindow("WorldBox Mod Manager")
	u.w.Resize(fyne.NewSize(1200, 820))
	u.build()
	stop := listenForOthers(func() {
		fyne.Do(func() {
			u.w.Show()
			u.w.RequestFocus()
		})
	})
	defer stop()
	go u.background()
	u.w.ShowAndRun()
	return nil
}

func (u *ui) build() {
	u.br = newBrowseView(u)
	u.inst = newInstalledView(u)
	u.set = newSetupView(u)
	u.views = []fyne.CanvasObject{u.br.root, u.inst.root, u.set.root}
	u.body = container.NewStack(u.views...)

	u.banners = container.NewVBox()
	u.actText = widget.NewLabel("")
	u.actText.Truncation = fyne.TextTruncateEllipsis
	u.actBar = widget.NewProgressBar()
	u.actBar.Hide()
	actBg := canvas.NewRectangle(pal().surface)
	activity := container.NewStack(actBg, container.New(layout.NewCustomPaddedLayout(2, 2, 14, 14),
		container.NewBorder(nil, nil, nil, fixedSize(u.actBar, 240, 22), u.actText)))
	topLine := canvas.NewRectangle(pal().border)
	topLine.SetMinSize(fyne.NewSize(1, 1))

	main := container.NewBorder(
		container.NewVBox(u.header(), container.New(layout.NewCustomPaddedLayout(0, 0, 16, 16), u.banners)),
		container.NewVBox(topLine, activity),
		nil, nil,
		container.New(layout.NewCustomPaddedLayout(10, 0, 16, 16), u.body),
	)
	u.w.SetContent(main)
	u.show(0)
	u.say("Welcome! Loading mods…")
}

// header is the band across the top: a sky-to-sea gradient with the name,
// the tabs and status chips.
func (u *ui) header() fyne.CanvasObject {
	p := pal()
	grad := canvas.NewHorizontalGradient(p.headerFrom, p.headerTo)
	icon := canvas.NewImageFromResource(fyne.NewStaticResource("wbmm.png", desktop.IconPNG()))
	icon.FillMode = canvas.ImageFillContain
	name := text("WorldBox Mod Manager", p.headerText, sTitle, true)
	sub := text("GameBanana · Steam Workshop · NeoModLoader", p.headerMuted, sSmall, false)
	brand := container.NewHBox(fixedSize(icon, 34, 34), container.NewVBox(layout.NewSpacer(), name, sub, layout.NewSpacer()))

	names := []string{"Browse", "Installed", "Setup"}
	tabs := container.NewHBox()
	for i, n := range names {
		i := i
		b := widget.NewButton(n, func() { u.show(i) })
		u.tabBtns = append(u.tabBtns, b)
		tabs.Add(b)
	}
	u.countTxt = text("", p.headerText, sSmall, true)
	u.nmlChip = container.NewStack()
	u.runChip = badge("WorldBox is running", p.sand)
	u.runChip.Hide()
	chips := container.NewHBox(u.runChip, u.nmlChip)

	row := container.NewBorder(nil, nil,
		container.NewHBox(brand, widget.NewSeparator(), tabs, u.countTxt),
		container.NewVBox(layout.NewSpacer(), chips, layout.NewSpacer()))
	return container.NewStack(grad, container.New(layout.NewCustomPaddedLayout(10, 10, 16, 16), row))
}

func (u *ui) show(i int) {
	u.current = i
	for j, v := range u.views {
		if j == i {
			v.Show()
		} else {
			v.Hide()
		}
	}
	for j, b := range u.tabBtns {
		if j == i {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.LowImportance
		}
		b.Refresh()
	}
	switch i {
	case 1:
		u.inst.reload()
	case 2:
		u.set.refresh()
	}
}

// ---------- activity bar ----------

// say shows a message in the activity bar.
func (u *ui) say(format string, a ...any) {
	u.actText.Importance = widget.MediumImportance
	u.actText.SetText(fmt.Sprintf(format, a...))
	u.actUntil = time.Now().Add(8 * time.Second)
}

// fail shows an error in the activity bar and logs it.
func (u *ui) fail(err error) {
	log.Print(err)
	u.actText.Importance = widget.DangerImportance
	u.actText.SetText(err.Error())
	u.actUntil = time.Now().Add(15 * time.Second)
}

// progress shows (or with frac < 0, hides) the activity bar's progress.
func (u *ui) progress(frac float64) {
	if frac < 0 {
		u.actBar.Hide()
		return
	}
	u.actBar.SetValue(frac)
	u.actBar.Show()
}

// idle is the activity bar when nothing is happening.
func (u *ui) idle() {
	if time.Now().Before(u.actUntil) || u.actBar.Visible() {
		return
	}
	u.actText.Importance = widget.LowImportance
	n := len(u.inst.mods)
	switch {
	case u.gameErr != nil:
		u.actText.SetText("WorldBox not found. Set its folder in Setup.")
	case n == 1:
		u.actText.SetText("Ready · 1 mod installed")
	default:
		u.actText.SetText(fmt.Sprintf("Ready · %d mods installed", n))
	}
}

// ---------- status, banners ----------

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
		u.gameDir, u.gameErr, u.running = gd, gerr, running
		if ls != nil {
			u.loader = ls
		}
		u.paintStatus()
	})
}

func (u *ui) paintStatus() {
	p := pal()
	if u.running {
		u.runChip.Show()
	} else {
		u.runChip.Hide()
	}
	var chip fyne.CanvasObject
	switch {
	case u.loader == nil:
		chip = badge("NeoModLoader …", p.headerMuted)
	case !u.loader.Installed:
		chip = badge("NeoModLoader missing", p.bad)
	case !u.loader.UpToDate && u.loader.CheckError == "":
		chip = badge("NeoModLoader update", p.sand)
	default:
		chip = badge("NeoModLoader ready", p.headerText)
	}
	u.nmlChip.Objects = []fyne.CanvasObject{chip}
	u.nmlChip.Refresh()
	u.paintBanners()
	u.set.refresh()
}

func (u *ui) paintBanners() {
	p := pal()
	var bs []fyne.CanvasObject
	add := func(msg string, c color.NRGBA, buttons ...fyne.CanvasObject) {
		row := container.NewBorder(nil, nil, nil, container.NewHBox(buttons...), label(msg))
		bs = append(bs, tint(row, withAlpha(c, 0x30)))
	}
	if u.restartTo != "" {
		add(fmt.Sprintf("wbmm %s is installed. Restart to use it.", u.restartTo), p.grass,
			widget.NewButton("Restart now", u.restart))
	}
	if u.gameErr != nil {
		add(u.gameErr.Error(), p.warn, widget.NewButton("Open Setup", func() { u.show(2) }))
	} else if u.loader != nil && !u.loader.Installed {
		add("NeoModLoader isn't installed. WorldBox mods need it to load.", p.warn,
			widget.NewButton("Install NeoModLoader", u.installLoader))
	}
	if !u.hideInstall && !desktop.IsInstalled() && u.version != "dev" {
		install := widget.NewButton("Install", u.installApp)
		install.Importance = widget.HighImportance
		add("Install WorldBox Mod Manager as an app: a menu entry, a desktop shortcut, and updates in place.", p.sea,
			install, widget.NewButton("Not now", func() { u.hideInstall = true; u.paintBanners() }))
	}
	u.banners.Objects = bs
	u.banners.Refresh()
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
			u.paintBanners()
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
	u.refreshStatus(true)
	fyne.Do(func() { u.inst.reload() })
	time.Sleep(3 * time.Second)
	var lastApp, lastMods time.Time
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		cfg := u.m.Config()
		if !cfg.NoAppUpdates && time.Since(lastApp) > 6*time.Hour {
			lastApp = time.Now()
			u.selfUpdate(false)
		}
		if !cfg.NoModUpdates && time.Since(lastMods) > 6*time.Hour && !game.Running() {
			lastMods = time.Now()
			u.autoUpdateMods()
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
		u.paintBanners()
		u.say("wbmm %s is installed. Restart to use it.", rel.Version)
	})
}

func (u *ui) autoUpdateMods() {
	updated, held, err := u.m.AutoUpdate()
	fyne.Do(func() {
		switch {
		case len(updated) > 0:
			u.say("Updated %s.", strings.Join(updated, ", "))
			u.inst.reload()
		case len(held) > 0:
			u.say("Updates waiting for you under Installed: %s", strings.Join(held, ", "))
		}
		if err != nil {
			log.Printf("automatic mod updates: %v", err)
		}
	})
}

// ---------- opening things ----------

func (u *ui) openURL(link string) {
	p, err := url.Parse(link)
	if err != nil || (p.Scheme != "https" && p.Scheme != "steam") {
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
