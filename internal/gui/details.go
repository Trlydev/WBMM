package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/sources"
)

// detailsView is a mod's preview page: pictures, description, stats, and
// its files (GameBanana) or the Subscribe button (Workshop).
type detailsView struct {
	b    *browseView
	root *fyne.Container

	card    modCard
	seq     int
	hero    *fyne.Container // the big picture
	strip   *fyne.Container // screenshot thumbnails
	name    *widget.Label
	byline  *widget.Label
	stats   *fyne.Container
	actions *fyne.Container
	files   *fyne.Container
	filesH  fyne.CanvasObject
	about   *widget.Label

	gb *sources.GBProfile
	ws *sources.WSItem
}

func newDetailsView(b *browseView) *detailsView {
	d := &detailsView{b: b}
	back := widget.NewButtonWithIcon("Back to mods", theme.NavigateBackIcon(), b.closeDetails)
	back.Importance = widget.LowImportance
	page := widget.NewButtonWithIcon("Open page", theme.ComputerIcon(), func() { b.u.openURL(d.card.URL) })
	page.Importance = widget.LowImportance

	d.hero = container.NewStack()
	d.strip = container.NewHBox()
	d.name = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	d.name.SizeName = sHuge
	d.name.Wrapping = fyne.TextWrapWord
	d.byline = mutedLabel("")
	d.stats = container.NewGridWithColumns(2)
	d.actions = container.NewVBox()
	d.files = container.NewVBox()
	d.filesH = title("Files")
	d.about = label("")

	heroBox := fixedSize(d.hero, 560, 315)
	left := container.NewVBox(heroBox, container.NewHScroll(d.strip))
	right := card(container.NewVBox(d.name, d.byline, widget.NewSeparator(), d.stats, widget.NewSeparator(), d.actions), 14)
	top := container.New(&detailsTop{}, left, right)
	body := container.NewVBox(top, d.filesH, d.files, title("About this mod"), card(d.about, 12))
	d.root = container.NewBorder(container.NewBorder(nil, nil, back, page), nil, nil, nil,
		container.NewVScroll(container.New(layout.NewCustomPaddedLayout(4, 16, 0, 8), body)))
	return d
}

// detailsTop puts the pictures beside the info panel, which takes the
// rest of the width.
type detailsTop struct{}

const infoMinW = 320

func (detailsTop) MinSize(objs []fyne.CanvasObject) fyne.Size {
	l, r := objs[0].MinSize(), objs[1].MinSize()
	return fyne.NewSize(l.Width+16+infoMinW, max(l.Height, r.Height))
}

func (detailsTop) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	l, r := objs[0], objs[1]
	lw := l.MinSize().Width
	l.Resize(fyne.NewSize(lw, l.MinSize().Height))
	l.Move(fyne.NewPos(0, 0))
	r.Resize(fyne.NewSize(max(size.Width-lw-16, infoMinW), size.Height))
	r.Move(fyne.NewPos(lw+16, 0))
}

func (d *detailsView) open(c modCard) {
	d.card, d.gb, d.ws = c, nil, nil
	d.seq++
	seq := d.seq
	d.name.SetText(c.Name)
	by := ""
	if c.Author != "" {
		by = "by " + c.Author
	}
	if c.Category != "" {
		by += " · " + c.Category
	}
	d.byline.SetText(by)
	d.setHero(c.Thumb, 460)
	d.strip.Objects = nil
	d.strip.Refresh()
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
				d.about.SetText("Couldn't load this mod's page: " + err.Error())
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
	bg.CornerRadius = 12
	d.hero.Objects = []fyne.CanvasObject{bg, img}
	d.hero.Refresh()
}

func (d *detailsView) fill() {
	if d.gb != nil {
		p := d.gb
		text := sources.HTMLText(p.Text)
		if text == "" {
			text = p.Description
		}
		if text == "" {
			text = "No description."
		}
		d.about.SetText(text)
		shots := p.Screenshots()
		if len(shots) > 0 {
			d.setHero(shots[0], 1120)
		}
		d.paintStrip(shots)
	} else if d.ws != nil {
		text := sources.BBText(d.ws.Description)
		if text == "" {
			text = "No description."
		}
		d.about.SetText(text)
		d.setHero(d.ws.Preview, 1120)
		d.paintStrip(nil)
	}
	d.paintStats()
	d.paintActions()
}

func (d *detailsView) paintStrip(shots []string) {
	d.strip.Objects = nil
	if len(shots) > 1 {
		for _, s := range shots {
			s := s
			img := newRemoteImage(s, 240, canvas.ImageFillCover)
			img.CornerRadius = 6
			d.strip.Add(newTappable(fixedSize(img, 112, 63), func() { d.setHero(s, 1120) }, nil))
		}
	}
	d.strip.Refresh()
}

func (d *detailsView) paintStats() {
	c := d.card
	var rows [][2]string
	switch {
	case d.gb != nil:
		rows = [][2]string{
			{"Downloads", count(d.gb.Downloads)}, {"Likes", count(d.gb.Likes)},
			{"Views", count(d.gb.Views)}, {"Updated", ago(c.Updated)},
		}
		if d.gb.Version != "" {
			rows = append(rows, [2]string{"Version", d.gb.Version})
		}
	case d.ws != nil:
		rows = [][2]string{
			{"Subscribers", count(d.ws.Subs)}, {"Favorites", count(d.ws.Favorited)},
			{"Views", count(d.ws.Views)}, {"Updated", ago(d.ws.Updated)},
			{"Size", size(atoi64(d.ws.Size))}, {"Created", time.Unix(d.ws.Created, 0).Format("Jan 2006")},
		}
	default:
		rows = [][2]string{{"Updated", ago(c.Updated)}}
	}
	d.stats.Objects = nil
	for _, r := range rows {
		d.stats.Add(container.NewVBox(mutedLabel(r[0]), widget.NewLabelWithStyle(r[1], fyne.TextAlignLeading, fyne.TextStyle{Bold: true})))
	}
	d.stats.Refresh()
}

// paintActions draws the buttons for what can be done with the mod now.
func (d *detailsView) paintActions() {
	u, c := d.b.u, d.card
	d.actions.Objects = nil
	key := u.opKey(c.Source, c.ID)
	switch {
	case c.IsMap:
		d.actions.Add(mutedLabel("This is a world map, not a mod. Download it from its page."))
		d.actions.Add(widget.NewButton("Open page", func() { u.openURL(c.URL) }))
	case c.Source == "workshop":
		if u.m.Subscribed()[c.ID] {
			on := widget.NewButtonWithIcon("Subscribed", theme.ConfirmIcon(), nil)
			on.Importance = widget.SuccessImportance
			off := widget.NewButtonWithIcon("Unsubscribe", theme.DeleteIcon(), func() { u.unsubscribe(c.ID, c.Name) })
			u.bindOp(key, off)
			d.actions.Add(container.NewGridWithColumns(2, on, off))
			d.actions.Add(mutedLabel("Steam keeps it up to date. It loads from Steam's Workshop folder."))
		} else {
			sub := widget.NewButtonWithIcon("Subscribe and download", theme.DownloadIcon(), func() { u.subscribe(c.ID, c.Name) })
			sub.Importance = widget.HighImportance
			u.bindOp(key, sub)
			d.actions.Add(sub)
			d.actions.Add(mutedLabel("wbmm subscribes through your Steam client and follows the download. Steam needs to be running."))
		}
	default:
		have := u.m.InstalledGB()[atoi(c.ID)]
		if d.gb != nil && len(d.gb.Files) > 0 {
			newest := d.gb.Files[0]
			lbl := "Install newest file"
			if have {
				lbl = "Reinstall newest file"
			}
			btn := widget.NewButtonWithIcon(lbl, theme.DownloadIcon(), func() { u.installGB(atoi(c.ID), newest, c.Name, false) })
			btn.Importance = widget.HighImportance
			u.bindOp(key, btn)
			d.actions.Add(btn)
		}
		if have {
			d.actions.Add(mutedLabel("Installed in your Mods folder. wbmm keeps it updated."))
		} else {
			d.actions.Add(mutedLabel("Downloads into your Mods folder after GameBanana's virus scan passed it."))
		}
	}
	d.actions.Refresh()
	d.paintFiles()
}

func (d *detailsView) paintFiles() {
	u, c := d.b.u, d.card
	d.files.Objects = nil
	if d.gb == nil || len(d.gb.Files) == 0 || c.IsMap {
		d.filesH.Hide()
		d.files.Refresh()
		return
	}
	d.filesH.Show()
	installed := u.m.InstalledFiles(atoi(c.ID))
	p := pal()
	for _, f := range d.gb.Files {
		f := f
		meta := fmt.Sprintf("%s · %s", size(f.Size), ago(f.Added))
		if f.Version != "" {
			meta += " · " + f.Version
		}
		scan := badge("virus scan passed", p.grass)
		if !f.Clean() {
			scan = badge("virus scan not passed", p.bad)
		}
		metaL := widget.NewLabel(meta) // not wrapped: it sits beside the badge
		metaL.SizeName, metaL.Importance = sSmall, widget.LowImportance
		info := container.NewVBox(
			widget.NewLabelWithStyle(f.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(metaL, container.NewCenter(scan)))
		if f.Description != "" {
			info.Add(mutedLabel(f.Description))
		}
		var btn *widget.Button
		if installed[f.ID] {
			btn = widget.NewButtonWithIcon("Installed", theme.ConfirmIcon(), func() { u.installGB(atoi(c.ID), f, c.Name, false) })
			btn.Importance = widget.LowImportance
		} else {
			btn = widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() { u.installGB(atoi(c.ID), f, c.Name, false) })
		}
		u.bindOp(u.opKey(c.Source, c.ID), btn)
		d.files.Add(card(container.NewBorder(nil, nil, nil, container.NewVBox(layout.NewSpacer(), btn, layout.NewSpacer()), info), 10))
	}
	d.files.Refresh()
}

func atoi64(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	return n
}
