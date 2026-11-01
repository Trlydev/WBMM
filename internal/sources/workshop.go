package sources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const wsAppID = 1206560 // WorldBox

// WSItem is a Steam Workshop item, from the keyless GetPublishedFileDetails.
type WSItem struct {
	ID          string `json:"publishedfileid"`
	Result      int    `json:"result"`
	AppID       int    `json:"consumer_app_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Preview     string `json:"preview_url"`
	Size        string `json:"file_size"`
	Created     int64  `json:"time_created"`
	Updated     int64  `json:"time_updated"`
	Subs        int    `json:"subscriptions"`
	LifetimeSub int    `json:"lifetime_subscriptions"`
	Favorited   int    `json:"favorited"`
	Views       int    `json:"views"`
	Creator     string `json:"creator"`
	Banned      int    `json:"banned"`
	Tags        []struct {
		Tag string `json:"tag"`
	} `json:"tags"`
}

func (it WSItem) URL() string {
	return "https://steamcommunity.com/sharedfiles/filedetails/?id=" + it.ID
}

// HasTag reports whether the item carries tag (case-insensitive).
func (it WSItem) HasTag(tag string) bool {
	for _, t := range it.Tags {
		if strings.EqualFold(t.Tag, tag) {
			return true
		}
	}
	return false
}

// WSSorts maps wbmm's sort names to the Workshop's.
var WSSorts = map[string]string{
	"popular": "trend", // trending this week
	"new":     "mostrecent",
	"updated": "lastupdated",
	"subs":    "totaluniquesubscribers", // all-time popularity
}

// The Workshop browser is a React page with generated class names; the
// rendered grid links each item as href="…filedetails/?id=N", in order.
// Links inside descriptions sit in escaped JSON (\") and don't match.
var reWSLink = regexp.MustCompile(`href="https://steamcommunity\.com/sharedfiles/filedetails/\?id=(\d+)"`)

// Workshop tags WorldBox uses.
const (
	TagMod   = "Mod"
	TagWorld = "World"
)

// WSBrowseIDs returns the item IDs on one page of the Workshop with tag
// (TagMod or TagWorld), sorted or searched as asked (30 per page).
func WSBrowseIDs(query, sort, tag string, page int) ([]string, error) {
	q := url.Values{}
	q.Set("appid", fmt.Sprint(wsAppID))
	q.Set("section", "readytouseitems")
	q.Add("requiredtags[]", tag)
	q.Set("p", fmt.Sprint(page))
	if s := strings.TrimSpace(query); s != "" {
		q.Set("searchtext", s)
		q.Set("browsesort", "textsearch")
	} else {
		v, ok := WSSorts[sort]
		if !ok {
			v = "trend"
		}
		q.Set("browsesort", v)
		q.Set("actualsort", v)
	}
	html, err := getText("https://steamcommunity.com/workshop/browse/?"+q.Encode(), 8<<20)
	if err != nil {
		return nil, err
	}
	return parseWSIDs(html), nil
}

func parseWSIDs(html string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, m := range reWSLink.FindAllStringSubmatch(html, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	return ids
}

// WSDetails returns the WorldBox items among ids, in the same order.
// Removed, private and other games' items are left out.
func WSDetails(ids []string) ([]WSItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	form := url.Values{}
	form.Set("itemcount", fmt.Sprint(len(ids)))
	for i, id := range ids {
		form.Set(fmt.Sprintf("publishedfileids[%d]", i), id)
	}
	resp, err := postForm("https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/", form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Response struct {
			Details []WSItem `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var items []WSItem
	for _, it := range out.Response.Details {
		if it.Result == 1 && it.AppID == wsAppID && it.Banned == 0 {
			items = append(items, it)
		}
	}
	return items, nil
}

// WSBrowse is WSBrowseIDs followed by WSDetails. more reports whether the
// Workshop likely has another page.
func WSBrowse(query, sort, tag string, page int) (items []WSItem, more bool, err error) {
	ids, err := WSBrowseIDs(query, sort, tag, page)
	if err != nil {
		return nil, false, err
	}
	items, err = WSDetails(ids)
	return items, len(ids) >= 30, err
}

var reWSRef = regexp.MustCompile(`(?:filedetails/\?id=|^ws:)(\d{6,12})`)

// ParseWSRef accepts "ws:123" or a Workshop item link.
func ParseWSRef(s string) (string, bool) {
	if m := reWSRef.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		return m[1], true
	}
	return "", false
}

// WSAllIDs lists the IDs of Workshop items with tag, most subscribed
// first, reading at most maxPages pages of 30.
func WSAllIDs(tag string, maxPages int) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for page := 1; page <= maxPages; page++ {
		ids, err := WSBrowseIDs("", "subs", tag, page)
		if err != nil {
			return out, err
		}
		n := 0
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
				n++
			}
		}
		if len(ids) < 30 || n == 0 {
			break
		}
	}
	return out, nil
}

// WSDetailsAll is WSDetails for any number of IDs (100 per request).
func WSDetailsAll(ids []string) ([]WSItem, error) {
	var out []WSItem
	for i := 0; i < len(ids); i += 100 {
		items, err := WSDetails(ids[i:min(i+100, len(ids))])
		if err != nil {
			return out, err
		}
		out = append(out, items...)
	}
	return out, nil
}
