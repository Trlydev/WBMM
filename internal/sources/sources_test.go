package sources

import (
	"reflect"
	"testing"
)

func TestParseWSIDs(t *testing.T) {
	// The grid's links, in order, with a repeat (image and title link to the
	// same item) and a link inside escaped JSON that must be ignored.
	html := `<a href="https://steamcommunity.com/sharedfiles/filedetails/?id=3827596767" class="x"><img></a>` +
		`<a href="https://steamcommunity.com/sharedfiles/filedetails/?id=3827596767">Walls</a>` +
		`{\"short_description\":\"Requires NML (https://steamcommunity.com/sharedfiles/filedetails/?id=3080294469)\"}` +
		`<a href="https://steamcommunity.com/sharedfiles/filedetails/?id=3821806562">Other</a>`
	got := parseWSIDs(html)
	want := []string{"3827596767", "3821806562"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseWSIDs = %v, want %v", got, want)
	}
}

func TestParseRefs(t *testing.T) {
	for in, want := range map[string]int{
		"gb:462076": 462076, "462076": 462076, "https://gamebanana.com/mods/462076": 462076,
	} {
		if got, ok := ParseGBRef(in); !ok || got != want {
			t.Errorf("ParseGBRef(%q) = %d, %v", in, got, ok)
		}
	}
	if _, ok := ParseGBRef("gb:abc"); ok {
		t.Error("ParseGBRef accepted gb:abc")
	}
	for in, want := range map[string]string{
		"ws:3139755386": "3139755386",
		"https://steamcommunity.com/sharedfiles/filedetails/?id=3139755386&searchtext=": "3139755386",
	} {
		if got, ok := ParseWSRef(in); !ok || got != want {
			t.Errorf("ParseWSRef(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := ParseWSRef("ws:12"); ok {
		t.Error("ParseWSRef accepted a too-short id")
	}
}

func TestHTMLText(t *testing.T) {
	got := HTMLText(`<h2>Features</h2><ul><li>Guns &amp; tanks</li><li>Nukes</li></ul><p>Have <b>fun</b>!<br>Bye</p>`)
	want := "Features\n\n• Guns & tanks\n• Nukes\n\nHave fun!\nBye"
	if got != want {
		t.Errorf("HTMLText = %q, want %q", got, want)
	}
}

func TestBBText(t *testing.T) {
	got := BBText("[h1]ModernBox[/h1]\r\n[list][*]Guns[*]Bombs[/list]\r\n[url=https://x.y]Discord[/url]")
	want := "ModernBox\n• Guns• Bombs\nDiscord"
	if got != want {
		t.Errorf("BBText = %q, want %q", got, want)
	}
}
