package gui

import (
	"fmt"
	"strconv"
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

// modCard is one browse result, from either source.
type modCard struct {
	Source   string // "gamebanana" or "workshop"
	ID       string
	Name     string
	Author   string
	Thumb    string
	URL      string
	Category string
	Updated  int64
	Likes    int
	Views    int
	Subs     int
	IsMap    bool
}

type sortOption struct{ key, label string }

var sorts = map[string][]sortOption{
	"gamebanana": {{"popular", "Most downloaded"}, {"new", "Newest"}, {"updated", "Recently updated"}, {"liked", "Most liked"}},
	"workshop":   {{"subs", "Most subscribed"}, {"popular", "Trending this week"}, {"new", "Newest"}, {"updated", "Recently updated"}},
}

type browseView struct {
	u    *ui
	root *fyne.Container // the list or a preview page

	list    fyne.CanvasObject
	details *detailsView

	src     string
	srcBtns map[string]*widget.Button
	search  *widget.Entry
	sortSel *widget.Select
	note    *widget.Label
	grid    *fyne.Container
	status  *widget.Label
	more    *widget.Button
	scroll  *container.Scroll

	cards []modCard
	page  int
	seq   int
}

func newBrowseView(u *ui) *browseView {
	b := &browseView{u: u, srcBtns: map[string]*widget.Button{}}
	for _, s := range []struct{ key, label string }{{"gamebanana", "GameBanana"}, {"workshop", "Steam Workshop"}} {
		key := s.key
		b.srcBtns[key] = widget.NewButton(s.label, func() { b.setSource(key) })
	}
	b.search = widget.NewEntry()
	b.search.SetPlaceHolder("Search mods, or paste a GameBanana or Workshop link")
	b.search.OnSubmitted = func(string) { b.load(true) }
	b.sortSel = widget.NewSelect(nil, func(string) {
		if b.cards != nil || b.page > 0 {
			b.load(true)
		}
	})
	go_ := widget.NewButtonWithIcon("Search", theme.SearchIcon(), func() { b.load(true) })
	go_.Importance = widget.HighImportance

	toolbar := container.NewBorder(nil, nil,
		container.NewHBox(b.srcBtns["gamebanana"], b.srcBtns["workshop"]),
		container.NewHBox(fixedSize(b.sortSel, 190, 36), go_),
		b.search)
	b.note = mutedLabel("")
	b.grid = container.New(&gridLayout{minW: 228, gap: 14})
	b.status = label("")
	b.status.Alignment = fyne.TextAlignCenter
	b.more = widget.NewButton("Load more", func() { b.page++; b.load(false) })
	b.more.Hide()
	b.scroll = container.NewVScroll(container.NewVBox(b.grid, b.status, container.NewCenter(b.more), layout.NewSpacer()))
	b.list = container.NewBorder(container.NewVBox(toolbar, b.note), nil, nil, nil, b.scroll)
	b.details = newDetailsView(b)
	b.details.root.Hide()
	b.root = container.NewStack(b.list, b.details.root)
	b.setSource("gamebanana")
	return b
}

func (b *browseView) setSource(src string) {
	b.src = src
	for k, btn := range b.srcBtns {
		if k == src {
			btn.Importance = widget.HighImportance
		} else {
			btn.Importance = widget.MediumImportance
		}
		btn.Refresh()
	}
	var labels []string
	for _, o := range sorts[src] {
		labels = append(labels, o.label)
	}
	b.page = 0
	b.cards = nil
	b.sortSel.Options = labels
	b.sortSel.SetSelectedIndex(0) // also loads
	if src == "gamebanana" {
		b.note.SetText("GameBanana mods download straight into your Mods folder. Click a mod to see it first.")
	} else {
		b.note.SetText("Workshop mods are downloaded by Steam: wbmm subscribes for you and follows the download. Click a mod to see it first.")
	}
	b.load(true)
}

func (b *browseView) sortKey() string {
	i := b.sortSel.SelectedIndex()
	if i < 0 {
		i = 0
	}
	return sorts[b.src][i].key
}

// load fetches a page of results; reset starts over at page 1.
func (b *browseView) load(reset bool) {
	q := strings.TrimSpace(b.search.Text)
	// Links go to the source they belong to.
	if _, ok := sources.ParseWSRef(q); ok && b.src != "workshop" {
		b.setSource("workshop")
		return
	}
	if strings.Contains(q, "gamebanana.com/mods/") && b.src != "gamebanana" {
		b.setSource("gamebanana")
		return
	}
	if reset {
		b.page = 1
		b.cards = nil
		b.grid.Objects = nil
		b.grid.Refresh()
		b.scroll.ScrollToTop()
	}
	b.seq++
	seq, src, sort, page := b.seq, b.src, b.sortKey(), b.page
	b.status.SetText("Loading…")
	b.status.Show()
	b.more.Hide()
	go func() {
		cards, more, err := fetchCards(src, q, sort, page)
		fyne.Do(func() {
			if seq != b.seq {
				return
			}
			if err != nil {
				b.status.SetText("Couldn't load mods: " + err.Error())
				return
			}
			b.cards = append(b.cards, cards...)
			b.status.Hide()
			if len(b.cards) == 0 {
				b.status.SetText("No mods found.")
				b.status.Show()
			}
			b.more.Hidden = !more
			b.repaint()
		})
	}()
}

func fetchCards(src, q, sort string, page int) ([]modCard, bool, error) {
	var cards []modCard
	if src == "workshop" {
		var items []sources.WSItem
		var more bool
		var err error
		if id, ok := sources.ParseWSRef(q); ok {
			items, err = sources.WSDetails([]string{id})
		} else {
			items, more, err = sources.WSBrowse(q, sort, page)
		}
		for _, it := range items {
			cards = append(cards, wsCard(it))
		}
		return cards, more, err
	}
	if strings.Contains(q, "gamebanana.com/mods/") {
		id, _ := sources.ParseGBRef(q)
		m, err := sources.GBGetMod(id)
		if err != nil {
			return nil, false, err
		}
		return []modCard{gbCard(*m)}, false, nil
	}
	mods, more, err := sources.GBList(q, sort, page)
	for _, m := range mods {
		cards = append(cards, gbCard(m))
	}
	return cards, more, err
}

func gbCard(m sources.GBMod) modCard {
	upd := m.Updated
	if upd == 0 {
		upd = m.Modified
	}
	return modCard{
		Source: "gamebanana", ID: strconv.Itoa(m.ID), Name: m.Name, Author: m.Submitter.Name,
		Thumb: m.Thumb(), URL: m.URL, Category: m.Category.Name, Updated: upd,
		Likes: m.Likes, Views: m.Views, IsMap: m.IsMap(),
	}
}

func wsCard(it sources.WSItem) modCard {
	return modCard{
		Source: "workshop", ID: it.ID, Name: it.Title, Thumb: it.Preview, URL: it.URL(),
		Updated: it.Updated, Subs: it.Subs, Views: it.Views, IsMap: !it.HasTag("Mod"),
	}
}

// repaint rebuilds the cards (installed marks change) and the open
// preview page.
func (b *browseView) repaint() {
	gb, ws := b.u.m.InstalledGB(), b.u.m.Subscribed()
	objs := make([]fyne.CanvasObject, 0, len(b.cards))
	for _, c := range b.cards {
		have := c.Source == "gamebanana" && gb[atoi(c.ID)] || c.Source == "workshop" && ws[c.ID]
		objs = append(objs, b.cardFor(c, have))
	}
	b.grid.Objects = objs
	b.grid.Refresh()
	if b.details.root.Visible() {
		b.details.paintActions()
	}
}

func (b *browseView) cardFor(c modCard, have bool) fyne.CanvasObject {
	p := pal()
	img := newRemoteImage(c.Thumb, 460, canvas.ImageFillCover)
	img.CornerRadius = 10
	imgBg := canvas.NewRectangle(p.surface2)
	imgBg.CornerRadius = 10
	var tag fyne.CanvasObject = layout.NewSpacer()
	switch {
	case c.IsMap:
		tag = badge("World map", p.sea)
	case have && c.Source == "workshop":
		tag = badge("Subscribed", p.grass)
	case have:
		tag = badge("Installed", p.grass)
	}
	imgBox := fixedSize(container.NewStack(imgBg, img,
		container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), container.NewVBox(container.NewHBox(tag)))), 228, 132)

	name := widget.NewLabelWithStyle(c.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	name.Truncation = fyne.TextTruncateEllipsis
	var meta string
	if c.Author != "" {
		meta = "by " + c.Author + " · "
	}
	meta += ago(c.Updated)
	metaL := widget.NewLabel(meta)
	metaL.Truncation = fyne.TextTruncateEllipsis
	metaL.SizeName = sSmall
	metaL.Importance = widget.LowImportance
	var stats string
	if c.Source == "workshop" {
		stats = count(c.Subs) + " subscribers"
	} else {
		stats = fmt.Sprintf("%s likes · %s views", count(c.Likes), count(c.Views))
		if c.Category != "" {
			stats = c.Category + " · " + stats
		}
	}
	statsL := widget.NewLabel(stats)
	statsL.Truncation = fyne.TextTruncateEllipsis
	statsL.SizeName = sSmall
	statsL.Importance = widget.LowImportance

	open := func() { b.openDetails(c) }
	var btn *widget.Button
	switch {
	case c.IsMap:
		btn = widget.NewButton("Open page", func() { b.u.openURL(c.URL) })
	case have:
		label := "Installed"
		if c.Source == "workshop" {
			label = "Subscribed"
		}
		btn = widget.NewButtonWithIcon(label, theme.ConfirmIcon(), open)
		btn.Importance = widget.LowImportance
	case c.Source == "workshop":
		btn = widget.NewButtonWithIcon("Subscribe", theme.DownloadIcon(), func() { b.u.subscribe(c.ID, c.Name) })
		btn.Importance = widget.HighImportance
	default:
		btn = widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() { b.u.installGBNewest(c) })
		btn.Importance = widget.HighImportance
	}
	b.u.bindOp(b.u.opKey(c.Source, c.ID), btn)

	body := container.New(layout.NewCustomPaddedLayout(0, 8, 6, 6), container.NewVBox(name, metaL, statsL, btn))
	content := container.NewBorder(imgBox, nil, nil, nil, body)
	bg := canvas.NewRectangle(p.surface)
	bg.CornerRadius = 12
	bg.StrokeColor = p.border
	bg.StrokeWidth = 1
	return newTappable(container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(6, 6, 6, 6), content)), open, func(in bool) {
		if in {
			bg.StrokeColor = p.grass
			bg.StrokeWidth = 2
		} else {
			bg.StrokeColor = p.border
			bg.StrokeWidth = 1
		}
		bg.Refresh()
	})
}

func (b *browseView) openDetails(c modCard) {
	b.details.open(c)
	b.list.Hide()
	b.details.root.Show()
}

func (b *browseView) closeDetails() {
	b.details.root.Hide()
	b.list.Show()
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func ago(ts int64) string {
	if ts == 0 {
		return ""
	}
	d := time.Since(time.Unix(ts, 0)).Hours() / 24
	switch {
	case d < 1:
		return "today"
	case d < 2:
		return "yesterday"
	case d < 31:
		return fmt.Sprintf("%d days ago", int(d))
	case d < 365:
		return fmt.Sprintf("%d months ago", int(d/30))
	case d < 730:
		return "a year ago"
	}
	return fmt.Sprintf("%d years ago", int(d/365))
}
