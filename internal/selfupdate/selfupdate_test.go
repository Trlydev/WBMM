package selfupdate

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.12", "v0.1.9", true},
		{"v0.2.0", "v0.1.99", true},
		{"v0.1.9", "v0.1.9", false},
		{"v0.1.8", "v0.1.9", false},
		{"v1.0.0", "dev", false},
		{"garbage", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// Self-update only trusts SHA256SUMS.txt with the release key's signature.
func TestVerifySums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	old := publicKey
	publicKey = pub
	defer func() { publicKey = old }()
	sums := []byte("abc  wbmm-linux-amd64\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))
	if err := verifySums(sums, sig+"\n"); err != nil {
		t.Errorf("valid signature refused: %v", err)
	}
	if err := verifySums(append(sums, 'x'), sig); err == nil {
		t.Error("changed checksums accepted")
	}
	_, other, _ := ed25519.GenerateKey(nil)
	if err := verifySums(sums, base64.StdEncoding.EncodeToString(ed25519.Sign(other, sums))); err == nil {
		t.Error("someone else's signature accepted")
	}
}

// TestReleaseKeyMatches checks the built-in public key against the private
// release key file (only on the maintainer's PC: WBMM_RELEASE_KEY_FILE).
func TestReleaseKeyMatches(t *testing.T) {
	p := os.Getenv("WBMM_RELEASE_KEY_FILE")
	if p == "" {
		t.Skip("set WBMM_RELEASE_KEY_FILE to the private release key")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode(b)
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("abc  wbmm-linux-amd64\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(k.(ed25519.PrivateKey), sums))
	if err := verifySums(sums, sig); err != nil {
		t.Fatal("the built-in public key doesn't match the release key")
	}
}

// A signed checksum file is only good for the version it names.
func TestSignedVersion(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	old := publicKey
	publicKey = pub
	defer func() { publicKey = old }()
	sums := "abc  wbmm-linux-amd64\n# wbmm v0.2.1\n"
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(sums)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			w.Write([]byte(sig))
			return
		}
		w.Write([]byte(sums))
	}))
	defer srv.Close()
	if got, err := expectedSum(srv.URL+"/s", srv.URL+"/s.sig", "wbmm-linux-amd64", "v0.2.1"); err != nil || got != "abc" {
		t.Errorf("right version: %q, %v", got, err)
	}
	if _, err := expectedSum(srv.URL+"/s", srv.URL+"/s.sig", "wbmm-linux-amd64", "v9.9.9"); err == nil {
		t.Error("an old signed release was accepted as v9.9.9")
	}
}
