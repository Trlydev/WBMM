package sources

import (
	"fmt"
	"strings"
)

type Release struct {
	Tag       string  `json:"tag_name"`
	Name      string  `json:"name"`
	URL       string  `json:"html_url"`
	Published string  `json:"published_at"`
	Assets    []Asset `json:"assets"`
}

type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"` // "sha256:<hex>"
}

// SHA256 returns the asset's SHA-256 as GitHub reports it, or "".
func (a Asset) SHA256() string {
	if h, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
		return strings.ToLower(h)
	}
	return ""
}

// LatestRelease returns repo's release marked latest ("owner/name").
func LatestRelease(repo string) (*Release, error) {
	var r Release
	if err := getJSON("https://api.github.com/repos/"+repo+"/releases/latest", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Asset returns the release asset called name.
func (r *Release) Asset(name string) (Asset, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("release %s has no %s", r.Tag, name)
}
