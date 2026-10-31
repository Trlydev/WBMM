package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/desktop"
)

type setupView struct {
	u    *ui
	root fyne.CanvasObject

	nmlText  *widget.Label
	nmlBtn   *widget.Button
	notes    *widget.Button
	gameText *widget.Label
	gameDir  *widget.Entry
	modsAuto *widget.Check
	appAuto  *widget.Check
	appText  *widget.Label
	appBtn   *widget.Button
}

func newSetupView(u *ui) *setupView {
	s := &setupView{u: u}

	s.nmlText = label("Checking…")
	s.nmlBtn = widget.NewButtonWithIcon("Install NeoModLoader", theme.DownloadIcon(), u.installLoader)
	s.nmlBtn.Importance = widget.HighImportance
	s.notes = widget.NewButton("Release notes", func() {
		if u.loader != nil {
			u.openURL(u.loader.ReleaseURL)
		}
	})
	nml := section("NeoModLoader",
		"The loader WorldBox mods need. It lives in the game's StreamingAssets/mods folder.",
		s.nmlText, container.NewHBox(s.nmlBtn, s.notes))

	s.gameText = label("")
	s.gameDir = widget.NewEntry()
	s.gameDir.SetPlaceHolder("Empty: find WorldBox through Steam")
	choose := widget.NewButtonWithIcon("Choose…", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(d fyne.ListableURI, err error) {
			if err == nil && d != nil {
				s.gameDir.SetText(d.Path())
				s.saveGameDir()
			}
		}, u.w)
	})
	save := widget.NewButton("Save", s.saveGameDir)
	gameRow := container.NewBorder(nil, nil, nil, container.NewHBox(choose, save), s.gameDir)
	gameCard := section("Game folder", "", s.gameText, gameRow)

	cfg := u.m.Config()
	s.modsAuto = widget.NewCheck("Update GameBanana mods automatically", func(bool) { s.saveUpdates() })
	s.modsAuto.SetChecked(!cfg.NoModUpdates)
	s.appAuto = widget.NewCheck("Update WorldBox Mod Manager automatically", func(bool) { s.saveUpdates() })
	s.appAuto.SetChecked(!cfg.NoAppUpdates)
	checkNow := widget.NewButtonWithIcon("Check now", theme.ViewRefreshIcon(), func() {
		go u.selfUpdate(true)
		u.inst.checkUpdates(true)
	})
	updates := section("Updates",
		"Checked when wbmm starts and every six hours. Mod updates only follow the file you installed (a mod's other versions or ports are never swapped in) and only install when GameBanana's virus scan passed them. Steam keeps Workshop mods updated itself.",
		s.modsAuto, s.appAuto, container.NewHBox(checkNow))

	s.appText = label("")
	s.appBtn = widget.NewButton("", nil)
	app := section("App", "", s.appText, container.NewHBox(s.appBtn))

	how := section("How it works", "",
		mutedLabel("GameBanana: wbmm downloads the file, checks GameBanana's virus scan, unpacks it and puts each mod in worldbox/Mods, and remembers where it came from to update it."),
		mutedLabel("Steam Workshop: wbmm subscribes through your Steam client (Steam must be running), the same way the game does. Steam downloads the mod, keeps it updated, and removes it when you unsubscribe."),
		mutedLabel("Turning a mod off writes to NeoModLoader's own mod list, the one its in-game Mods window uses."),
		mutedLabel("The first time, turn on Experimental Mode in WorldBox's settings, or mods won't load."),
		mutedLabel("Mods are code that runs on your PC. Install mods from authors you trust: no virus scan catches everything."))

	about := mutedLabel("WorldBox Mod Manager " + u.version + " · github.com/Trlydev/WBMM · Not affiliated with Maxim Karpenko, Valve, GameBanana or the NeoModLoader team.")

	s.root = container.NewVScroll(maxWidth(container.New(layout.NewCustomPaddedLayout(0, 16, 0, 8),
		container.NewVBox(nml, gameCard, updates, app, how, about)), 900))
	return s
}

func section(name, desc string, objs ...fyne.CanvasObject) fyne.CanvasObject {
	box := container.NewVBox(title(name))
	if desc != "" {
		box.Add(mutedLabel(desc))
	}
	for _, o := range objs {
		box.Add(o)
	}
	return container.New(layout.NewCustomPaddedLayout(0, 8, 0, 0), card(box, 14))
}

func (s *setupView) saveGameDir() {
	if err := s.u.m.SetGameDir(s.gameDir.Text); err != nil {
		s.u.fail(err)
		return
	}
	s.u.say("Game folder saved.")
	go s.u.refreshStatus(true)
	s.u.inst.reload()
}

func (s *setupView) saveUpdates() {
	if s.modsAuto == nil || s.appAuto == nil {
		return
	}
	if err := s.u.m.SetUpdates(s.modsAuto.Checked, s.appAuto.Checked); err != nil {
		s.u.fail(err)
	}
}

func (s *setupView) refresh() {
	u := s.u
	switch {
	case u.gameErr != nil:
		s.gameText.SetText(u.gameErr.Error())
	default:
		s.gameText.SetText("Using " + u.gameDir)
	}
	if s.u.w.Canvas().Focused() != s.gameDir && s.gameDir.Text == "" {
		s.gameDir.SetText(u.m.Config().GameDir)
	}

	s.nmlBtn.Hide()
	s.notes.Hide()
	switch l := u.loader; {
	case u.gameErr != nil:
		s.nmlText.SetText("Set the game folder first.")
	case l == nil:
		s.nmlText.SetText("Checking…")
	case !l.Installed:
		s.nmlText.SetText("Not installed.")
		s.nmlBtn.SetText("Install NeoModLoader")
		s.nmlBtn.Show()
	case l.CheckError != "":
		s.nmlText.SetText("Installed. Couldn't check for a newer version: " + l.CheckError)
	case !l.UpToDate:
		s.nmlText.SetText(fmt.Sprintf("Installed, but %s is out.", l.Latest))
		s.nmlBtn.SetText("Update NeoModLoader")
		s.nmlBtn.Show()
	default:
		s.nmlText.SetText(fmt.Sprintf("Installed and up to date (%s).", l.Latest))
	}
	if u.loader != nil && u.loader.ReleaseURL != "" {
		s.notes.Show()
	}

	if desktop.IsInstalled() {
		s.appText.SetText("Installed at " + desktop.InstallPath() + ", with a menu entry. Uninstalling leaves your mods and settings alone.")
		s.appBtn.SetText("Uninstall app")
		s.appBtn.OnTapped = func() {
			dialog.ShowConfirm("Uninstall WorldBox Mod Manager?", "The program, menu entry and shortcuts are removed. Your mods stay.", func(ok bool) {
				if ok {
					if err := desktop.Uninstall(func(string, ...any) {}); err != nil {
						u.fail(err)
						return
					}
					u.a.Quit()
				}
			}, u.w)
		}
	} else {
		s.appText.SetText("Running from " + desktop.StartedAs() + ". Install it as an app to get a menu entry and a fixed place for updates.")
		s.appBtn.SetText("Install as app")
		s.appBtn.OnTapped = u.installApp
	}
	s.appBtn.Refresh()
}
