// Package sources talks to the places mods come from: GameBanana (public
// API, direct downloads, server-side antivirus results), the Steam Workshop
// (public pages and the keyless details API) and GitHub releases (for
// NeoModLoader and wbmm itself).
package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// client has no overall time limit (a 100 MB mod on a slow line takes a
// while); requests set their own (see get), downloads stop when they stall.
// Every hop, redirects included, must be https.
var client = &http.Client{Transport: publicTransport(), CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing to follow a redirect to %s (not https)", req.URL.Redacted())
	}
	return nil
}}

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
	t.TLSHandshakeTimeout = 20 * time.Second
	t.ResponseHeaderTimeout = 45 * time.Second
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

// apiTimeout limits a whole API request or page read (not a download).
const apiTimeout = 90 * time.Second

func do(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("refusing %s: not https", req.URL.Redacted())
	}
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

// get fetches u within apiTimeout.
func get(u string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }

func postForm(u string, form url.Values) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
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

// ErrEmptyDownload: the server answered with nothing (GameBanana does
// this when it's busy or limiting how much one address downloads).
var ErrEmptyDownload = errors.New("the server sent an empty file (it may be busy): try again in a few minutes")

// Download saves url to path and returns its SHA-256 hex digest. Files
// larger than maxBytes are refused.
func Download(u, path string, maxBytes int64) (sha256hex string, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	resp, err := do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// No overall limit, but a download that stalls for a minute stops.
	body := &stallReader{r: resp.Body, timer: time.AfterFunc(stallTimeout, cancel)}
	defer body.timer.Stop()
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("download is larger than %d MB", maxBytes>>20)
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, maxBytes+1))
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("the download stalled for %s", stallTimeout)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxBytes {
		err = fmt.Errorf("download is larger than %d MB", maxBytes>>20)
	}
	if err == nil && n == 0 {
		err = ErrEmptyDownload
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// imageHosts are the only hosts mod images are fetched from.
var imageHosts = map[string]bool{
	"images.gamebanana.com":             true,
	"images.steamusercontent.com":       true,
	"steamuserimages-a.akamaihd.net":    true,
	"shared.akamai.steamstatic.com":     true,
	"shared.fastly.steamstatic.com":     true,
	"cdn.akamai.steamstatic.com":        true,
	"shared.cloudflare.steamstatic.com": true,
}

// GetImage downloads a mod preview image (at most 15 MB) from a known
// image host.
func GetImage(u string) ([]byte, error) {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || !imageHosts[strings.ToLower(p.Hostname())] {
		return nil, fmt.Errorf("not a known image host: %s", u)
	}
	resp, err := get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 15<<20))
}

const stallTimeout = time.Minute

// stallReader restarts its timer whenever data arrives.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(stallTimeout)
	}
	return n, err
}
