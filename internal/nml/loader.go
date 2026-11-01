package nml

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Trlydev/WBMM/internal/sources"
)

const (
	// Repo publishes NML's builds; its release marked latest is the stable one.
	Repo    = "WorldBoxOpenMods/ModLoader"
	DLLName = "NeoModLoader.dll"
)

// LoaderStatus describes the NeoModLoader install in a game's native mods
// folder (StreamingAssets/mods).
type LoaderStatus struct {
	Installed  bool   `json:"installed"`
	UpToDate   bool   `json:"upToDate"`
	Latest     string `json:"latest,omitempty"` // release name, e.g. "v1.2.1.1-BugFix"
	LatestSHA  string `json:"latestSha,omitempty"`
	ReleaseURL string `json:"releaseUrl,omitempty"`
	// CheckError is set when the latest release couldn't be read
	// (offline); Installed is still accurate.
	CheckError string `json:"checkError,omitempty"`
}

// Status checks the installed NeoModLoader.dll against the latest release.
func Status(nativeModsDir string) LoaderStatus {
	var st LoaderStatus
	sum, err := fileSHA256(filepath.Join(nativeModsDir, DLLName))
	st.Installed = err == nil
	rel, err := sources.LatestRelease(Repo)
	if err != nil {
		st.CheckError = err.Error()
		return st
	}
	st.Latest, st.ReleaseURL = rel.Name, rel.URL
	if st.Latest == "" {
		st.Latest = rel.Tag
	}
	if a, err := rel.Asset(DLLName); err == nil {
		st.LatestSHA = a.SHA256()
		st.UpToDate = st.Installed && a.SHA256() != "" && a.SHA256() == sum
	}
	return st
}

// backupSuffix marks the previous version kept by Install.
const backupSuffix = ".wbmm-previous"

// HasBackup reports whether Install kept a previous version to go back to.
func HasBackup(nativeModsDir string) bool {
	_, err := os.Stat(filepath.Join(nativeModsDir, DLLName+backupSuffix))
	return err == nil
}

// Restore puts back the version Install replaced (new NML versions
// sometimes break older mods). The game must be closed.
func Restore(nativeModsDir string) error {
	for _, name := range []string{DLLName, "NeoModLoader.pdb"} {
		p := filepath.Join(nativeModsDir, name)
		prev := p + backupSuffix
		if _, err := os.Stat(prev); err != nil {
			if name == DLLName {
				return errors.New("there's no previous NeoModLoader version to go back to")
			}
			continue
		}
		if err := os.Rename(prev, p); err != nil {
			return err
		}
	}
	return nil
}

// Install downloads the latest NeoModLoader.dll (and its .pdb, for readable
// error logs) into the native mods folder, checking each file against the
// SHA-256 GitHub lists for it, and keeps the version it replaces. The game
// must be closed.
func Install(nativeModsDir string) (string, error) {
	rel, err := sources.LatestRelease(Repo)
	if err != nil {
		return "", fmt.Errorf("couldn't read NeoModLoader's releases: %w", err)
	}
	if err := os.MkdirAll(nativeModsDir, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{DLLName, "NeoModLoader.pdb"} {
		a, err := rel.Asset(name)
		if err != nil {
			if name == DLLName {
				return "", err
			}
			continue
		}
		want := a.SHA256()
		if want == "" {
			return "", fmt.Errorf("GitHub lists no checksum for %s; not installing it", name)
		}
		dst := filepath.Join(nativeModsDir, name)
		tmp := dst + ".wbmm-download"
		got, err := sources.Download(a.URL, tmp, 200<<20)
		if err != nil {
			return "", fmt.Errorf("downloading %s: %w", name, err)
		}
		if got != want {
			os.Remove(tmp)
			return "", fmt.Errorf("%s doesn't match its published checksum; not installed", name)
		}
		if old, err := fileSHA256(dst); err == nil && old != want {
			os.Remove(dst + backupSuffix)
			os.Rename(dst, dst+backupSuffix)
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return "", err
		}
	}
	if rel.Name != "" {
		return rel.Name, nil
	}
	return rel.Tag, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
