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

	"github.com/Trlydev/WBMM/internal/popularity"
	"github.com/Trlydev/WBMM/internal/sources"
)

// modCard is one item of a browser list, from any source.
type modCard struct {
	Source   string // "gamebanana" or "workshop"
	Kind     string // "mod", "world" (a Workshop world) or "map" (a GameBanana map)
	ID       string
	Name     string
	Author   string
	Thumb    string // image URL
	Local    string // local image file, when there's no URL
	URL      string
	Category string
	Updated  int64
	Likes    int
	Views    int
	Subs     int
	// Stat is the number the list is sorted by, in words ("568k downloads",
	// "+340 subscribers this week"); empty: the source's own numbers.
	Stat string
}

func (c modCard) key() string { return c.Source + ":" + c.ID }

type sortOption struct{ key, label string }

// browserSource is one tab of a browser: where its items come from.
type browserSource struct {
	key, label string
	sorts      []sortOption
	note       string
	fetch      func(u *ui, query, sort string, page int) ([]modCard, bool, error)
}

// browser is a searchable list of mods or worlds with a details panel:
// the Get mods and Worlds pages.
type browser struct {
	u       *ui
	root    fyne.CanvasObject
	sources []browserSource
	src     int

	srcBtns []*widget.Button
	search  *widget.Entry
	sortSel *widget.Select
	sortBox fyne.CanvasObject
	note    *widget.Label
	list    *fyne.Container
	status  *widget.Label
	more    *widget.Button
	scroll  *container.Scroll
	details *detailsView

	cards    []modCard
	page     int
	seq      int
	selected string
	loading  bool
}

func newBrowser(u *ui, title string, srcs []browserSource) *browser {
	b := &browser{u: u, sources: srcs}
	seg := container.NewHBox()
	for i, s := range srcs {
		i := i
		btn := widget.NewButton(s.label, func() { b.setSource(i) })
		b.srcBtns = append(b.srcBtns, btn)
		seg.Add(btn)
	}
	b.search = widget.NewEntry()
	b.search.SetPlaceHolder("Search, or paste a link")
	b.search.OnSubmitted = func(string) { b.load(true) }
	b.sortSel = widget.NewSelect(nil, func(string) {
		if !b.loading {
			b.load(true)
		}
	})
	b.note = mutedLabel("")
	b.list = container.NewVBox()
	b.status = label("")
	b.status.Alignment = fyne.TextAlignCenter
	b.more = widget.NewButton("Load more", func() { b.page++; b.load(false) })
	b.more.Hide()
	b.scroll = container.NewVScroll(container.NewVBox(b.list, b.status, container.NewCenter(b.more)))
	b.details = newDetailsView(u)

	b.sortBox = fixedSize(b.sortSel, 180, 36)
	searchRow := container.NewBorder(nil, nil, nil, b.sortBox, b.search)
	left := container.NewBorder(container.NewVBox(searchRow, b.note), nil, nil, nil, b.scroll)
	split := container.NewHSplit(left, b.details.root)
	split.Offset = 0.56
	b.root = container.NewBorder(pageHeader(title, seg), nil, nil, nil, split)
	b.setSource(0)
	return b
}

func (b *browser) setSource(i int) {
	b.src = i
	s := b.sources[i]
	for j, btn := range b.srcBtns {
		if j == i {
			btn.Importance = widget.HighImportance
		} else {
			btn.Importance = widget.LowImportance
		}
		btn.Refresh()
	}
	b.loading = true
	var labels []string
	for _, o := range s.sorts {
		labels = append(labels, o.label)
	}
	b.sortSel.Options = labels
	if len(labels) > 0 {
		b.sortSel.SetSelectedIndex(0)
		b.sortBox.Show()
	} else {
		b.sortBox.Hide()
	}
	b.loading = false
	b.note.SetText(s.note)
	b.load(true)
}

func (b *browser) sortKey() string {
	s := b.sources[b.src]
	i := b.sortSel.SelectedIndex()
	if i < 0 || i >= len(s.sorts) {
		return ""
	}
	return s.sorts[i].key
}

// load fetches a page of items; reset starts over at page 1.
func (b *browser) load(reset bool) {
	if reset {
		b.page = 1
		b.cards = nil
		b.list.Objects = nil
		b.list.Refresh()
		b.scroll.ScrollToTop()
	}
	b.seq++
	seq, s, q, sort, page := b.seq, b.sources[b.src], strings.TrimSpace(b.search.Text), b.sortKey(), b.page
	b.status.SetText("Loading…")
	b.status.Show()
	b.more.Hide()
	go func() {
		cards, more, err := s.fetch(b.u, q, sort, page)
		fyne.Do(func() {
			if seq != b.seq {
				return
			}
			if err != nil {
				b.status.SetText("Couldn't load: " + err.Error())
				return
			}
			b.cards = append(b.cards, cards...)
			b.status.Hide()
			if len(b.cards) == 0 {
				b.status.SetText("Nothing found.")
				b.status.Show()
			}
			b.more.Hidden = !more
			b.repaint()
		})
	}()
}

// repaint rebuilds the rows (installed marks and selection change).
func (b *browser) repaint() {
	gb, ws := b.u.m.InstalledGB(), b.u.m.Subscribed()
	rows := make([]fyne.CanvasObject, 0, len(b.cards))
	for _, c := range b.cards {
		have := c.Source == "gamebanana" && gb[atoi(c.ID)] || c.Source == "workshop" && ws[c.ID]
		rows = append(rows, b.row(c, have))
	}
	b.list.Objects = rows
	b.list.Refresh()
	b.details.paintActions()
}

func (b *browser) row(c modCard, have bool) fyne.CanvasObject {
	p := pal()
	var img *canvas.Image
	if c.Thumb != "" {
		img = newRemoteImage(c.Thumb, 240, canvas.ImageFillCover)
	} else {
		img = localImage(c.Local, 240)
	}
	img.CornerRadius = 6
	bg := canvas.NewRectangle(p.surface2)
	bg.CornerRadius = 6
	thumb := fixedSize(container.NewStack(bg, img), 104, 58)

	var meta []string
	if c.Author != "" {
		meta = append(meta, c.Author)
	}
	if c.Category != "" {
		meta = append(meta, c.Category)
	}
	if u := ago(c.Updated); u != "" {
		meta = append(meta, u)
	}
	stats := c.Stat
	switch {
	case stats != "":
	case c.Source == "workshop":
		stats = count(c.Subs) + " subscribers"
	default:
		stats = fmt.Sprintf("%s likes · %s views", count(c.Likes), count(c.Views))
	}
	text := tight(
		line(c.Name, true, theme.SizeNameText, widget.MediumImportance),
		line(strings.Join(meta, " · "), false, sSmall, widget.LowImportance),
		line(stats, false, sSmall, widget.LowImportance))

	var action fyne.CanvasObject
	switch {
	case c.Kind == "map":
		action = badge("map", p.sea)
	case have && c.Source == "workshop":
		action = badge("subscribed", p.grass)
	case have:
		action = badge("installed", p.grass)
	default:
		label, act := "Install", func() { b.u.installGBNewest(c) }
		if c.Source == "workshop" {
			label, act = "Subscribe", func() { b.u.subscribe(c.ID, c.Name) }
		}
		btn := widget.NewButton(label, act)
		btn.Importance = widget.HighImportance
		b.u.bindOp(b.u.opKey(c.Source, c.ID), btn)
		action = btn
	}
	content := container.NewBorder(nil, nil, thumb, container.NewCenter(action),
		container.NewVBox(layout.NewSpacer(), text, layout.NewSpacer()))
	return listRow(content, c.key() == b.selected, func() { b.open(c) })
}

// open selects an item and shows it in the details panel.
func (b *browser) open(c modCard) {
	b.selected = c.key()
	b.details.open(c)
	b.repaint()
}

// ---------- sources ----------

// Sorts by "rank:<period>" come from wbmm's popularity tracker (counted
// the same way for every item, and shown on the card); the rest are the
// site's own.
var gbSorts = []sortOption{{"rank:all", "Most downloaded"}, {"rank:week", "Trending this week"}, {"rank:month", "Trending this month"},
	{"new", "Newest"}, {"updated", "Recently updated"}, {"liked", "Most liked"}}
var wsSorts = []sortOption{{"rank:all", "Most subscribed"}, {"rank:week", "Trending this week"}, {"rank:month", "Trending this month"},
	{"new", "Newest"}, {"updated", "Recently updated"}}

const pageSize = 30

// fetchRanked lists items from the popularity index. ok is false when there
// is no index (offline, or not published yet): the caller asks the site.
func fetchRanked(src, kind, q, sort string, page int) (cards []modCard, more, ok bool) {
	period, isRank := strings.CutPrefix(sort, "rank:")
	if !isRank {
		return nil, false, false
	}
	ix, err := popularity.Fetch()
	if err != nil {
		return nil, false, false
	}
	items, more := ix.Query(src, kind, q, period, (page-1)*pageSize, pageSize)
	unit := "downloads"
	if src == "ws" {
		unit = "subscribers"
	}
	for _, it := range items {
		c := modCard{Name: it.Name, Author: it.Author, Thumb: it.Image, URL: it.URL, Category: it.Category,
			Updated: it.Updated, Likes: it.Likes, Subs: it.Subs, Kind: it.Kind}
		c.Source, c.ID = "gamebanana", strings.TrimPrefix(it.Ref, "gb:")
		if src == "ws" {
			c.Source, c.ID = "workshop", strings.TrimPrefix(it.Ref, "ws:")
		}
		switch period {
		case popularity.AllTime:
			c.Stat = count(it.Gain(period)) + " " + unit
		default:
			c.Stat = fmt.Sprintf("+%s %s this %s · %s total", count(it.Gain(period)), unit, period, count(it.Gain(popularity.AllTime)))
		}
		cards = append(cards, c)
	}
	return cards, more, true
}

// sortFallback is the site's own sort to use when the index is missing.
func sortFallback(sort string, workshop bool) string {
	if !strings.HasPrefix(sort, "rank:") {
		return sort
	}
	switch {
	case workshop && sort == "rank:all":
		return "subs"
	case workshop:
		return "popular"
	}
	return "popular"
}

func fetchGameBanana(_ *ui, q, sort string, page int) ([]modCard, bool, error) {
	if !strings.Contains(q, "gamebanana.com/") {
		if cards, more, ok := fetchRanked("gb", "", q, sort, page); ok {
			return cards, more, nil
		}
	}
	sort = sortFallback(sort, false)
	if strings.Contains(q, "gamebanana.com/mods/") {
		id, _ := sources.ParseGBRef(q)
		m, err := sources.GBGetMod(id)
		if err != nil {
			return nil, false, err
		}
		return []modCard{gbCard(*m)}, false, nil
	}
	mods, more, err := sources.GBList(q, sort, page)
	var cards []modCard
	for _, m := range mods {
		cards = append(cards, gbCard(m))
	}
	return cards, more, err
}

func fetchWorkshop(tag string) func(*ui, string, string, int) ([]modCard, bool, error) {
	kind := "mod"
	if tag == sources.TagWorld {
		kind = "world"
	}
	return func(_ *ui, q, sort string, page int) ([]modCard, bool, error) {
		if _, isLink := sources.ParseWSRef(q); !isLink {
			if cards, more, ok := fetchRanked("ws", kind, q, sort, page); ok {
				return cards, more, nil
			}
		}
		sort = sortFallback(sort, true)
		var items []sources.WSItem
		var more bool
		var err error
		if id, ok := sources.ParseWSRef(q); ok {
			items, err = sources.WSDetails([]string{id})
		} else {
			items, more, err = sources.WSBrowse(q, sort, tag, page)
		}
		var cards []modCard
		for _, it := range items {
			cards = append(cards, wsCard(it))
		}
		return cards, more, err
	}
}

// fetchSubscribedWorlds lists the Workshop worlds Steam has downloaded,
// named and pictured from the Workshop where it still has them.
func fetchSubscribedWorlds(u *ui, q, _ string, _ int) ([]modCard, bool, error) {
	worlds, err := u.m.Worlds()
	if err != nil {
		return nil, false, err
	}
	var ids []string
	for _, w := range worlds {
		ids = append(ids, w.WorkshopID)
	}
	byID := map[string]sources.WSItem{}
	if items, err := sources.WSDetails(ids); err == nil {
		for _, it := range items {
			byID[it.ID] = it
		}
	}
	var cards []modCard
	for _, w := range worlds {
		c := modCard{Source: "workshop", Kind: "world", ID: w.WorkshopID, Name: w.Name, Local: w.Preview,
			URL: "https://steamcommunity.com/sharedfiles/filedetails/?id=" + w.WorkshopID, Updated: w.Updated}
		if it, ok := byID[w.WorkshopID]; ok {
			c = wsCard(it)
		}
		if c.Name == "" {
			c.Name = "World " + w.WorkshopID
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(q)) {
			continue
		}
		cards = append(cards, c)
	}
	return cards, false, nil
}

func gbCard(m sources.GBMod) modCard {
	upd := m.Updated
	if upd == 0 {
		upd = m.Modified
	}
	kind := "mod"
	if m.IsMap() {
		kind = "map"
	}
	return modCard{
		Source: "gamebanana", Kind: kind, ID: strconv.Itoa(m.ID), Name: m.Name, Author: m.Submitter.Name,
		Thumb: m.Thumb(), URL: m.URL, Category: m.Category.Name, Updated: upd,
		Likes: m.Likes, Views: m.Views,
	}
}

func wsCard(it sources.WSItem) modCard {
	kind := "mod"
	if it.HasTag(sources.TagWorld) && !it.HasTag(sources.TagMod) {
		kind = "world"
	}
	return modCard{
		Source: "workshop", Kind: kind, ID: it.ID, Name: it.Title, Thumb: it.Preview, URL: it.URL(),
		Updated: it.Updated, Subs: it.Subs, Views: it.Views,
	}
}

// ---------- the two browser pages ----------

type getModsPage struct{ *browser }

func newGetModsPage(u *ui) *getModsPage {
	return &getModsPage{newBrowser(u, "Get mods", []browserSource{
		{key: "gamebanana", label: "GameBanana", sorts: gbSorts, fetch: fetchGameBanana,
			note: "Installs into your Mods folder after a safety check."},
		{key: "workshop", label: "Steam Workshop", sorts: wsSorts, fetch: fetchWorkshop(sources.TagMod),
			note: "wbmm subscribes through Steam, follows the download and checks the mod before the game loads it."},
	})}
}

// open shows a mod from elsewhere (My mods) in this page's details panel.
func (g *getModsPage) open(c modCard) {
	want := 0
	if c.Source == "workshop" {
		want = 1
	}
	if g.src != want {
		g.setSource(want)
	}
	g.browser.open(c)
}

type worldsPage struct{ *browser }

func newWorldsPage(u *ui) *worldsPage {
	return &worldsPage{newBrowser(u, "Worlds", []browserSource{
		{key: "subscribed", label: "Subscribed", fetch: fetchSubscribedWorlds,
			note: "Worlds you're subscribed to. WorldBox lists them in its saves menu; they hold no code."},
		{key: "workshop", label: "Browse Workshop", sorts: wsSorts, fetch: fetchWorkshop(sources.TagWorld),
			note: "Worlds from the Steam Workshop. Subscribe and Steam downloads them."},
	})}
}

// reload refreshes the subscribed list (the browse tab keeps its results).
func (w *worldsPage) reload() {
	if w.sources[w.src].key == "subscribed" {
		w.load(true)
	} else {
		w.repaint()
	}
}

// ---------- formatting ----------

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
