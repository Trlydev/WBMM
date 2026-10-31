// Package sources talks to the places mods come from: GameBanana (public
// API, direct downloads, server-side antivirus results), the Steam Workshop
// (public pages and the keyless details API) and GitHub releases (for
// NeoModLoader and wbmm itself).
package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

var UserAgent = "wbmm/dev (+https://github.com/Trlydev/WBMM)"

var client = &http.Client{Timeout: 5 * time.Minute, Transport: publicTransport()}

// publicTransport only connects to public internet addresses, so a hijacked
// download link can't make wbmm send requests to this computer or the local
// network, directly or through a redirect. With a proxy configured, the
// proxy resolves names and the check is left to it.
func publicTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if !proxyConfigured() {
		d.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !PublicIP(ip) {
				return fmt.Errorf("refusing to connect to non-public address %s", host)
			}
			return nil
		}
	}
	t.DialContext = d.DialContext
	return t
}

func proxyConfigured() bool {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// PublicIP reports whether ip is a routable internet address.
func PublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.Equal(net.IPv4bcast) || ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) // 100.64.0.0/10 (CGNAT)
}

func do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s %s: HTTP %d", req.Method, req.URL.Redacted(), resp.StatusCode)
	}
	return resp, nil
}

func get(u string) (*http.Response, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	return do(req)
}

func postForm(u string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(req)
}

func getJSON(u string, v any) error {
	resp, err := get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// getText reads at most max bytes of a page.
func getText(u string, max int64) (string, error) {
	resp, err := get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max))
	return string(b), err
}

// Download saves url to path and returns its SHA-256 hex digest. Files
// larger than maxBytes are refused.
func Download(u, path string, maxBytes int64) (sha256hex string, err error) {
	resp, err := get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("download is larger than %d MB", maxBytes>>20)
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxBytes {
		err = fmt.Errorf("download is larger than %d MB", maxBytes>>20)
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
