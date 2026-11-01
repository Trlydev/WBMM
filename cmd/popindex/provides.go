package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/Trlydev/WBMM/internal/archive"
	"github.com/Trlydev/WBMM/internal/nml"
	"github.com/Trlydev/WBMM/internal/popularity"
	"github.com/Trlydev/WBMM/internal/sources"
)

// A mod.json names what a mod needs by NeoModLoader ID ("Lous12.
// PoliticalWorld"), not by where to get it. To find it, popindex reads the
// mod.json files in each mod's newest GameBanana file: 20 minutes a day
// (mod files never change, so the IDs found are kept by file ID in
// provides-cache.json) until all are read, then only new uploads. Nothing
// in them is run: the archive is unpacked into a temporary folder, its
// mod.json files read, and the folder deleted.

var (
	filesPerRun = flag.Int("files", 300, "most GameBanana files to read for mod IDs per run")
	readFor     = flag.Duration("read-for", 20*time.Minute, "time to spend reading them")
)

const maxReadSize = 100 << 20

// fileIDs is what was found in one GameBanana file.
type fileIDs struct {
	Mod  int      `json:"mod"`
	UIDs []string `json:"uids,omitempty"`
	Err  string   `json:"err,omitempty"`
}

func provides(cachePath string, stats map[int]sources.GBStat, items map[string]popularity.Item) (map[string]string, error) {
	cache := map[string]fileIDs{}
	if b, err := os.ReadFile(cachePath); err == nil {
		json.Unmarshal(b, &cache)
	}
	// The newest file of each mod (maps hold no mod.json).
	type job struct {
		mod  int
		file sources.GBFile
	}
	var jobs []job
	live := map[string]bool{}
	for id, st := range stats {
		if items[fmt.Sprintf("gb:%d", id)].Kind != "mod" || len(st.Files) == 0 {
			continue
		}
		newest := st.Files[0]
		for _, f := range st.Files[1:] {
			if f.Added > newest.Added {
				newest = f
			}
		}
		key := strconv.Itoa(newest.ID)
		live[key] = true
		if _, done := cache[key]; !done {
			jobs = append(jobs, job{id, newest})
		}
	}
	// Most downloaded first, so the mods people need are found first.
	sort.Slice(jobs, func(i, j int) bool { return stats[jobs[i].mod].Downloads > stats[jobs[j].mod].Downloads })
	if len(jobs) > *filesPerRun {
		jobs = jobs[:*filesPerRun]
	}
	start, read := time.Now(), 0
	for _, j := range jobs {
		if time.Since(start) > *readFor {
			break
		}
		read++
		uids, err := readIDs(j.file)
		e := fileIDs{Mod: j.mod, UIDs: uids}
		if err != nil {
			e.Err = err.Error()
		}
		cache[strconv.Itoa(j.file.ID)] = e
	}
	for k := range cache {
		if !live[k] {
			delete(cache, k) // replaced by a newer file, or deleted
		}
	}
	fmt.Fprintf(os.Stderr, "provides: read %d of %d new files, %d known\n", read, len(jobs), len(cache))
	if err := writeJSON(cachePath, cache); err != nil {
		return nil, err
	}
	// Several items can hold the same mod (re-uploads, packs): the most
	// downloaded wins.
	out := map[string]string{}
	best := map[string]int{}
	for _, e := range cache {
		for _, uid := range e.UIDs {
			sym := nml.Symbol(uid)
			if n := stats[e.Mod].Downloads; sym != "" && (out[sym] == "" || n > best[sym]) {
				out[sym], best[sym] = fmt.Sprintf("gb:%d", e.Mod), n
			}
		}
	}
	return out, nil
}

// readIDs downloads a GameBanana file and returns the IDs of the mods in it.
func readIDs(f sources.GBFile) ([]string, error) {
	if f.Size > maxReadSize {
		return nil, fmt.Errorf("too big to read")
	}
	if !f.FromGameBanana() {
		return nil, fmt.Errorf("not hosted on GameBanana")
	}
	tmp, err := os.MkdirTemp("", "popindex-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	arc := filepath.Join(tmp, "download")
	if _, err := sources.Download(f.DownloadURL, arc, maxReadSize); err != nil {
		return nil, err
	}
	if err := f.Verify(arc); err != nil {
		return nil, err
	}
	out := filepath.Join(tmp, "files")
	if err := os.Mkdir(out, 0o755); err != nil {
		return nil, err
	}
	if err := archive.Extract(arc, out); err != nil {
		return nil, err
	}
	var uids []string
	for _, root := range nml.ModRoots(out) {
		if d, err := nml.ReadDeclare(filepath.Join(root, nml.DeclFile)); err == nil && d.UID() != "" {
			uids = append(uids, d.UID())
		}
	}
	time.Sleep(300 * time.Millisecond) // gently
	return uids, nil
}
