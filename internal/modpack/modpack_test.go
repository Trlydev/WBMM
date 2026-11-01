package modpack

import (
	"fmt"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := Pack{Name: "Modern war", Entries: []Entry{
		{Source: GameBanana, ID: 462076, FileID: 1847891, Enabled: true, Name: "ModernBox"},
		{Source: Workshop, ID: 3387086485, Enabled: true, Name: "Optime"},
		{Source: Workshop, ID: 3139755386, Enabled: false, Name: "ModernBox (Workshop)"},
		{Source: Local, Enabled: true, Name: "My own tweaks"},
	}}
	code := p.Encode()
	if !strings.HasPrefix(code, Prefix) {
		t.Fatalf("code %q", code)
	}
	got, err := Decode("Try my pack! " + code + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(p) {
		t.Errorf("got  %+v\nwant %+v", got, p)
	}
}

// Twenty mods fit in a chat message.
func TestSmall(t *testing.T) {
	p := Pack{Name: "Big pack"}
	for i := 0; i < 20; i++ {
		p.Entries = append(p.Entries, Entry{Source: GameBanana, ID: uint64(300000 + i*1731), FileID: uint64(1500000 + i*977),
			Enabled: true, Name: fmt.Sprintf("Some Mod Name %d", i)})
	}
	if n := len(p.Encode()); n > 400 {
		t.Errorf("20 mods take %d characters", n)
	}
}

func TestBadCodes(t *testing.T) {
	for _, s := range []string{"", "hello", Prefix, Prefix + "!!!!", Prefix + "AAAA", Prefix + strings.Repeat("A", 5000)} {
		if _, err := Decode(s); err == nil {
			t.Errorf("accepted %q", s[:min(len(s), 20)])
		}
	}
}

// A People Playground code (ppgmods, the sister app) says where it belongs.
func TestPeoplePlaygroundCode(t *testing.T) {
	if _, err := Decode("PPG1.y2BgYGIQSCxKz8wrSSzOyczPS8wBAA"); err != ErrPeoplePlayground {
		t.Fatalf("got %v", err)
	}
}
