// Command popindex records today's GameBanana download counts and Workshop
// subscriptions for WorldBox and writes the ranking (popularity.json) that
// wbmm sorts by. It runs once a day in GitHub Actions
// (.github/workflows/popularity.yml):
//
//	go run ./cmd/popindex -dir data
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Trlydev/WBMM/internal/popularity"
	"github.com/Trlydev/WBMM/internal/sources"
)

var dir = flag.String("dir", "data", "folder holding snapshots/ and popularity.json")

// Workshop worlds number in the tens of thousands; the most subscribed
// 1,500 cover what anyone browses. All Workshop mods are tracked.
const worldPages = 50

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "popindex:", err)
		os.Exit(1)
	}
}

func run() error {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	snapDir := filepath.Join(*dir, "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return err
	}
	snap := popularity.Snapshot{Date: today, Counts: map[string]int{}}
	items := map[string]popularity.Item{}
	add := func(it popularity.Item) {
		items[it.Ref] = it
		snap.Counts[it.Ref] = it.N
	}
	if err := collectGB(add); err != nil {
		fmt.Fprintln(os.Stderr, "GameBanana:", err)
	} else {
		snap.Sources = append(snap.Sources, "gb")
	}
	if err := collectWS(add); err != nil {
		fmt.Fprintln(os.Stderr, "Steam Workshop:", err)
	} else {
		snap.Sources = append(snap.Sources, "ws")
	}
	if len(snap.Sources) == 0 {
		return fmt.Errorf("no source answered")
	}
	if err := writeJSON(filepath.Join(snapDir, today+".json"), snap); err != nil {
		return err
	}
	snaps, err := loadSnapshots(snapDir, now)
	if err != nil {
		return err
	}
	list := make([]popularity.Item, 0, len(items))
	for _, it := range items {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Ref < list[j].Ref })
	popularity.Compute(list, snaps)
	ix := popularity.Index{Generated: now, Since: snaps[0].Date, Items: list}
	fmt.Fprintf(os.Stderr, "%d items, sources %v, history since %s\n", len(list), snap.Sources, ix.Since)
	return writeJSON(filepath.Join(*dir, "popularity.json"), ix)
}

func collectGB(add func(popularity.Item)) error {
	all, err := sources.GBAll()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return fmt.Errorf("no mods listed")
	}
	ids := make([]int, len(all))
	for i, m := range all {
		ids[i] = m.ID
	}
	dl, err := sources.GBDownloads(ids)
	if err != nil {
		return err
	}
	for _, m := range all {
		kind := "mod"
		if m.IsMap() {
			kind = "map"
		}
		upd := m.Updated
		if upd == 0 {
			upd = m.Modified
		}
		add(popularity.Item{Ref: fmt.Sprintf("gb:%d", m.ID), Src: "gb", Kind: kind, Name: m.Name,
			Author: m.Submitter.Name, Image: m.Thumb(), Category: m.Category.Name, URL: m.URL,
			Updated: upd, Likes: m.Likes, N: dl[m.ID]})
	}
	return nil
}

func collectWS(add func(popularity.Item)) error {
	mods, err := sources.WSAllIDs(sources.TagMod, 100)
	if err != nil {
		return err
	}
	worlds, err := sources.WSAllIDs(sources.TagWorld, worldPages)
	if err != nil {
		return err
	}
	details, err := sources.WSDetailsAll(append(mods, worlds...))
	if err != nil {
		return err
	}
	if len(details) == 0 {
		return fmt.Errorf("no items listed")
	}
	for _, it := range details {
		kind := "mod"
		if it.HasTag(sources.TagWorld) && !it.HasTag(sources.TagMod) {
			kind = "world"
		}
		add(popularity.Item{Ref: "ws:" + it.ID, Src: "ws", Kind: kind, Name: it.Title, Image: it.Preview,
			URL: it.URL(), Updated: it.Updated, Subs: it.Subs, N: it.LifetimeSub})
	}
	return nil
}

func loadSnapshots(snapDir string, now time.Time) ([]popularity.Snapshot, error) {
	ents, err := os.ReadDir(snapDir)
	if err != nil {
		return nil, err
	}
	cutoff := now.AddDate(0, 0, -popularity.Retention).Format("2006-01-02")
	var out []popularity.Snapshot
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".json")
		if name < cutoff {
			os.Remove(filepath.Join(snapDir, e.Name()))
			continue
		}
		b, err := os.ReadFile(filepath.Join(snapDir, e.Name()))
		if err != nil {
			return nil, err
		}
		var s popularity.Snapshot
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func writeJSON(p string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o644)
}
