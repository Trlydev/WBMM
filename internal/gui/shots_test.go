package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/Trlydev/WBMM/internal/manager"
)

// TestScreenshots renders each screen to PNG files in $WBMM_SHOTS, with
// live data, for looking at the window without opening it.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("WBMM_SHOTS")
	if dir == "" {
		t.Skip("set WBMM_SHOTS to a folder to render screenshots")
	}
	a := test.NewApp()
	if os.Getenv("WBMM_SHOTS_LIGHT") == "" {
		forceVariant = int(theme.VariantDark)
	}
	a.Settings().SetTheme(wbTheme{})
	m, err := manager.Open()
	if err != nil {
		t.Fatal(err)
	}
	u := &ui{a: a, m: m, version: "v0.2.0"}
	u.w = a.NewWindow("WorldBox Mod Manager")
	u.build()
	u.w.Resize(fyne.NewSize(1200, 820))
	u.refreshStatus(true)
	u.inst.reload()
	shot := func(name string, wait time.Duration) {
		time.Sleep(wait)
		// The real window re-lays out every frame; the test canvas only on
		// resize.
		fyne.DoAndWait(func() {
			u.w.Resize(fyne.NewSize(1201, 820))
			u.w.Resize(fyne.NewSize(1200, 820))
		})
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, u.w.Canvas().Capture())
	}
	shot("1-browse", 8*time.Second)
	fyne.DoAndWait(func() { u.br.setSource("workshop") })
	shot("2-workshop", 8*time.Second)
	fyne.DoAndWait(func() { u.br.openDetails(u.br.cards[0]) })
	shot("3-workshop-details", 6*time.Second)
	fyne.DoAndWait(func() { u.br.closeDetails(); u.br.setSource("gamebanana") })
	time.Sleep(6 * time.Second)
	fyne.DoAndWait(func() { u.br.openDetails(u.br.cards[1]) })
	shot("4-gb-details", 8*time.Second)
	fyne.DoAndWait(func() { u.show(1) })
	shot("5-installed", 3*time.Second)
	fyne.DoAndWait(func() { u.show(2) })
	shot("6-setup", 2*time.Second)
}
