package gui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/modpack"
)

// modpacksPage lists the modpacks saved in wbmm (profiles) and shows one:
// what applying it would do, and the buttons to apply, share or save it.
type modpacksPage struct {
	u     *ui
	root  fyne.CanvasObject
	list  *fyne.Container
	panel *fyne.Container

	saved    []manager.SavedPack
	selected string        // path of the selected saved pack
	opened   *modpack.Pack // a pack opened from a file or code, not saved yet
	editing  bool          // the selected saved pack is being edited
}

func newModpacksPage(u *ui) *modpacksPage {
	v := &modpacksPage{u: u}
	save := widget.NewButtonWithIcon("Save my mods", theme.DocumentSaveIcon(), v.saveCurrent)
	save.Importance = widget.HighImportance
	openFile := widget.NewButtonWithIcon("Open file", theme.FolderOpenIcon(), v.openFile)
	paste := widget.NewButtonWithIcon("Paste code", theme.ContentPasteIcon(), v.pasteCode)
	v.list = container.NewVBox()
	v.panel = container.NewStack(placeholder("Pick a modpack, or open one someone shared."))
	intro := mutedLabel("A modpack is a list of mods in one short code (a .wbm file). Share it, and wbmm installs the same mods for whoever opens it. Saved packs work as profiles: switch between sets of mods in one click.")
	left := container.NewBorder(intro, nil, nil, nil, container.NewVScroll(v.list))
	split := container.NewHSplit(left, v.panel)
	split.Offset = 0.45
	v.root = container.NewBorder(pageHeader("Modpacks", save, openFile, paste), nil, nil, nil, split)
	return v
}

func (v *modpacksPage) reload() {
	saved, err := manager.SavedPacks()
	if err != nil {
		v.u.fail(err)
	}
	v.saved = saved
	if len(saved) > 0 {
		v.u.nav[pageModpacks].setCount(fmt.Sprint(len(saved)))
	} else {
		v.u.nav[pageModpacks].setCount("")
	}
	v.paint()
}

func (v *modpacksPage) paint() {
	v.list.Objects = nil
	if v.opened != nil {
		v.list.Add(v.row(*v.opened, "", true))
	}
	for _, sp := range v.saved {
		v.list.Add(v.row(sp.Pack, sp.Path, false))
	}
	if len(v.list.Objects) == 0 {
		v.list.Add(container.NewVBox(label("No modpacks yet."), widget.NewButton("Save my mods as a modpack", v.saveCurrent)))
	}
	v.list.Refresh()
	v.paintPanel()
}

func (v *modpacksPage) row(pk modpack.Pack, path string, opened bool) fyne.CanvasObject {
	sub := fmt.Sprintf("%d mods", len(pk.Entries))
	if opened {
		sub += " · opened, not saved"
	}
	text := tight(line(pk.Name, true, theme.SizeNameText, widget.MediumImportance), line(sub, false, sSmall, widget.LowImportance))
	icon := widget.NewIcon(theme.StorageIcon())
	selected := (opened && v.selected == "") || (!opened && path == v.selected)
	return listRow(container.NewBorder(nil, nil, fixedSize(icon, 28, 28), nil, container.NewVBox(layout.NewSpacer(), text, layout.NewSpacer())),
		selected, func() {
			v.editing = false
			if opened {
				v.selected = ""
			} else {
				v.selected = path
			}
			v.paint()
		})
}

// current is the pack the panel shows.
func (v *modpacksPage) current() (*modpack.Pack, string) {
	if v.selected == "" {
		return v.opened, ""
	}
	for i := range v.saved {
		if v.saved[i].Path == v.selected {
			return &v.saved[i].Pack, v.saved[i].Path
		}
	}
	return nil, ""
}

func (v *modpacksPage) paintPanel() {
	pk, path := v.current()
	if pk == nil {
		v.panel.Objects = []fyne.CanvasObject{placeholder("Pick a modpack, or open one someone shared.")}
		v.panel.Refresh()
		return
	}
	if v.editing && path != "" {
		v.paintEditor(pk, path)
		return
	}
	u, p := v.u, pal()
	only := widget.NewCheck("Switch off mods that aren't in this pack", nil)
	steps := container.NewVBox(label("Checking what's installed…"))
	apply := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), nil)
	apply.Importance = widget.HighImportance
	apply.Disable()
	var plan []manager.Step
	replan := func() {
		pack, excl := *pk, only.Checked
		go func() {
			st, err := u.m.PlanPack(pack, excl)
			fyne.Do(func() {
				steps.Objects = nil
				if err != nil {
					steps.Add(label(err.Error()))
					steps.Refresh()
					return
				}
				plan = st
				changes := 0
				for _, s := range st {
					steps.Add(stepRow(s))
					if s.What != "ok" && s.What != "manual" {
						changes++
					}
				}
				steps.Refresh()
				if changes == 0 {
					apply.SetText("Nothing to change")
					apply.Disable()
				} else {
					apply.SetText(fmt.Sprintf("Apply (%d changes)", changes))
					apply.Enable()
				}
			})
		}()
	}
	only.OnChanged = func(bool) { replan() }
	apply.OnTapped = func() { v.apply(plan) }

	copyCode := widget.NewButtonWithIcon("Copy code", theme.ContentCopyIcon(), func() {
		u.a.Clipboard().SetContent(pk.Encode())
		u.say("Copied %s's code. Paste it anywhere; whoever opens it in wbmm gets the same mods.", pk.Name)
	})
	saveFile := widget.NewButtonWithIcon("Save .wbm file", theme.DocumentSaveIcon(), func() { v.saveFile(*pk) })
	acts := container.NewHBox(apply, copyCode, saveFile)
	if path == "" {
		keep := widget.NewButtonWithIcon("Keep in my modpacks", theme.ContentAddIcon(), func() {
			if _, err := manager.SavePack(*pk); err != nil {
				u.fail(err)
				return
			}
			v.opened = nil
			v.reload()
		})
		acts.Add(keep)
	} else {
		acts.Add(widget.NewButtonWithIcon("Edit", theme.DocumentCreateIcon(), func() {
			v.editing = true
			v.paintPanel()
		}))
		del := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
			dialog.ShowConfirm("Delete "+pk.Name+"?", "Only the saved modpack is deleted; your mods stay.", func(ok bool) {
				if ok {
					os.Remove(path)
					v.selected = ""
					v.reload()
				}
			}, u.w)
		})
		acts.Add(del)
	}
	code := widget.NewLabel(pk.Encode())
	code.Wrapping = fyne.TextWrapBreak
	code.TextStyle.Monospace = true
	code.SizeName = sSmall

	body := container.NewVBox(
		line(pk.Name, true, sTitle, widget.MediumImportance),
		mutedLabel(fmt.Sprintf("%d mods", len(pk.Entries))),
		acts, only,
		section2("What applying it does", steps),
		section2("Code", tint(code, withAlpha(p.text, 0x0a))),
	)
	v.panel.Objects = []fyne.CanvasObject{container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 12, 12, 8), body))}
	v.panel.Refresh()
	replan()
}

// stepRow says what applying a pack does with one mod.
func stepRow(s manager.Step) fyne.CanvasObject {
	p := pal()
	words, c := map[string]string{
		"ok":         "ready",
		"install":    "will install from GameBanana",
		"update":     "will install the pack's file",
		"subscribe":  "will subscribe on the Workshop",
		"switch-on":  "will switch on",
		"switch-off": "will switch off",
		"manual":     "installed by hand: get it yourself",
	}[s.What], p.grass
	switch s.What {
	case "manual":
		c = p.warn
	case "install", "update", "subscribe":
		c = p.sea
	case "switch-on", "switch-off":
		c = p.sand
	}
	name := s.Entry.Name
	if !s.Entry.Enabled && s.What != "switch-off" {
		name += " (off in the pack)"
	}
	return container.NewBorder(nil, nil, nil, container.NewCenter(badge(words, c)), line(name, false, theme.SizeNameText, widget.MediumImportance))
}

func (v *modpacksPage) apply(plan []manager.Step) {
	u := v.u
	if u.running {
		u.fail(fmt.Errorf("close WorldBox first: NeoModLoader rewrites its mod list while the game runs"))
		return
	}
	u.say("Applying the modpack…")
	go func() {
		errs := u.m.ApplyPack(plan, func(msg string) { fyne.Do(func() { u.say("%s", msg) }) })
		fyne.Do(func() {
			if len(errs) > 0 {
				var msgs []string
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				dialog.ShowInformation("Some of the modpack didn't apply", strings.Join(msgs, "\n"), u.w)
			} else {
				u.say("Modpack applied. It takes effect the next time WorldBox starts.")
			}
			u.refreshViews()
			v.paintPanel()
		})
	}()
}

func (v *modpacksPage) saveCurrent() {
	name := widget.NewEntry()
	name.SetPlaceHolder("Name, like \"Modern war\"")
	dialog.ShowForm("Save my mods as a modpack", "Save", "Cancel", []*widget.FormItem{widget.NewFormItem("Name", name)}, func(ok bool) {
		if !ok {
			return
		}
		n := strings.TrimSpace(name.Text)
		if n == "" {
			n = "My mods"
		}
		pk, err := v.u.m.ExportPack(n)
		if err != nil {
			v.u.fail(err)
			return
		}
		path, err := manager.SavePack(pk)
		if err != nil {
			v.u.fail(err)
			return
		}
		v.selected = path
		v.reload()
		v.u.say("Saved %s (%d mods).", n, len(pk.Entries))
	}, v.u.w)
}

func (v *modpacksPage) saveFile(pk modpack.Pack) {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil {
			return
		}
		path := w.URI().Path()
		w.Close()
		if !strings.HasSuffix(strings.ToLower(path), modpack.Ext) {
			os.Remove(path) // the dialog made it empty; write it with the extension
			path += modpack.Ext
		}
		if err := manager.WritePackFile(path, pk); err != nil {
			v.u.fail(err)
			return
		}
		v.u.say("Saved %s.", path)
	}, v.u.w)
	d.SetFileName(pk.Name + modpack.Ext)
	d.Show()
}

func (v *modpacksPage) openFile() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, 1<<20))
		if err != nil {
			v.u.fail(err)
			return
		}
		v.openText(string(b))
	}, v.u.w)
	d.SetFilter(storage.NewExtensionFileFilter([]string{modpack.Ext}))
	d.Show()
}

func (v *modpacksPage) pasteCode() {
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder("WBM1.…")
	entry.SetText(v.u.a.Clipboard().Content())
	dialog.ShowForm("Open a modpack code", "Open", "Cancel", []*widget.FormItem{widget.NewFormItem("Code", entry)}, func(ok bool) {
		if ok {
			v.openText(entry.Text)
		}
	}, v.u.w)
}

// openText shows a pack from a code or a .wbm file's text.
func (v *modpacksPage) openText(text string) {
	pk, err := modpack.Decode(text)
	if err != nil {
		v.u.fail(err)
		return
	}
	v.opened, v.selected = &pk, ""
	v.u.show(pageModpacks)
	v.paint()
	v.u.say("Opened %s (%d mods). Check the list, then Apply.", pk.Name, len(pk.Entries))
}

// openPath opens a .wbm file given on the command line or forwarded by a
// second start (a double-click on the file).
func (v *modpacksPage) openPath(path string) {
	pk, err := manager.ReadPackFile(path)
	if err != nil {
		v.u.fail(fmt.Errorf("opening %s: %w", path, err))
		return
	}
	v.opened, v.selected = &pk, ""
	v.u.show(pageModpacks)
	v.paint()
	v.u.say("Opened %s (%d mods). Check the list, then Apply.", pk.Name, len(pk.Entries))
}

// ---------- editing a saved pack ----------

// entryKey identifies a pack entry: where it comes from, or its name for
// mods installed by hand.
func entryKey(e modpack.Entry) string {
	if e.Source == modpack.Local {
		return "local:" + strings.ToLower(e.Name)
	}
	return fmt.Sprintf("%d:%d", e.Source, e.ID)
}

func entrySource(e modpack.Entry) string {
	return map[int]string{modpack.GameBanana: "GameBanana", modpack.Workshop: "Workshop", modpack.Local: "installed by hand"}[e.Source]
}

// paintEditor shows a saved pack's mods to rename it, switch mods on or
// off in it, remove and add them. Each change is saved straight away.
func (v *modpacksPage) paintEditor(pk *modpack.Pack, path string) {
	u := v.u
	save := func() bool {
		if err := manager.WritePackFile(path, *pk); err != nil {
			u.fail(err)
			return false
		}
		return true
	}
	name := widget.NewEntry()
	name.SetText(pk.Name)
	rename := func() {
		n := strings.TrimSpace(name.Text)
		if n == "" || n == pk.Name {
			return
		}
		old := pk.Name
		pk.Name = n
		np, err := manager.SavePack(*pk)
		if err != nil {
			pk.Name = old
			u.fail(err)
			return
		}
		if np != path {
			os.Remove(path)
		}
		v.selected = np
	}
	name.OnSubmitted = func(string) { rename(); v.reload() }

	list := container.NewVBox()
	for i := range pk.Entries {
		i := i
		e := pk.Entries[i]
		sw := newToggle(e.Enabled, func(on bool) {
			pk.Entries[i].Enabled = on
			save()
		})
		rm := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			pk.Entries = append(pk.Entries[:i], pk.Entries[i+1:]...)
			if save() {
				v.paintPanel()
			}
		})
		rm.Importance = widget.LowImportance
		info := tight(line(e.Name, true, theme.SizeNameText, widget.MediumImportance), line(entrySource(e), false, sSmall, widget.LowImportance))
		list.Add(container.NewBorder(nil, nil, nil, container.NewHBox(container.NewCenter(sw), rm), info))
	}
	if len(pk.Entries) == 0 {
		list.Add(mutedLabel("No mods in it. Add some of yours."))
	}

	add := widget.NewButtonWithIcon("Add installed mods", theme.ContentAddIcon(), func() { v.addToPack(pk, save) })
	add.Importance = widget.HighImportance
	useMine := widget.NewButtonWithIcon("Use my current mods", theme.ViewRefreshIcon(), func() {
		dialog.ShowConfirm("Replace "+pk.Name+"'s mods?", "It will list the mods installed now, on or off as they are now.", func(ok bool) {
			if !ok {
				return
			}
			cur, err := u.m.ExportPack(pk.Name)
			if err != nil {
				u.fail(err)
				return
			}
			pk.Entries = cur.Entries
			if save() {
				v.paintPanel()
			}
		}, u.w)
	})
	done := widget.NewButtonWithIcon("Done", theme.ConfirmIcon(), func() {
		rename()
		v.editing = false
		v.reload()
	})

	body := container.NewVBox(
		widget.NewForm(widget.NewFormItem("Name", name)),
		container.NewHBox(add, useMine, layout.NewSpacer(), done),
		mutedLabel("The switch says whether a mod is on when the pack is applied. Changes are saved as you make them."),
		section2(fmt.Sprintf("Mods (%d)", len(pk.Entries)), list),
	)
	v.panel.Objects = []fyne.CanvasObject{container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 12, 12, 8), body))}
	v.panel.Refresh()
}

// addToPack lets the person pick installed mods the pack doesn't list.
func (v *modpacksPage) addToPack(pk *modpack.Pack, save func() bool) {
	u := v.u
	cur, err := u.m.ExportPack("")
	if err != nil {
		u.fail(err)
		return
	}
	in := map[string]bool{}
	for _, e := range pk.Entries {
		in[entryKey(e)] = true
	}
	var candidates []modpack.Entry
	for _, e := range cur.Entries {
		if !in[entryKey(e)] {
			candidates = append(candidates, e)
		}
	}
	if len(candidates) == 0 {
		u.say("Every installed mod is already in %s.", pk.Name)
		return
	}
	picked := map[int]bool{}
	box := container.NewVBox()
	for i, e := range candidates {
		i := i
		box.Add(widget.NewCheck(e.Name+"  ·  "+entrySource(e), func(on bool) { picked[i] = on }))
	}
	sc := container.NewVScroll(box)
	sc.SetMinSize(fyne.NewSize(420, 320))
	dialog.ShowCustomConfirm("Add to "+pk.Name, "Add", "Cancel", sc, func(ok bool) {
		if !ok {
			return
		}
		n := 0
		for i, e := range candidates {
			if picked[i] {
				e.Enabled = true
				pk.Entries = append(pk.Entries, e)
				n++
			}
		}
		if n > 0 && save() {
			u.say("Added %d mod(s) to %s.", n, pk.Name)
			v.paintPanel()
		}
	}, u.w)
}
