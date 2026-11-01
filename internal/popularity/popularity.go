// Package popularity ranks WorldBox mods and worlds by how much attention
// they get.
//
// Neither GameBanana nor the Steam Workshop says what is popular this week,
// and wbmm has no server. A scheduled GitHub Action (cmd/popindex) records
// each item's counter once a day: GameBanana downloads, Workshop lifetime
// subscriptions (both only go up). The difference between today's count and
// the count 1, 7 or 30 days ago is the popularity for that period. Snapshots
// older than Retention are deleted and the data branch is rewritten as one
// commit, so the history never grows.
package popularity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Retention is how many days of daily snapshots are kept.
const Retention = 31

const (
	repo   = "Trlydev/WBMM"
	branch = "popularity-data"
	// IndexURL is where the Action publishes the ranking.
	IndexURL = "https://raw.githubusercontent.com/" + repo + "/" + branch + "/popularity.json"
)

// Snapshot is one day's counters.
type Snapshot struct {
	Date    string         `json:"date"`    // 2006-01-02 (UTC)
	Sources []string       `json:"sources"` // sources collected successfully that day
	Counts  map[string]int `json:"counts"`  // ref -> counter
}

// Item is a ranked entry with what a browse list needs.
type Item struct {
	Ref      string `json:"ref"`  // gb:<id> or ws:<id>
	Src      string `json:"src"`  // gb or ws
	Kind     string `json:"kind"` // mod, world (Workshop) or map (GameBanana)
	Name     string `json:"name"`
	Author   string `json:"author,omitempty"`
	Image    string `json:"image,omitempty"`
	Category string `json:"category,omitempty"`
	URL      string `json:"url,omitempty"`
	Updated  int64  `json:"updated,omitempty"` // unix time
	Likes    int    `json:"likes,omitempty"`   // GameBanana
	Subs     int    `json:"subs,omitempty"`    // Workshop, current subscribers

	N int `json:"n"`           // the counter today: downloads, or lifetime subscriptions
	D int `json:"d,omitempty"` // gained in the last day
	W int `json:"w,omitempty"` // ... week
	M int `json:"m,omitempty"` // ... 30 days
}

// Index is the published ranking.
type Index struct {
	Generated time.Time `json:"generated"`
	Since     string    `json:"since"` // oldest snapshot used
	Items     []Item    `json:"items"`
	// Game is the newest WorldBox version series and when it came out:
	// mods last updated before then may not work.
	Game *Game `json:"game,omitempty"`
	// Provides maps a mod's NeoModLoader ID (as nml.Symbol writes it) to
	// the GameBanana item that installs it, for fetching dependencies.
	Provides map[string]string `json:"provides,omitempty"`
}

// Game is a WorldBox version series.
type Game struct {
	Version  string `json:"version"`
	Released int64  `json:"released"` // unix time
}

// Outdated reports whether something last updated at t predates the
// current game version.
func (ix *Index) Outdated(t int64) bool {
	return ix != nil && ix.Game != nil && t > 0 && t < ix.Game.Released
}

// Compute fills each item's D/W/M from the snapshots (oldest first; the
// last one is today's).
func Compute(items []Item, snaps []Snapshot) {
	if len(snaps) == 0 {
		return
	}
	t0, _ := time.Parse("2006-01-02", snaps[len(snaps)-1].Date)
	for i := range items {
		it := &items[i]
		gain := func(days int) int {
			base := baseline(snaps, t0.AddDate(0, 0, -days), it.Src)
			if base == nil {
				return 0
			}
			old, ok := base.Counts[it.Ref]
			if !ok {
				return 0 // not tracked then: no fair comparison
			}
			return max(it.N-old, 0)
		}
		it.D, it.W, it.M = gain(1), gain(7), gain(30)
	}
}

// baseline is the newest snapshot taken on or before day that has src; if
// history is shorter than the period, the oldest snapshot with src.
func baseline(snaps []Snapshot, day time.Time, src string) *Snapshot {
	var oldest, best *Snapshot
	for i := range snaps[:len(snaps)-1] {
		s := &snaps[i]
		if !has(s.Sources, src) {
			continue
		}
		if oldest == nil {
			oldest = s
		}
		if d, err := time.Parse("2006-01-02", s.Date); err == nil && !d.After(day) {
			best = s
		}
	}
	if best != nil {
		return best
	}
	return oldest
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Periods a ranking can use.
const (
	AllTime = "all"
	Day     = "day"
	Week    = "week"
	Month   = "month"
)

// Gain returns an item's count for a period. All time, Workshop items
// rank by their current subscribers (what Steam shows); gains come from
// lifetime subscriptions, which only go up.
func (it Item) Gain(period string) int {
	switch period {
	case Day:
		return it.D
	case Week:
		return it.W
	case Month:
		return it.M
	}
	if it.Src == "ws" && it.Subs > 0 {
		return it.Subs
	}
	return it.N
}

// Query filters and ranks items: one source and kind, names containing every
// word of q, most gained in the period first (ties: all time).
func (ix *Index) Query(src, kind, q, period string, offset, limit int) (items []Item, more bool) {
	words := strings.Fields(strings.ToLower(q))
	var out []Item
	for _, it := range ix.Items {
		if it.Src != src || (kind != "" && it.Kind != kind) {
			continue
		}
		name := strings.ToLower(it.Name + " " + it.Author)
		ok := true
		for _, w := range words {
			if !strings.Contains(name, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := out[i].Gain(period), out[j].Gain(period); a != b {
			return a > b
		}
		return out[i].N > out[j].N
	})
	if offset >= len(out) {
		return nil, false
	}
	out = out[offset:]
	if len(out) > limit {
		return out[:limit], true
	}
	return out, false
}

// Days is how many days of history the index covers.
func (ix *Index) Days() int {
	d, err := time.Parse("2006-01-02", ix.Since)
	if err != nil {
		return 0
	}
	return int(ix.Generated.Sub(d).Hours() / 24)
}

var (
	mu      sync.Mutex
	cached  *Index
	fetched time.Time
	// CacheDir keeps the last index for offline use ("" disables).
	CacheDir string
	client   = &http.Client{Timeout: 45 * time.Second}
)

// Fetch returns the published index, refreshed at most once an hour.
func Fetch() (*Index, error) {
	mu.Lock()
	defer mu.Unlock()
	if cached != nil && time.Since(fetched) < time.Hour {
		return cached, nil
	}
	ix, err := download()
	if err == nil {
		cached, fetched = ix, time.Now()
		return ix, nil
	}
	if cached != nil {
		return cached, nil
	}
	if CacheDir != "" {
		if b, rerr := os.ReadFile(filepath.Join(CacheDir, "popularity.json")); rerr == nil {
			var ix Index
			if json.Unmarshal(b, &ix) == nil {
				cached, fetched = &ix, time.Now().Add(-50*time.Minute) // retry in 10 minutes
				return cached, nil
			}
		}
	}
	return nil, err
}

// latestURL is popularity.json at the branch's newest commit: GitHub's CDN
// serves a branch URL stale for a few minutes after each run, a commit URL
// never changes.
func latestURL() string {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/commits/"+branch, nil)
	req.Header.Set("Accept", "application/vnd.github.sha")
	req.Header.Set("User-Agent", "wbmm")
	resp, err := client.Do(req)
	if err != nil {
		return IndexURL
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 100))
	sha := strings.TrimSpace(string(b))
	if resp.StatusCode != 200 || len(sha) != 40 || strings.Trim(sha, "0123456789abcdef") != "" {
		return IndexURL
	}
	return "https://raw.githubusercontent.com/" + repo + "/" + sha + "/popularity.json"
}

func download() (*Index, error) {
	req, _ := http.NewRequest("GET", latestURL(), nil)
	req.Header.Set("User-Agent", "wbmm")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no popularity data published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("popularity data: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, err
	}
	if CacheDir != "" {
		os.WriteFile(filepath.Join(CacheDir, "popularity.json"), b, 0o644)
	}
	return &ix, nil
}
