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

// WorldBox colours: grass, sea, sand and night sky, light and dark.

type palette struct {
	bg, surface, surface2, border, text, muted    color.NRGBA
	grass, grassInk, sea, sand, warn, bad         color.NRGBA
	headerFrom, headerTo, headerText, headerMuted color.NRGBA
}

func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{uint8(hex >> 16), uint8(hex >> 8), uint8(hex), 0xff}
}

var darkPal = palette{
	bg: rgb(0x10161d), surface: rgb(0x18212b), surface2: rgb(0x212c38), border: rgb(0x2c3a48),
	text: rgb(0xe7eef5), muted: rgb(0x8fa1b3),
	grass: rgb(0x74c043), grassInk: rgb(0x0c1a05), sea: rgb(0x4aa3e0), sand: rgb(0xe2b94b),
	warn: rgb(0xe8a33d), bad: rgb(0xe5534b),
	headerFrom: rgb(0x1f4d2b), headerTo: rgb(0x1b3f5e), headerText: rgb(0xf2f7ee), headerMuted: rgb(0xb9cfc0),
}

var lightPal = palette{
	bg: rgb(0xeef3ec), surface: rgb(0xffffff), surface2: rgb(0xe4ece2), border: rgb(0xcfdacb),
	text: rgb(0x17211a), muted: rgb(0x5b6b5e),
	grass: rgb(0x3f8f1d), grassInk: rgb(0xffffff), sea: rgb(0x1f6fc0), sand: rgb(0xa77a08),
	warn: rgb(0xa86500), bad: rgb(0xc4302b),
	headerFrom: rgb(0x2f6b35), headerTo: rgb(0x245a85), headerText: rgb(0xffffff), headerMuted: rgb(0xd8e8dc),
}

// variantFor is the variant shown: the system's, unless a test sets one.
var forceVariant = -1

func variantFor(v fyne.ThemeVariant) fyne.ThemeVariant {
	if forceVariant >= 0 {
		return fyne.ThemeVariant(forceVariant)
	}
	return v
}

func pal() *palette {
	if variantFor(fyne.CurrentApp().Settings().ThemeVariant()) == theme.VariantLight {
		return &lightPal
	}
	return &darkPal
}

const (
	sSmall fyne.ThemeSizeName = "wbmm.small"
	sTitle fyne.ThemeSizeName = "wbmm.title"
	sHuge  fyne.ThemeSizeName = "wbmm.huge"
)

type wbTheme struct{}

func (wbTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := &darkPal
	if variantFor(v) == theme.VariantLight {
		p = &lightPal
	}
	switch n {
	case theme.ColorNameBackground:
		return p.bg
	case theme.ColorNameForeground:
		return p.text
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return p.grass
	case theme.ColorNameForegroundOnPrimary:
		return p.grassInk
	case theme.ColorNameButton:
		return p.surface2
	case theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return p.surface
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return p.border
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return p.muted
	case theme.ColorNameHover:
		return withAlpha(p.grass, 0x22)
	case theme.ColorNamePressed:
		return withAlpha(p.grass, 0x44)
	case theme.ColorNameSelection:
		return withAlpha(p.sea, 0x55)
	case theme.ColorNameHyperlink:
		return p.sea
	case theme.ColorNameWarning:
		return p.warn
	case theme.ColorNameError:
		return p.bad
	case theme.ColorNameSuccess:
		return p.grass
	case theme.ColorNameScrollBar:
		return withAlpha(p.muted, 0x88)
	}
	return theme.DefaultTheme().Color(n, v)
}

func (wbTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (wbTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (wbTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14
	case sSmall, theme.SizeNameCaptionText:
		return 12
	case sTitle, theme.SizeNameSubHeadingText:
		return 17
	case sHuge, theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 10
	case theme.SizeNameScrollBar:
		return 8
	}
	return theme.DefaultTheme().Size(n)
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// ---------- building blocks ----------

// text is a single-line canvas text in a theme size.
func text(s string, c color.Color, size fyne.ThemeSizeName, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = theme.Size(size)
	t.TextStyle.Bold = bold
	return t
}

func label(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	return l
}

func mutedLabel(s string) *widget.Label {
	l := label(s)
	l.Importance = widget.LowImportance
	l.SizeName = sSmall
	return l
}

func title(s string) *widget.Label {
	l := widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = sTitle
	l.Wrapping = fyne.TextWrapWord
	return l
}

// card is a rounded, bordered surface.
func card(content fyne.CanvasObject, padding float32) *fyne.Container {
	bg := canvas.NewRectangle(pal().surface)
	bg.CornerRadius = 12
	bg.StrokeColor = pal().border
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(padding, padding, padding, padding), content))
}

// tint is a borderless coloured box (banners, notes).
func tint(content fyne.CanvasObject, fill color.Color) *fyne.Container {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = 10
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(8, 8, 12, 12), content))
}

// badge is a small rounded tag.
func badge(s string, fg color.NRGBA) fyne.CanvasObject {
	t := text(s, fg, sSmall, true)
	bg := canvas.NewRectangle(withAlpha(fg, 0x2a))
	bg.CornerRadius = 9
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(2, 2, 8, 8), t))
}

// fixedSize gives an object an exact minimum size.
func fixedSize(o fyne.CanvasObject, w, h float32) fyne.CanvasObject {
	return container.New(&sizeLayout{fyne.NewSize(w, h)}, o)
}

type sizeLayout struct{ s fyne.Size }

func (l *sizeLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.s }
func (l *sizeLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

// maxWidth centres content and stops it growing wider than w.
func maxWidth(o fyne.CanvasObject, w float32) fyne.CanvasObject {
	return container.New(&widthLayout{w}, o)
}

type widthLayout struct{ w float32 }

func (l *widthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	s := objs[0].MinSize()
	return fyne.NewSize(min(s.Width, l.w), s.Height)
}
func (l *widthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w := min(size.Width, l.w)
	objs[0].Resize(fyne.NewSize(w, size.Height))
	objs[0].Move(fyne.NewPos((size.Width-w)/2, 0))
}

// tappable makes any content clickable, with a pointer cursor.
type tappable struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
	onHover func(bool)
}

func newTappable(content fyne.CanvasObject, onTap func(), onHover func(bool)) *tappable {
	t := &tappable{content: content, onTap: onTap, onHover: onHover}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappable) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(t.content) }
func (t *tappable) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}
func (t *tappable) Cursor() desktop.Cursor { return desktop.PointerCursor }
func (t *tappable) MouseIn(*desktop.MouseEvent) {
	if t.onHover != nil {
		t.onHover(true)
	}
}
func (t *tappable) MouseMoved(*desktop.MouseEvent) {}
func (t *tappable) MouseOut() {
	if t.onHover != nil {
		t.onHover(false)
	}
}
