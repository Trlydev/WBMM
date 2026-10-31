package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/scan"
)

// What the scanner's rules mean, in plain words.
var ruleTitles = map[string]string{
	"steals-secrets":      "Reads your passwords, logins or tokens",
	"exfiltration":        "Sends data to a drop site",
	"persistence":         "Makes itself start with your computer",
	"dropper":             "Runs commands outside the game",
	"download-and-run":    "Downloads and runs programs",
	"tampers-with-mods":   "Writes into other mods or the mod loader",
	"workshop-upload":     "Uploads to the Steam Workshop (how mod worms spread)",
	"steam-ugc":           "Uses the Steam Workshop upload API (how mod worms spread)",
	"steamugc":            "Uses the Steam Workshop upload API (how mod worms spread)",
	"steam-messages":      "Sends Steam chat messages",
	"steam-friends":       "Uses Steam friends and chat",
	"steamfriends":        "Uses Steam friends and chat",
	"steam-auth":          "Asks Steam for your login ticket",
	"steamuser":           "Asks Steam for your login ticket",
	"hidden-executable":   "Hides a program inside another file",
	"embedded-executable": "Hides a program inside its code",
	"disguised-binary":    "A program disguised as another kind of file",
	"symlink":             "Links to files outside the mod",
	"scan-error":          "Couldn't be checked",

	"network":            "Uses the internet",
	"httpclient":         "Uses the internet",
	"webclient":          "Uses the internet",
	"network-and-run":    "Uses the internet and starts programs",
	"process-spawn":      "Starts other programs",
	"processstartinfo":   "Starts other programs",
	"native-interop":     "Calls native (non-.NET) code",
	"native-code":        "Contains native code",
	"code-loader":        "Loads more code while running",
	"embedded-library":   "Carries extra code libraries",
	"replaces-game-code": "Replaces the game's own code",
	"registry":           "Uses the Windows registry",
	"shell-string":       "Mentions a command shell",
	"deserialization":    "Uses risky .NET object loading",
	"names-loader-files": "Mentions NeoModLoader's or the game's files",
	"self-replication":   "Writes mod files",
	"writes-scripts":     "Writes code files",
	"executable-file":    "Ships a program or script file",
	"xor-decoding":       "Decodes hidden text",
	"split-strings":      "Builds hidden text from pieces",
	"steamworks-any":     "Uses the Steamworks API",
	"worm-dll":           "Uses things a 2026 worm used",
}

func ruleTitle(rule string) string {
	if t, ok := ruleTitles[rule]; ok {
		return t
	}
	return rule
}

// safetyBadge is the Installed list's mark for a mod's last check.
func (u *ui) safetyBadge(md manager.Mod) fyne.CanvasObject {
	p := pal()
	v, current := u.m.Verdict(md.Folder)
	switch {
	case v == nil || !current:
		return badge("checking…", p.muted)
	case v.Blocked():
		b := widget.NewButton("Held: review", func() { u.review(md, v) })
		b.Importance = widget.DangerImportance
		return b
	case v.Accepted:
		return newTappable(badge("allowed by you", p.sand), func() { u.showFindings(md.Name, v.Findings) }, nil)
	case v.Max >= scan.High:
		return newTappable(badge("worth a look", p.sand), func() { u.showFindings(md.Name, v.Findings) }, nil)
	}
	return badge("checked", p.grass)
}

// findingsList shows the HIGH and CRITICAL findings, grouped by meaning.
func findingsList(findings []scan.Finding) fyne.CanvasObject {
	box := container.NewVBox()
	seen := map[string]int{}
	for _, f := range findings {
		if f.Severity < scan.High {
			continue
		}
		key := f.Rule + "|" + f.File
		if seen[key]++; seen[key] > 1 {
			continue
		}
		sev := "Worth a look"
		imp := widget.WarningImportance
		if f.Severity >= scan.Critical {
			sev, imp = "Serious", widget.DangerImportance
		}
		head := widget.NewLabelWithStyle(sev+": "+ruleTitle(f.Rule), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		head.Importance = imp
		where := f.File
		if f.Line > 0 {
			where = fmt.Sprintf("%s, line %d", f.File, f.Line)
		}
		box.Add(container.NewVBox(head, mutedLabel(f.Detail), mutedLabel(where)))
	}
	if len(box.Objects) == 0 {
		box.Add(label("Nothing worth worrying about was found."))
	}
	s := container.NewVScroll(box)
	s.SetMinSize(fyne.NewSize(620, 360))
	return s
}

func (u *ui) showFindings(name string, findings []scan.Finding) {
	intro := mutedLabel("These are things the mod's code can do. Mods often need some of them (the internet for AI text, native code for file pickers); check that they fit what the mod is for.")
	dialog.ShowCustom("Safety check: "+name, "Close", container.NewBorder(intro, nil, nil, nil, findingsList(findings)), u.w)
}

// review shows why a mod is held and lets the person allow it.
func (u *ui) review(md manager.Mod, v *manager.Verdict) {
	intro := label(fmt.Sprintf("wbmm switched %s off because its code does things malware does. It may have an innocent reason (a mod that updates itself, say), but nothing checks what such code does once it runs.", md.Name))
	content := container.NewBorder(intro, nil, nil, nil, findingsList(v.Findings))
	d := dialog.NewCustomConfirm("Held by the safety check", "Allow this version", "Keep it off", content, func(ok bool) {
		if !ok {
			return
		}
		if err := u.m.Accept(md); err != nil {
			u.fail(err)
			return
		}
		u.say("Allowed %s. If its files change, it is checked again.", md.Name)
		u.inst.reload()
	}, u.w)
	d.Show()
}

// riskDialog asks before installing something the scanner stopped.
func (u *ui) riskDialog(name string, err *manager.RiskError, install func()) {
	intro := label(fmt.Sprintf("%s's code does things malware does. It may have an innocent reason, but nothing checks what such code does once it runs. Install it only if you trust the author.", name))
	d := dialog.NewCustomConfirm("The safety check stopped this install", "Install anyway", "Cancel",
		container.NewBorder(intro, nil, nil, nil, findingsList(err.Findings)), func(ok bool) {
			if ok {
				install()
			}
		}, u.w)
	d.Show()
}

// heldMessage summarises newly held mods for the activity bar.
func heldMessage(held []manager.Mod) string {
	var names []string
	for _, md := range held {
		names = append(names, md.Name)
	}
	return "The safety check switched off " + strings.Join(names, ", ") + ". Review under Installed."
}
