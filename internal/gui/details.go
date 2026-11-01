package gui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/sources"
)

// detailsView is the panel beside a browser list: pictures, what to do
// with the item, its files (GameBanana) and its description.
type detailsView struct {
	u    *ui
	root *fyne.Container

	card     modCard
	has      bool
	seq      int
	hero     *fyne.Container
	strip    *fyne.Container
	stripBox *container.Scroll
	name     *widget.Label
	byline   *widget.Label
	stats    *widget.Label
	actions  *fyne.Container
	files    *fyne.Container
	about    *widget.Label
	body     fyne.CanvasObject
	empty    fyne.CanvasObject

	gb *sources.GBProfile
	ws *sources.WSItem
}

func newDetailsView(u *ui) *detailsView {
	d := &detailsView{u: u}
	d.hero = container.NewStack()
	d.strip = container.NewHBox()
	d.name = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	d.name.SizeName = sTitle
	d.name.Wrapping = fyne.TextWrapWord
	d.byline = mutedLabel("")
	d.stats = mutedLabel("")
	d.actions = container.NewVBox()
	d.files = container.NewVBox()
	d.about = label("")
	d.stripBox = container.NewHScroll(d.strip)
	content := container.NewVBox(
		heightOf(d.hero, 230),
		d.stripBox,
		tight(d.name, d.byline, d.stats),
		d.actions,
		d.files,
		section2("About", d.about),
	)
	d.body = container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 12, 12, 8), content))
	d.empty = placeholder("Pick something on the left to see it here.")
	d.root = container.NewStack(d.empty)
	return d
}

// heightOf gives o a fixed height and the full width.
func heightOf(o fyne.CanvasObject, h float32) fyne.CanvasObject {
	return container.New(&heightLayout{h}, o)
}

type heightLayout struct{ h float32 }

func (l *heightLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(objs[0].MinSize().Width, l.h)
}
func (l *heightLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Resize(fyne.NewSize(size.Width, l.h))
	objs[0].Move(fyne.NewPos(0, 0))
}

// section2 is a small heading over content.
func section2(name string, content fyne.CanvasObject) fyne.CanvasObject {
	h := text(strings.ToUpper(name), pal().muted, sSmall, true)
	return container.NewVBox(container.New(layout.NewCustomPaddedLayout(10, 0, 4, 0), h), content)
}

func (d *detailsView) open(c modCard) {
	d.card, d.has, d.gb, d.ws = c, true, nil, nil
	d.seq++
	seq := d.seq
	d.root.Objects = []fyne.CanvasObject{d.body}
	d.root.Refresh()
	d.name.SetText(c.Name)
	var by []string
	if c.Author != "" {
		by = append(by, "by "+c.Author)
	}
	if c.Category != "" {
		by = append(by, c.Category)
	}
	switch {
	case c.Kind == "world":
		by = append(by, "Workshop world")
	case c.Source == "workshop":
		by = append(by, "Workshop mod")
	default:
		by = append(by, "GameBanana")
	}
	d.byline.SetText(strings.Join(by, " · "))
	if c.Thumb != "" {
		d.setHero(c.Thumb, 460)
	} else {
		d.setLocalHero(c.Local)
	}
	d.strip.Objects = nil
	d.strip.Refresh()
	d.stripBox.Hide()
	d.about.SetText("Loading…")
	d.files.Objects = nil
	d.files.Refresh()
	d.paintStats()
	d.paintActions()
	go func() {
		var gb *sources.GBProfile
		var ws *sources.WSItem
		var err error
		if c.Source == "workshop" {
			var items []sources.WSItem
			items, err = sources.WSDetails([]string{c.ID})
			if err == nil && len(items) == 1 {
				ws = &items[0]
			} else if err == nil {
				err = fmt.Errorf("the Workshop item is gone or private")
			}
		} else {
			gb, err = sources.GBGetProfile(atoi(c.ID))
		}
		fyne.Do(func() {
			if seq != d.seq {
				return
			}
			if err != nil {
				d.about.SetText("Couldn't load its page: " + err.Error())
				return
			}
			d.gb, d.ws = gb, ws
			d.fill()
		})
	}()
}

func (d *detailsView) setHero(url string, w int) {
	img := newRemoteImage(url, w, canvas.ImageFillContain)
	bg := canvas.NewRectangle(pal().surface2)
	bg.CornerRadius = 10
	d.hero.Objects = []fyne.CanvasObject{bg, img}
	d.hero.Refresh()
}

func (d *detailsView) setLocalHero(path string) {
	img := localImage(path, 900)
	img.FillMode = canvas.ImageFillContain
	bg := canvas.NewRectangle(pal().surface2)
	bg.CornerRadius = 10
	d.hero.Objects = []fyne.CanvasObject{bg, img}
	d.hero.Refresh()
}

func (d *detailsView) fill() {
	switch {
	case d.gb != nil:
		text := sources.HTMLText(d.gb.Text)
		if text == "" {
			text = d.gb.Description
		}
		d.about.SetText(orNone(text))
		shots := d.gb.Screenshots()
		if len(shots) > 0 {
			d.setHero(shots[0], 1000)
		}
		d.paintStrip(shots)
	case d.ws != nil:
		d.about.SetText(orNone(sources.BBText(d.ws.Description)))
		if d.ws.Preview != "" {
			d.setHero(d.ws.Preview, 1000)
		}
	}
	d.paintStats()
	d.paintActions()
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "No description."
	}
	return s
}

func (d *detailsView) paintStrip(shots []string) {
	d.strip.Objects = nil
	if len(shots) < 2 {
		d.stripBox.Hide()
	} else {
		d.stripBox.Show()
	}
	if len(shots) > 1 {
		for _, s := range shots {
			s := s
			img := newRemoteImage(s, 220, canvas.ImageFillCover)
			img.CornerRadius = 5
			d.strip.Add(newTappable(fixedSize(img, 96, 54), func() { d.setHero(s, 1000) }, nil))
		}
	}
	d.strip.Refresh()
}

func (d *detailsView) paintStats() {
	c := d.card
	var parts []string
	switch {
	case d.gb != nil:
		parts = []string{count(d.gb.Downloads) + " downloads", count(d.gb.Likes) + " likes", count(d.gb.Views) + " views"}
		if d.gb.Version != "" {
			parts = append(parts, "version "+d.gb.Version)
		}
	case d.ws != nil:
		parts = []string{count(d.ws.Subs) + " subscribers", count(d.ws.Favorited) + " favorites", size(atoi64(d.ws.Size)),
			"since " + time.Unix(d.ws.Created, 0).Format("Jan 2006")}
	}
	if u := ago(c.Updated); u != "" {
		parts = append(parts, "updated "+u)
	}
	d.stats.SetText(strings.Join(parts, " · "))
}

// paintActions draws what can be done with the item now.
func (d *detailsView) paintActions() {
	if !d.has {
		return
	}
	u, c := d.u, d.card
	d.actions.Objects = nil
	key := u.opKey(c.Source, c.ID)
	page := widget.NewButtonWithIcon("Open page", theme.ComputerIcon(), func() { u.openURL(c.URL) })
	switch {
	case c.Kind == "map":
		id := atoi(c.ID)
		if recs := u.m.InstalledMaps()[id]; len(recs) > 0 {
			var slots []string
			for _, r := range recs {
				slots = append(slots, filepath.Base(r.Slot))
			}
			rm := widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() { u.removeMap(id, c.Name) })
			u.bindOp(key, rm)
			d.actions.Add(container.NewHBox(badge("installed", pal().grass), layout.NewSpacer(), rm, page))
			d.actions.Add(mutedLabel("In save slot " + strings.Join(slots, ", ") + ". Load it from WorldBox's Saves."))
		} else {
			in := widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() { u.installMap(id, c.Name, false) })
			in.Importance = widget.HighImportance
			u.bindOp(key, in)
			d.actions.Add(container.NewHBox(in, page))
			d.actions.Add(mutedLabel("Copies the world into a free save slot (only its save files: maps hold no code). Load it from WorldBox's Saves."))
		}
	case c.Source == "workshop":
		if u.m.Subscribed()[c.ID] {
			off := widget.NewButtonWithIcon("Unsubscribe", theme.DeleteIcon(), func() { u.unsubscribe(c.ID, c.Name) })
			u.bindOp(key, off)
			d.actions.Add(container.NewHBox(badge("subscribed", pal().grass), layout.NewSpacer(), off, page))
			if c.Kind == "world" {
				d.actions.Add(mutedLabel("Steam keeps it updated. Load it from WorldBox's saves menu."))
			} else {
				d.actions.Add(mutedLabel("Steam keeps it updated; wbmm checks every update before the game loads it."))
			}
			d.addOutdated(d.wsUpdated())
		} else {
			sub := widget.NewButtonWithIcon("Subscribe", theme.DownloadIcon(), func() { u.subscribe(c.ID, c.Name) })
			sub.Importance = widget.HighImportance
			u.bindOp(key, sub)
			d.actions.Add(container.NewHBox(sub, page))
			d.actions.Add(mutedLabel("Subscribes through your Steam client (Steam needs to be running), follows the download and checks the mod before the game can load it."))
			if c.Kind != "world" {
				d.addOutdated(d.wsUpdated())
			}
		}
	default:
		have := u.m.InstalledGB()[atoi(c.ID)]
		row := container.NewHBox()
		if have {
			row.Add(badge("installed", pal().grass))
			row.Add(layout.NewSpacer())
		}
		if d.gb != nil && len(d.gb.Files) > 0 {
			newest := d.gb.Files[0]
			lbl := "Install"
			if have {
				lbl = "Reinstall newest"
			}
			btn := widget.NewButtonWithIcon(lbl, theme.DownloadIcon(), func() { u.installGB(atoi(c.ID), newest, c.Name, false) })
			if !have {
				btn.Importance = widget.HighImportance
			}
			u.bindOp(key, btn)
			row.Add(btn)
		}
		row.Add(page)
		d.actions.Add(row)
		if d.gb != nil && len(d.gb.Files) > 0 {
			d.addOutdated(d.gb.Files[0].Added)
			d.actions.Add(d.paintCheck(atoi(c.ID), d.gb.Files[0]))
		} else {
			d.addOutdated(c.Updated)
		}
	}
	d.actions.Refresh()
	d.paintFiles()
}

func (d *detailsView) paintFiles() {
	u, c := d.u, d.card
	d.files.Objects = nil
	if d.gb == nil || len(d.gb.Files) < 2 || c.Kind == "map" {
		d.files.Refresh()
		return
	}
	installed := u.m.InstalledFiles(atoi(c.ID))
	p := pal()
	list := container.NewVBox()
	for _, f := range d.gb.Files {
		f := f
		meta := size(f.Size) + " · " + ago(f.Added)
		if f.Version != "" {
			meta += " · " + f.Version
		}
		if !f.Clean() {
			meta += " · virus scan not passed"
		}
		info := tight(line(f.Name, true, theme.SizeNameText, widget.MediumImportance), line(meta, false, sSmall, widget.LowImportance))
		var right fyne.CanvasObject
		if installed[f.ID] {
			right = badge("installed", p.grass)
		} else {
			btn := widget.NewButton("Install", func() { u.installGB(atoi(c.ID), f, c.Name, false) })
			u.bindOp(u.opKey(c.Source, c.ID), btn)
			right = btn
		}
		list.Add(container.NewBorder(nil, nil, nil, container.NewCenter(right), info))
		if f.Description != "" {
			list.Add(mutedLabel(f.Description))
		}
	}
	d.files.Add(section2(fmt.Sprintf("Files (%d)", len(d.gb.Files)), list))
	d.files.Refresh()
}

// addOutdated warns about a mod last updated (at t) before the current
// WorldBox version.
func (d *detailsView) addOutdated(t int64) {
	if !d.u.outdated(t) {
		return
	}
	l := label(d.u.outdatedNote())
	l.Importance = widget.WarningImportance
	l.Wrapping = fyne.TextWrapWord
	d.actions.Add(l)
}

// wsUpdated is when the Workshop item's files last changed.
func (d *detailsView) wsUpdated() int64 {
	if d.ws != nil {
		return d.ws.Updated
	}
	return d.card.Updated
}

func atoi64(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	return n
}
