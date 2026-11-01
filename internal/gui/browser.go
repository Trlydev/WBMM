package gui

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
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

// The sorts every searchable tab offers. Popularity has a second choice,
// the period; together they make the key "rank:<period>".
var sorts = []sortOption{{"rank", "Popularity"}, {"updated", "Recently updated"}, {"new", "Newest"}, {"relevance", "Relevance"}}
var periods = []sortOption{{popularity.Day, "Today"}, {popularity.Week, "This week"}, {popularity.Month, "This month"}, {popularity.AllTime, "All time"}}

// browserSource is one tab of a browser: where its items come from.
type browserSource struct {
	key, label string
	sorted     bool // offers the sorts (a list of what you have doesn't)
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

	srcBtns   []*widget.Button
	search    *widget.Entry
	sortSel   *widget.Select
	periodSel *widget.Select
	sortBox   fyne.CanvasObject
	periodBox fyne.CanvasObject
	note      *widget.Label
	list      *fyne.Container
	status    *widget.Label
	more      *widget.Button
	scroll    *container.Scroll
	details   *detailsView

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
	reload := func(string) {
		if !b.loading {
			b.load(true)
		}
	}
	b.sortSel = widget.NewSelect(labels(sorts), func(string) {
		if b.sortKey() == "" {
			return
		}
		if strings.HasPrefix(b.sortKey(), "rank:") {
			b.periodBox.Show()
		} else {
			b.periodBox.Hide()
		}
		reload("")
	})
	b.periodSel = widget.NewSelect(labels(periods), reload)
	b.note = mutedLabel("")
	b.list = container.NewVBox()
	b.status = label("")
	b.status.Alignment = fyne.TextAlignCenter
	b.more = widget.NewButton("Load more", func() { b.page++; b.load(false) })
	b.more.Hide()
	b.scroll = container.NewVScroll(container.NewVBox(b.list, b.status, container.NewCenter(b.more)))
	b.details = newDetailsView(u)

	b.loading = true
	b.periodSel.SetSelectedIndex(len(periods) - 1) // all time
	b.loading = false
	b.sortBox = fixedSize(b.sortSel, 170, 36)
	b.periodBox = fixedSize(b.periodSel, 130, 36)
	searchRow := container.NewBorder(nil, nil, nil, container.NewHBox(b.sortBox, b.periodBox), b.search)
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
	if s.sorted {
		b.sortSel.SetSelectedIndex(0) // popularity
		b.sortBox.Show()
		b.periodBox.Show()
	} else {
		b.sortBox.Hide()
		b.periodBox.Hide()
	}
	b.loading = false
	b.note.SetText(s.note)
	b.load(true)
}

// sortKey is what the fetchers get: "rank:<period>", "updated", "new" or
// "relevance"; "" for a tab without sorts.
func (b *browser) sortKey() string {
	i := b.sortSel.SelectedIndex()
	if !b.sources[b.src].sorted || i < 0 {
		return ""
	}
	k := sorts[i].key
	if k == "rank" {
		p := popularity.AllTime
		if j := b.periodSel.SelectedIndex(); j >= 0 {
			p = periods[j].key
		}
		k += ":" + p
	}
	return k
}

func labels(opts []sortOption) []string {
	var out []string
	for _, o := range opts {
		out = append(out, o.label)
	}
	return out
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
	gb, ws, maps := b.u.m.InstalledGB(), b.u.m.Subscribed(), b.u.m.InstalledMaps()
	rows := make([]fyne.CanvasObject, 0, len(b.cards))
	for _, c := range b.cards {
		have := c.Source == "gamebanana" && gb[atoi(c.ID)] || c.Source == "workshop" && ws[c.ID]
		if c.Kind == "map" {
			have = len(maps[atoi(c.ID)]) > 0
		}
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
	if c.Kind == "mod" && b.u.outdated(c.Updated) && b.u.release != nil {
		meta = append(meta, "before WorldBox "+b.u.release.Version)
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
	case c.Kind == "map" && have:
		action = badge("installed", p.grass)
	case c.Kind == "map":
		btn := widget.NewButton("Install", func() { b.u.installMap(atoi(c.ID), c.Name, false) })
		btn.Importance = widget.HighImportance
		b.u.bindOp(b.u.opKey(c.Source, c.ID), btn)
		action = btn
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

// Popularity ("rank:<period>") comes from wbmm's popularity tracker (counted
// the same way for every item, and shown on the card); the other sorts are
// the site's own.

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
	when := map[string]string{popularity.Day: "today", popularity.Week: "this week", popularity.Month: "this month"}[period]
	// Until the tracker has a week or a month of history, it says since when.
	if need := map[string]int{popularity.Week: 7, popularity.Month: 30}[period]; need > 0 && ix.Days() < need {
		if d, err := time.Parse("2006-01-02", ix.Since); err == nil {
			when = "since " + d.Format("Jan 2")
		}
	}
	for _, it := range items {
		c := modCard{Name: it.Name, Author: it.Author, Thumb: it.Image, URL: it.URL, Category: it.Category,
			Updated: it.Updated, Likes: it.Likes, Subs: it.Subs, Kind: it.Kind}
		c.Source, c.ID = "gamebanana", strings.TrimPrefix(it.Ref, "gb:")
		if src == "ws" {
			c.Source, c.ID = "workshop", strings.TrimPrefix(it.Ref, "ws:")
		}
		if when == "" {
			c.Stat = count(it.Gain(period)) + " " + unit
		} else {
			c.Stat = fmt.Sprintf("+%s %s %s · %s total", count(it.Gain(period)), unit, when, count(it.Gain(popularity.AllTime)))
		}
		cards = append(cards, c)
	}
	return cards, more, true
}

// siteSort is the site's own sort for a sort key. Popularity without the
// index is the site's (all time: most downloaded or subscribed; shorter:
// the Workshop's trending), and relevance needs words to match.
func siteSort(sort, q string, workshop bool) string {
	switch {
	case sort == "relevance" && strings.TrimSpace(q) != "":
		return sort
	case sort == "updated", sort == "new":
		return sort
	case workshop && sort != "rank:all" && sort != "relevance":
		return "popular"
	case workshop:
		return "subs"
	}
	return "popular"
}

// ranked reports whether a fetch should use the popularity index: sorted by
// popularity, or by relevance with nothing to search for.
func ranked(sort, q string) (string, bool) {
	if sort == "relevance" && strings.TrimSpace(q) == "" {
		return "rank:all", true
	}
	return sort, strings.HasPrefix(sort, "rank:")
}

// fetchGameBanana lists GameBanana mods, or with maps its world maps.
func fetchGameBanana(maps bool) func(*ui, string, string, int) ([]modCard, bool, error) {
	kind := "mod"
	if maps {
		kind = "map"
	}
	return func(_ *ui, q, sort string, page int) ([]modCard, bool, error) {
		if strings.Contains(q, "gamebanana.com/") {
			id, ok := sources.ParseGBRef(q)
			if !ok {
				return nil, false, errors.New("that isn't a GameBanana mod link")
			}
			m, err := sources.GBGetMod(id)
			if err != nil {
				return nil, false, err
			}
			return []modCard{gbCard(*m)}, false, nil
		}
		if rs, ok := ranked(sort, q); ok {
			if cards, more, ok := fetchRanked("gb", kind, q, rs, page); ok {
				return cards, more, nil
			}
		}
		mods, more, err := sources.GBList(q, siteSort(sort, q, false), maps, page)
		var cards []modCard
		for _, m := range mods {
			cards = append(cards, gbCard(m))
		}
		return cards, more, err
	}
}

func fetchWorkshop(tag string) func(*ui, string, string, int) ([]modCard, bool, error) {
	kind := "mod"
	if tag == sources.TagWorld {
		kind = "world"
	}
	return func(_ *ui, q, sort string, page int) ([]modCard, bool, error) {
		var items []sources.WSItem
		var more bool
		var err error
		if id, ok := sources.ParseWSRef(q); ok {
			items, err = sources.WSDetails([]string{id})
		} else {
			if rs, ok := ranked(sort, q); ok {
				if cards, more, ok := fetchRanked("ws", kind, q, rs, page); ok {
					return cards, more, nil
				}
			}
			items, more, err = sources.WSBrowse(q, siteSort(sort, q, true), tag, page)
		}
		var cards []modCard
		for _, it := range items {
			cards = append(cards, wsCard(it))
		}
		return cards, more, err
	}
}

// fetchInstalledWorlds lists the Workshop worlds Steam has downloaded,
// named and pictured from the Workshop where it still has them, and the
// GameBanana maps wbmm put into save slots.
func fetchInstalledWorlds(u *ui, q, _ string, _ int) ([]modCard, bool, error) {
	worlds, err := u.m.Worlds()
	if err != nil {
		return nil, false, err
	}
	var cards []modCard
	installed := map[string]int64{}
	for id, recs := range u.m.InstalledMaps() {
		r := recs[0]
		var slots []string
		for _, r := range recs {
			slots = append(slots, filepath.Base(r.Slot))
		}
		c := modCard{Source: "gamebanana", Kind: "map", ID: strconv.Itoa(id), Name: r.Name,
			Local: filepath.Join(r.Slot, "preview.png"), URL: fmt.Sprintf("https://gamebanana.com/mods/%d", id),
			Category: "GameBanana map", Stat: "in save slot " + strings.Join(slots, ", ")}
		if q == "" || strings.Contains(strings.ToLower(c.Name), strings.ToLower(q)) {
			cards = append(cards, c)
			installed[c.ID] = r.Installed.Unix()
		}
	}
	sort.Slice(cards, func(i, j int) bool { return installed[cards[i].ID] > installed[cards[j].ID] })
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
		{key: "gamebanana", label: "GameBanana", sorted: true, fetch: fetchGameBanana(false),
			note: "Installs into your Mods folder after a safety check. Worlds are on the Worlds page."},
		{key: "workshop", label: "Steam Workshop", sorted: true, fetch: fetchWorkshop(sources.TagMod),
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
		{key: "installed", label: "Installed", fetch: fetchInstalledWorlds,
			note: "Workshop worlds you're subscribed to and maps wbmm put into your saves. WorldBox lists them under Saves; they hold no code."},
		{key: "workshop", label: "Steam Workshop", sorted: true, fetch: fetchWorkshop(sources.TagWorld),
			note: "Worlds from the Steam Workshop. Subscribe and Steam downloads them."},
		{key: "gamebanana", label: "GameBanana", sorted: true, fetch: fetchGameBanana(true),
			note: "World maps from GameBanana. Install puts one into a free save slot."},
	})}
}

// reload refreshes the subscribed list (the browse tab keeps its results).
func (w *worldsPage) reload() {
	if w.sources[w.src].key == "installed" {
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
