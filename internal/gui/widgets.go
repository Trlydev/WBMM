package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---------- on/off switch ----------

// toggle is an on/off switch, the control a mod list is made of.
type toggle struct {
	widget.BaseWidget
	On        bool
	Disabled  bool
	OnChanged func(bool)
	track     *canvas.Rectangle
	knob      *canvas.Circle
}

func newToggle(on bool, changed func(bool)) *toggle {
	t := &toggle{On: on, OnChanged: changed}
	t.ExtendBaseWidget(t)
	return t
}

func (t *toggle) Tapped(*fyne.PointEvent) {
	if t.Disabled {
		return
	}
	t.On = !t.On
	t.Refresh()
	if t.OnChanged != nil {
		t.OnChanged(t.On)
	}
}

// Set changes the switch without calling OnChanged.
func (t *toggle) Set(on bool) { t.On = on; t.Refresh() }

func (t *toggle) Cursor() desktop.Cursor {
	if t.Disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (t *toggle) MinSize() fyne.Size { return fyne.NewSize(40, 22) }

func (t *toggle) CreateRenderer() fyne.WidgetRenderer {
	t.track = canvas.NewRectangle(color.Transparent)
	t.track.CornerRadius = 11
	t.knob = canvas.NewCircle(color.White)
	return &toggleRenderer{t}
}

type toggleRenderer struct{ t *toggle }

func (r *toggleRenderer) Layout(size fyne.Size) {
	t := r.t
	h := float32(22)
	t.track.Resize(fyne.NewSize(40, h))
	t.track.Move(fyne.NewPos(0, (size.Height-h)/2))
	k := h - 6
	x := float32(3)
	if t.On {
		x = 40 - k - 3
	}
	t.knob.Resize(fyne.NewSize(k, k))
	t.knob.Move(fyne.NewPos(x, (size.Height-k)/2))
}

func (r *toggleRenderer) MinSize() fyne.Size { return r.t.MinSize() }

func (r *toggleRenderer) Refresh() {
	p := pal()
	t := r.t
	switch {
	case t.Disabled:
		t.track.FillColor = withAlpha(p.muted, 0x55)
	case t.On:
		t.track.FillColor = p.grass
	default:
		t.track.FillColor = p.border
	}
	t.knob.FillColor = color.White
	if t.Disabled {
		t.knob.FillColor = withAlpha(color.NRGBA{0xff, 0xff, 0xff, 0xff}, 0x99)
	}
	r.Layout(t.Size())
	t.track.Refresh()
	t.knob.Refresh()
}

func (r *toggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.t.track, r.t.knob}
}
func (r *toggleRenderer) Destroy() {}

// ---------- sidebar ----------

// navItem is one sidebar entry: an icon, a name, an optional count.
type navItem struct {
	widget.BaseWidget
	icon   fyne.Resource
	name   string
	count  string
	active bool
	onTap  func()
}

func newNavItem(icon fyne.Resource, name string, onTap func()) *navItem {
	n := &navItem{icon: icon, name: name, onTap: onTap}
	n.ExtendBaseWidget(n)
	return n
}

func (n *navItem) Tapped(*fyne.PointEvent) { n.onTap() }
func (n *navItem) Cursor() desktop.Cursor  { return desktop.PointerCursor }
func (n *navItem) setActive(on bool)       { n.active = on; n.Refresh() }
func (n *navItem) setCount(s string)       { n.count = s; n.Refresh() }

func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 8
	bar := canvas.NewRectangle(color.Transparent)
	bar.CornerRadius = 2
	ic := widget.NewIcon(n.icon)
	name := text(n.name, pal().text, theme.SizeNameText, false)
	cnt := text("", pal().muted, sSmall, true)
	row := container.New(layout.NewCustomPaddedLayout(8, 8, 12, 12),
		container.NewBorder(nil, nil, container.NewHBox(fixedSize(ic, 18, 18), name), cnt))
	r := &navRenderer{n: n, bg: bg, bar: bar, name: name, cnt: cnt, row: row}
	r.Refresh()
	return r
}

type navRenderer struct {
	n         *navItem
	bg, bar   *canvas.Rectangle
	name, cnt *canvas.Text
	row       *fyne.Container
}

func (r *navRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bar.Resize(fyne.NewSize(3, size.Height-16))
	r.bar.Move(fyne.NewPos(0, 8))
	r.row.Resize(size)
}
func (r *navRenderer) MinSize() fyne.Size { return r.row.MinSize() }
func (r *navRenderer) Refresh() {
	p := pal()
	r.name.Text, r.cnt.Text = r.n.name, r.n.count
	r.name.TextStyle.Bold = r.n.active
	if r.n.active {
		r.bg.FillColor = withAlpha(p.grass, 0x24)
		r.bar.FillColor = p.grass
		r.name.Color = p.text
	} else {
		r.bg.FillColor = color.Transparent
		r.bar.FillColor = color.Transparent
		r.name.Color = p.muted
	}
	r.bg.Refresh()
	r.bar.Refresh()
	r.name.Refresh()
	r.cnt.Refresh()
}
func (r *navRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.bar, r.row}
}
func (r *navRenderer) Destroy() {}

// ---------- list rows ----------

// listRow is a clickable row of a list, highlighted when selected.
func listRow(content fyne.CanvasObject, selected bool, onTap func()) fyne.CanvasObject {
	p := pal()
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 8
	if selected {
		bg.FillColor = withAlpha(p.grass, 0x26)
		bg.StrokeColor = withAlpha(p.grass, 0x99)
		bg.StrokeWidth = 1
	}
	padded := container.New(layout.NewCustomPaddedLayout(6, 6, 8, 8), content)
	return newTappable(container.NewStack(bg, padded), onTap, func(in bool) {
		if selected {
			return
		}
		if in {
			bg.FillColor = withAlpha(p.text, 0x0c)
		} else {
			bg.FillColor = color.Transparent
		}
		bg.Refresh()
	})
}

// line is a one-line label that shortens itself with "…".
func line(s string, bold bool, size fyne.ThemeSizeName, imp widget.Importance) *widget.Label {
	l := widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: bold})
	l.Truncation = fyne.TextTruncateEllipsis
	l.SizeName = size
	l.Importance = imp
	return l
}

// tight stacks lines with less space than labels leave on their own.
func tight(objs ...fyne.CanvasObject) fyne.CanvasObject {
	return container.New(&tightLayout{}, objs...)
}

type tightLayout struct{}

const tightOverlap = 12 // labels pad themselves by about this much

func (tightLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		w = max(w, s.Width)
		h += s.Height
		n++
	}
	if n > 1 {
		h -= tightOverlap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

func (tightLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	var y float32
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Resize(fyne.NewSize(size.Width, h))
		o.Move(fyne.NewPos(0, y))
		y += h - tightOverlap
	}
}

// placeholder is the details panel when nothing is selected.
func placeholder(s string) fyne.CanvasObject {
	l := widget.NewLabel(s)
	l.Alignment = fyne.TextAlignCenter
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return container.NewCenter(maxWidth(l, 280))
}

// pageHeader is a page's title row with controls on the right.
func pageHeader(title string, right ...fyne.CanvasObject) fyne.CanvasObject {
	t := text(title, pal().text, sHuge, true)
	return container.New(layout.NewCustomPaddedLayout(4, 6, 0, 0),
		container.NewBorder(nil, nil, container.NewCenter(t), container.NewHBox(right...)))
}
