package gui

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/Trlydev/WBMM/internal/sources"
)

// Images are fetched in the background, at most a few at a time, shrunk to
// the size they're shown at and kept in memory and on disk.

var (
	imgSem   = make(chan struct{}, 6)
	imgMu    sync.Mutex
	imgCache = map[string]image.Image{} // key: url + width
)

func imgCacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	d = filepath.Join(d, "wbmm", "images")
	if os.MkdirAll(d, 0o755) != nil {
		return ""
	}
	return d
}

// pruneImageCache removes cached images not used for 30 days.
func pruneImageCache() {
	d := imgCacheDir()
	es, _ := os.ReadDir(d)
	for _, e := range es {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 30*24*time.Hour {
			os.Remove(filepath.Join(d, e.Name()))
		}
	}
}

// newRemoteImage returns an image that fills in once url has loaded,
// scaled down to at most maxW pixels wide.
func newRemoteImage(url string, maxW int, fill canvas.ImageFill) *canvas.Image {
	img := canvas.NewImageFromImage(nil)
	img.FillMode = fill
	img.ScaleMode = canvas.ImageScaleSmooth
	if url == "" {
		return img
	}
	key := fmt.Sprintf("%s#%d", url, maxW)
	imgMu.Lock()
	cached := imgCache[key]
	imgMu.Unlock()
	if cached != nil {
		img.Image = cached
		return img
	}
	go func() {
		im := loadImage(url, maxW)
		if im == nil {
			return
		}
		imgMu.Lock()
		if len(imgCache) > 600 {
			imgCache = map[string]image.Image{}
		}
		imgCache[key] = im
		imgMu.Unlock()
		fyne.Do(func() {
			img.Image = im
			img.Refresh()
		})
	}()
	return img
}

func loadImage(url string, maxW int) image.Image {
	imgSem <- struct{}{}
	defer func() { <-imgSem }()
	var b []byte
	sum := sha1.Sum([]byte(url))
	file := ""
	if d := imgCacheDir(); d != "" {
		file = filepath.Join(d, hex.EncodeToString(sum[:]))
		if c, err := os.ReadFile(file); err == nil {
			b = c
			now := time.Now()
			os.Chtimes(file, now, now)
		}
	}
	if b == nil {
		var err error
		if b, err = sources.GetImage(url); err != nil {
			return nil
		}
		if file != "" {
			os.WriteFile(file, b, 0o644)
		}
	}
	return decodeScaled(b, maxW)
}

func decodeScaled(b []byte, maxW int) image.Image {
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= maxW || w == 0 {
		return src
	}
	nh := h * maxW / w
	dst := image.NewRGBA(image.Rect(0, 0, maxW, max(nh, 1)))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// localImage loads a mod's icon from its folder.
func localImage(path string, maxW int) *canvas.Image {
	img := canvas.NewImageFromImage(nil)
	img.FillMode = canvas.ImageFillCover
	img.ScaleMode = canvas.ImageScaleSmooth
	if path == "" {
		return img
	}
	go func() {
		b, err := os.ReadFile(path)
		if err != nil || len(b) > 20<<20 {
			return
		}
		if im := decodeScaled(b, maxW); im != nil {
			fyne.Do(func() {
				img.Image = im
				img.Refresh()
			})
		}
	}()
	return img
}
