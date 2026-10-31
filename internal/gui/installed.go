package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/manager"
)

type installedView struct {
	u       *ui
	root    fyne.CanvasObject
	filter  *widget.Entry
	list    *fyne.Container
	mods    []manager.Mod
	updates map[string]manager.Update
	err     error
}

func newInstalledView(u *ui) *installedView {
	v := &installedView{u: u, updates: map[string]manager.Update{}}
	v.filter = widget.NewEntry()
	v.filter.SetPlaceHolder("Filter installed mods")
	v.filter.OnChanged = func(string) { v.paint() }
	check := widget.NewButtonWithIcon("Check for updates", theme.ViewRefreshIcon(), func() { v.checkUpdates(true) })
	folder := widget.NewButtonWithIcon("Mods folder", theme.FolderOpenIcon(), func() {
		if gd, err := u.m.GameDir(); err == nil {
			if err := openFolder(game.ModsDir(gd)); err != nil {
				u.fail(err)
			}
		}
	})
	toolbar := container.NewBorder(nil, nil, nil, container.NewHBox(check, folder), v.filter)
	v.list = container.NewVBox()
	note := mutedLabel("Switches take effect the next time WorldBox starts. Close the game before changing them.")
	v.root = container.NewBorder(container.NewVBox(toolbar, note), nil, nil, nil,
		container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 16, 0, 8), v.list)))
	return v
}

// reload re-reads the installed mods.
func (v *installedView) reload() {
	go func() {
		mods, err := v.u.m.List()
		fyne.Do(func() {
			v.mods, v.err = mods, err
			n := len(mods)
			if n > 0 {
				v.u.countTxt.Text = fmt.Sprintf("%d installed", n)
			} else {
				v.u.countTxt.Text = ""
			}
			v.u.countTxt.Refresh()
			v.paint()
			v.u.idle()
		})
	}()
}

func (v *installedView) checkUpdates(loud bool) {
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

func (v *installedView) paint() {
	v.list.Objects = nil
	if v.err != nil {
		v.list.Add(label(v.err.Error()))
		v.list.Refresh()
		return
	}
	if len(v.mods) == 0 {
		v.list.Add(container.NewCenter(label("No mods yet. Find some under Browse.")))
		v.list.Refresh()
		return
	}
	f := strings.ToLower(strings.TrimSpace(v.filter.Text))
	var local, ws []fyne.CanvasObject
	for _, md := range v.mods {
		if f != "" && !strings.Contains(strings.ToLower(md.Name+" "+md.Author), f) {
			continue
		}
		row := v.row(md)
		if md.Source == "workshop" {
			ws = append(ws, row)
		} else {
			local = append(local, row)
		}
	}
	if len(local) > 0 {
		v.list.Add(title(fmt.Sprintf("Mods folder (%d)", len(local))))
		v.list.Objects = append(v.list.Objects, local...)
	}
	if len(ws) > 0 {
		v.list.Add(title(fmt.Sprintf("Steam Workshop (%d)", len(ws))))
		v.list.Objects = append(v.list.Objects, ws...)
	}
	if len(local)+len(ws) == 0 {
		v.list.Add(label("Nothing matches."))
	}
	v.list.Refresh()
}

func (v *installedView) row(md manager.Mod) fyne.CanvasObject {
	u, p := v.u, pal()
	icon := localImage(md.Icon, 96)
	icon.CornerRadius = 8
	var iconBox fyne.CanvasObject = fixedSize(icon, 48, 48)
	if md.Icon == "" {
		initial := "?"
		if md.Name != "" {
			initial = strings.ToUpper(md.Name[:1])
		}
		iconBox = fixedSize(tint(container.NewCenter(text(initial, p.muted, sTitle, true)), p.surface2), 48, 48)
	}

	var src fyne.CanvasObject
	switch md.Source {
	case "gamebanana":
		src = badge("GameBanana", p.sand)
	case "workshop":
		src = badge("Workshop", p.sea)
	default:
		src = badge("Added by hand", p.muted)
	}
	var name fyne.CanvasObject = widget.NewLabelWithStyle(md.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	if c, ok := cardFromMod(md); ok {
		// The name opens the mod's preview page.
		link := widget.NewHyperlink(md.Name, nil)
		link.TextStyle.Bold = true
		link.OnTapped = func() {
			u.show(0)
			u.br.openDetails(c)
		}
		name = link
	}
	ver := widget.NewLabel(md.Version)
	ver.Importance = widget.LowImportance
	meta := ""
	if md.Author != "" {
		meta = "by " + md.Author
	}
	if md.FileName != "" {
		meta += " · " + md.FileName
	}
	info := container.NewVBox(container.NewHBox(name, ver, container.NewCenter(src)), mutedLabel(meta))
	if md.Problem != "" {
		w := label(md.Problem)
		w.Importance = widget.WarningImportance
		info.Add(w)
	}

	toggle := widget.NewCheck("Load", nil)
	toggle.SetChecked(md.Enabled)
	toggle.OnChanged = func(on bool) {
		if err := u.m.SetEnabled(md.UID, on); err != nil {
			u.fail(err)
			toggle.OnChanged = nil
			toggle.SetChecked(!on)
			v.reload()
			return
		}
		if on {
			u.say("%s will load the next time WorldBox starts.", md.Name)
		} else {
			u.say("%s is off from the next start.", md.Name)
		}
	}
	if md.UID == "" || md.Problem != "" || u.running {
		toggle.Disable()
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
	if md.URL != "" {
		acts.Add(widget.NewButtonWithIcon("", theme.ComputerIcon(), func() { u.openURL(md.URL) }))
	}
	if md.Problem == "" {
		acts.Add(widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			if err := openFolder(md.Folder); err != nil {
				u.fail(err)
			}
		}))
	}
	if md.Source == "workshop" {
		b := widget.NewButtonWithIcon("Unsubscribe", theme.DeleteIcon(), func() { u.unsubscribe(md.WorkshopID, md.Name) })
		u.bindOp(u.opKey("workshop", md.WorkshopID), b)
		acts.Add(b)
	} else if md.UID != "" && md.Problem == "" {
		acts.Add(widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() { u.remove(md) }))
	}

	right := container.NewHBox(container.NewCenter(toggle), container.NewCenter(acts))
	return card(container.NewBorder(nil, nil, container.NewCenter(iconBox), right, info), 8)
}

// cardFromMod lets an installed GameBanana or Workshop mod open its page.
func cardFromMod(md manager.Mod) (modCard, bool) {
	switch md.Source {
	case "gamebanana":
		return modCard{Source: "gamebanana", ID: fmt.Sprint(md.GBID), Name: md.Name, Author: md.Author, URL: md.URL}, true
	case "workshop":
		return modCard{Source: "workshop", ID: md.WorkshopID, Name: md.Name, Author: md.Author, URL: md.URL}, true
	}
	return modCard{}, false
}
