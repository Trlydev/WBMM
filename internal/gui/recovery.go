package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/manager"
)

// The top of My mods says what went wrong at the last game start (a crash,
// mods that failed) and offers one-click ways out: switch the failed mods
// off, play without mods, and later bring them back.

// recoveryBanner is the box at the top of the My mods list, or nil.
func (v *myModsPage) recoveryBanner() fyne.CanvasObject {
	u, p := v.u, pal()
	box := container.NewVBox()
	add := func(msg string, imp widget.Importance) {
		l := label(msg)
		l.Wrapping = fyne.TextWrapWord
		l.Importance = imp
		box.Add(l)
	}

	if sa := u.m.SetAside(); sa != nil {
		add(fmt.Sprintf("Switched off for troubleshooting since %s: %s.", sa.Since.Format("Jan 2, 15:04"), strings.Join(sa.Names, ", ")), widget.MediumImportance)
		back := widget.NewButton("Bring them back", func() {
			failed, err := u.m.BringBack()
			switch {
			case err != nil:
				u.fail(err)
			case len(failed) > 0:
				u.fail(fmt.Errorf("couldn't switch on %s: see each mod", strings.Join(failed, ", ")))
			default:
				u.say("Switched your mods back on.")
			}
			v.reload()
		})
		back.Importance = widget.HighImportance
		box.Add(container.NewHBox(back))
	}

	gl := v.lastRun
	if gl != nil && !u.m.SeenRun(gl.Time) {
		var failed []manager.Mod
		for _, md := range v.mods {
			if md.Enabled && v.runResult(md).failed {
				failed = append(failed, md)
			}
		}
		if gl.Crashed || len(failed) > 0 {
			when := gl.Time.Format("Jan 2, 15:04")
			if gl.Crashed {
				msg := "WorldBox crashed at the last start (" + when + ")."
				if gl.LastMod != "" {
					msg += " The last mod NeoModLoader mentioned before it was " + gl.LastMod + "."
				}
				add(msg, widget.DangerImportance)
				if pairs := v.overlapPairs(); len(pairs) > 0 {
					add("These switched-on mods change the same game code, a common cause: "+strings.Join(pairs, "; ")+".", widget.MediumImportance)
				}
			}
			if len(failed) > 0 {
				var names []string
				for _, md := range failed {
					names = append(names, md.Name)
				}
				add(fmt.Sprintf("%d didn't load at the last start: %s. Select one to see why.", len(failed), strings.Join(names, ", ")), widget.WarningImportance)
			}
			row := container.NewHBox()
			if len(failed) > 0 {
				off := widget.NewButton(fmt.Sprintf("Switch off the %d that failed", len(failed)), func() {
					v.putAside(failed, false)
				})
				off.Importance = widget.HighImportance
				row.Add(off)
			}
			if gl.Crashed {
				row.Add(widget.NewButton("Play without mods", func() {
					var on []manager.Mod
					for _, md := range v.mods {
						if md.Enabled && md.Problem == "" {
							on = append(on, md)
						}
					}
					v.putAside(on, true)
				}))
			}
			row.Add(layout.NewSpacer())
			row.Add(widget.NewButton("Dismiss", func() {
				if err := u.m.DismissRun(gl.Time); err != nil {
					u.fail(err)
				}
				v.paint()
			}))
			box.Add(row)
		}
	}
	if len(box.Objects) == 0 {
		return nil
	}
	c := p.warn
	if gl != nil && gl.Crashed && !u.m.SeenRun(gl.Time) {
		c = p.bad
	}
	return container.New(layout.NewCustomPaddedLayout(0, 6, 0, 0), tint(box, withAlpha(c, 0x24)))
}

// putAside switches mods off for troubleshooting; play starts the game.
func (v *myModsPage) putAside(mods []manager.Mod, play bool) {
	u := v.u
	if err := u.m.PutAside(mods); err != nil {
		u.fail(err)
		return
	}
	if v.lastRun != nil {
		u.m.DismissRun(v.lastRun.Time)
	}
	u.say("Switched off %d mod(s). \"Bring them back\" on My mods switches them on again.", len(mods))
	v.reload()
	if play {
		u.play()
	}
}

// overlapPairs lists the pairs of switched-on mods that change the same
// game code, in words.
func (v *myModsPage) overlapPairs() []string {
	seen := map[string]bool{}
	var out []string
	names := map[string]string{}
	for _, md := range v.mods {
		names[md.UID] = md.Name
	}
	for uid, list := range v.overlaps {
		for _, o := range list {
			key := uid + "|" + o.UID
			if uid > o.UID {
				key = o.UID + "|" + uid
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, fmt.Sprintf("%s and %s (%s)", names[uid], o.Other, targetsWords(o.Targets)))
		}
	}
	return out
}

// targetsWords names up to two game methods.
func targetsWords(t []string) string {
	if len(t) <= 2 {
		return strings.Join(t, ", ")
	}
	return fmt.Sprintf("%s, %s and %d more", t[0], t[1], len(t)-2)
}

// overlapNote is the note on a mod's panel about mods that change the
// same game code, or "".
func (v *myModsPage) overlapNote(md manager.Mod) string {
	list := v.overlaps[md.UID]
	if len(list) == 0 {
		return ""
	}
	var parts []string
	for _, o := range list {
		parts = append(parts, fmt.Sprintf("%s (%s)", o.Other, targetsWords(o.Targets)))
	}
	return "Replaces or can skip the same game code as " + strings.Join(parts, "; ") +
		". They often work together, but if one misbehaves, try with the other switched off."
}
