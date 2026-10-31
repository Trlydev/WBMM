package gui

import (
	"net/http/httptest"
	"testing"
)

func TestAuthorized(t *testing.T) {
	s := &server{key: "secret"}
	r := httptest.NewRequest("GET", "/api/installed", nil)
	if s.authorized(r) {
		t.Error("no key accepted")
	}
	r.Header.Set("X-Key", "secret")
	if !s.authorized(r) {
		t.Error("right key refused")
	}
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if s.authorized(r) {
		t.Error("cross-site request accepted")
	}
	r = httptest.NewRequest("GET", "/api/icon?uid=X&k=secret", nil)
	if !s.authorized(r) {
		t.Error("key in the address refused")
	}
}

func TestAllowedURL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://gamebanana.com/mods/1":                            true,
		"https://steamcommunity.com/sharedfiles/filedetails/?id=1": true,
		"https://github.com/Trlydev/WBMM":                          true,
		"http://gamebanana.com/mods/1":                             false,
		"https://gamebanana.com.evil.example/":                     false,
		"file:///etc/passwd":                                       false,
	} {
		if allowedURL(u) != want {
			t.Errorf("allowedURL(%q) = %v", u, !want)
		}
	}
}

func TestNewer(t *testing.T) {
	if !newer("v0.1.10", "v0.1.9") || newer("v0.1.9", "v0.1.10") || newer("v0.2.0", "v0.2.0") {
		t.Error("newer compares wrong")
	}
}
