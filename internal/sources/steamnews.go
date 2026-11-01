package sources

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GameRelease is a WorldBox version series (0.51, say) and when it came
// out. Mods break when the game's code changes, which happens with each
// new series; the hotfixes after it (0.51.1, 0.51.2) rarely break them.
type GameRelease struct {
	Version  string `json:"version"`
	Released int64  `json:"released"` // unix time
}

var reGameVersion = regexp.MustCompile(`\b0\.(\d{1,3})(\.\d+)?`)

// LatestGameRelease reads WorldBox's announcements on Steam for the newest
// version series and the day it was released: the first announcement
// naming a full version (0.51.0) or saying it is live. Previews and
// "coming soon" posts name a series without either, and don't count.
func LatestGameRelease() (GameRelease, error) {
	var out struct {
		AppNews struct {
			Items []struct {
				Title string `json:"title"`
				Date  int64  `json:"date"`
			} `json:"newsitems"`
		} `json:"appnews"`
	}
	u := fmt.Sprintf("https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%d&count=100&maxlength=1&feeds=steam_community_announcements", wsAppID)
	if err := getJSON(u, &out); err != nil {
		return GameRelease{}, err
	}
	var news []newsItem
	for _, it := range out.AppNews.Items {
		news = append(news, newsItem{it.Title, it.Date})
	}
	return latestRelease(news)
}

type newsItem struct {
	Title string
	Date  int64
}

func latestRelease(news []newsItem) (GameRelease, error) {
	first := map[int]int64{} // series -> first release announcement
	for _, it := range news {
		m := reGameVersion.FindStringSubmatch(it.Title)
		if m == nil {
			continue
		}
		live := strings.Contains(strings.ToLower(it.Title), " live")
		if m[2] == "" && !live {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if t, ok := first[n]; !ok || it.Date < t {
			first[n] = it.Date
		}
	}
	if len(first) == 0 {
		return GameRelease{}, fmt.Errorf("no release announcements found")
	}
	var series []int
	for n := range first {
		series = append(series, n)
	}
	sort.Ints(series)
	n := series[len(series)-1]
	return GameRelease{Version: fmt.Sprintf("0.%d", n), Released: first[n]}, nil
}
