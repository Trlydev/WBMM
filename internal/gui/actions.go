package gui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/sources"
	"github.com/Trlydev/WBMM/internal/steam"
)

// An operation in progress (an install, a Workshop download) is keyed
// "gb:<id>" or "ws:<id>". Every button bound to that key shows its progress,
// wherever it is: a card, the preview page, the Installed list.

type op struct {
	label string
}

func (u *ui) opKey(source, id string) string {
	if source == "workshop" {
		return "ws:" + id
	}
	return "gb:" + id
}

// bindOp registers a button that acts on key; while an operation on key
// runs, it shows its progress and is disabled.
func (u *ui) bindOp(key string, b *widget.Button) {
	if u.opButtons == nil {
		u.opButtons = map[string][]*widget.Button{}
	}
	u.opButtons[key] = append(u.opButtons[key], b)
	if o, ok := u.ops[key]; ok {
		b.SetText(o.label)
		b.Disable()
	}
}

func (u *ui) setOp(key, label string) {
	if u.ops == nil {
		u.ops = map[string]*op{}
	}
	u.ops[key] = &op{label: label}
	for _, b := range u.opButtons[key] {
		b.SetText(label)
		b.Disable()
	}
}

// endOp finishes an operation and redraws everything that shows mods.
func (u *ui) endOp(key string) {
	delete(u.ops, key)
	u.progress(-1)
	u.opButtons = nil
	u.refreshViews()
}

// ---------- GameBanana ----------

// installGBNewest installs a GameBanana mod from a card: straight away when
// it has one file, otherwise its preview page opens to pick one.
func (u *ui) installGBNewest(c modCard) {
	key := u.opKey("gamebanana", c.ID)
	id, _ := strconv.Atoi(c.ID)
	u.setOp(key, "Checking…")
	go func() {
		files, err := sources.GBFiles(id)
		fyne.Do(func() {
			if err != nil {
				u.endOp(key)
				u.fail(err)
				return
			}
			if len(files) != 1 {
				u.endOp(key)
				u.show(pageGet)
				u.get.open(c)
				if len(files) > 1 {
					u.say("%s has %d files. Pick the one you want.", c.Name, len(files))
				}
				return
			}
			delete(u.ops, key)
			u.installGB(id, files[0], c.Name, false)
		})
	}()
}

// installGB downloads, checks and installs one GameBanana file.
func (u *ui) installGB(id int, f sources.GBFile, name string, force bool) {
	u.installGBWith(id, f, name, force, false)
}

func (u *ui) installGBWith(id int, f sources.GBFile, name string, force, acceptRisk bool) {
	key := u.opKey("gamebanana", strconv.Itoa(id))
	if _, busy := u.ops[key]; busy {
		return
	}
	if !f.Clean() && !force {
		msg := fmt.Sprintf("GameBanana's virus scan hasn't passed %s.\n\nInstall it anyway? Only do this if you trust the author.", f.Name)
		d := dialog.NewConfirm("Virus scan not passed", msg, func(ok bool) {
			if ok {
				u.installGBWith(id, f, name, true, acceptRisk)
			}
		}, u.w)
		d.SetConfirmText("Install anyway")
		d.Show()
		return
	}
	u.setOp(key, "Installing…")
	u.say("Downloading and checking %s (%s)…", name, size(f.Size))
	go func() {
		names, err := u.m.InstallGB(id, f.ID, force, acceptRisk)
		fyne.Do(func() {
			u.endOp(key)
			var re *manager.RiskError
			if errors.As(err, &re) {
				u.say("The safety check stopped %s.", name)
				u.riskDialog(name, re, func() { u.installGBWith(id, f, name, force, true) })
				return
			}
			if err != nil {
				u.fail(fmt.Errorf("%s: %w", name, err))
				return
			}
			u.say("Installed %s. It loads the next time WorldBox starts.", strings.Join(names, ", "))
		})
	}()
}

// ---------- Steam Workshop ----------

// subscribe subscribes through Steam and follows the download.
func (u *ui) subscribe(id, name string) {
	key := u.opKey("workshop", id)
	if _, busy := u.ops[key]; busy {
		return
	}
	u.setOp(key, "Asking Steam…")
	u.say("Subscribing to %s through Steam…", name)
	go func() {
		verdict, err := u.m.Subscribe(id, func(p steam.Progress) {
			fyne.Do(func() {
				switch {
				case p.Phase == "downloading" && p.Total > 0:
					frac := float64(p.Done) / float64(p.Total)
					u.setOp(key, fmt.Sprintf("Downloading %d%%", int(frac*100)))
					u.progress(frac)
					u.actText.SetText(fmt.Sprintf("Steam is downloading %s: %s of %s", name, size(int64(p.Done)), size(int64(p.Total))))
				case p.Phase == "waiting":
					u.setOp(key, "Waiting for Steam…")
				}
			})
		})
		fyne.Do(func() {
			u.endOp(key)
			if errors.Is(err, steam.ErrNoLibrary) {
				u.openURL("steam://url/CommunityFilePage/" + id)
				u.say("wbmm can't reach Steam from this WorldBox install, so the mod opened in Steam: click Subscribe there.")
				return
			}
			if err != nil {
				u.fail(fmt.Errorf("%s: %w", name, err))
				return
			}
			switch {
			case verdict != nil && verdict.Blocked():
				u.fail(fmt.Errorf("the safety check switched %s off: its code does things malware does. Review it in My mods", name))
			case verdict == nil:
				u.say("Subscribed to %s. Steam downloaded it; it's in WorldBox's world list.", name)
			default:
				u.say("Subscribed to %s. Steam downloaded it and the safety check passed it; it loads the next time WorldBox starts.", name)
			}
		})
	}()
}

func (u *ui) unsubscribe(id, name string) {
	d := dialog.NewConfirm("Unsubscribe from "+name+"?", "Steam removes the mod from your PC.", func(ok bool) {
		if !ok {
			return
		}
		key := u.opKey("workshop", id)
		u.setOp(key, "Unsubscribing…")
		go func() {
			err := u.m.Unsubscribe(id)
			fyne.Do(func() {
				u.endOp(key)
				if err != nil {
					u.fail(fmt.Errorf("%s: %w", name, err))
					return
				}
				u.say("Unsubscribed from %s.", name)
			})
		}()
	}, u.w)
	d.SetConfirmText("Unsubscribe")
	d.Show()
}

// remove deletes a mod wbmm or the person put in the Mods folder.
func (u *ui) remove(md manager.Mod) {
	d := dialog.NewConfirm("Remove "+md.Name+"?", "Its folder is deleted from your Mods folder.", func(ok bool) {
		if !ok {
			return
		}
		u.say("Removing %s…", md.Name)
		go func() {
			err := u.m.Remove(md.UID)
			fyne.Do(func() {
				if err != nil {
					u.fail(err)
					return
				}
				u.say("Removed %s.", md.Name)
				u.refreshViews()
			})
		}()
	}, u.w)
	d.SetConfirmText("Remove")
	d.Show()
}

// ---------- formatting ----------

func size(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	}
	return fmt.Sprintf("%d KB", max(1, b/1024))
}

func count(n int) string {
	switch {
	case n >= 1e6:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1e3:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "k"
	}
	return strconv.Itoa(n)
}
