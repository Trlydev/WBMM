package popularity

import (
	"testing"
	"time"
)

func TestComputeAndQuery(t *testing.T) {
	snaps := []Snapshot{
		{Date: "2026-10-01", Sources: []string{"gb", "ws"}, Counts: map[string]int{"gb:1": 100, "gb:2": 500, "ws:9": 50}},
		{Date: "2026-10-25", Sources: []string{"gb", "ws"}, Counts: map[string]int{"gb:1": 150, "gb:2": 510, "ws:9": 80}},
		{Date: "2026-10-31", Sources: []string{"gb"}, Counts: map[string]int{"gb:1": 300, "gb:2": 520}},
		{Date: "2026-11-01", Sources: []string{"gb", "ws"}, Counts: map[string]int{"gb:1": 310, "gb:2": 521, "gb:3": 40, "ws:9": 90}},
	}
	items := []Item{
		{Ref: "gb:1", Src: "gb", Kind: "mod", Name: "Rising Star", N: 310},
		{Ref: "gb:2", Src: "gb", Kind: "mod", Name: "Old Giant", N: 521},
		{Ref: "gb:3", Src: "gb", Kind: "mod", Name: "Brand New", N: 40},
		{Ref: "ws:9", Src: "ws", Kind: "world", Name: "Europe", N: 90, Subs: 70},
	}
	Compute(items, snaps)
	if items[0].D != 10 || items[0].W != 160 || items[0].M != 210 {
		t.Errorf("gb:1 gains: %+v", items[0])
	}
	if items[2].W != 0 {
		t.Errorf("an item not tracked a week ago gained %d", items[2].W)
	}
	if items[3].D != 10 { // the 10-31 snapshot has no ws: compared with 10-25
		t.Errorf("ws:9 day gain: %+v", items[3])
	}
	ix := &Index{Generated: time.Now(), Items: items}
	if top, _ := ix.Query("gb", "", "", AllTime, 0, 10); top[0].Name != "Old Giant" {
		t.Errorf("all time: %v", top)
	}
	if top, _ := ix.Query("gb", "", "", Week, 0, 10); top[0].Name != "Rising Star" {
		t.Errorf("trending: %v", top)
	}
	if top, _ := ix.Query("gb", "mod", "giant", AllTime, 0, 10); len(top) != 1 {
		t.Errorf("search: %v", top)
	}
	if top, _ := ix.Query("ws", "world", "", AllTime, 0, 10); top[0].Gain(AllTime) != 70 {
		t.Errorf("Workshop all time is current subscribers: %v", top)
	}
}
