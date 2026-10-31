package sources

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	gbAPI    = "https://gamebanana.com/apiv11"
	GBGameID = 11196 // WorldBox
)

type GBMod struct {
	ID        int    `json:"_idRow"`
	Name      string `json:"_sName"`
	URL       string `json:"_sProfileUrl"`
	Version   string `json:"_sVersion"`
	Added     int64  `json:"_tsDateAdded"`
	Modified  int64  `json:"_tsDateModified"`
	Updated   int64  `json:"_tsDateUpdated"`
	Views     int    `json:"_nViewCount"`
	Likes     int    `json:"_nLikeCount"`
	Obsolete  bool   `json:"_bIsObsolete"`
	Submitter struct {
		Name string `json:"_sName"`
	} `json:"_aSubmitter"`
	Category struct {
		Name string `json:"_sName"`
	} `json:"_aRootCategory"`
	Game struct {
		ID int `json:"_idRow"`
	} `json:"_aGame"`
	Preview struct {
		Images []struct {
			Base string `json:"_sBaseUrl"`
			File string `json:"_sFile"`
			F220 string `json:"_sFile220"`
		} `json:"_aImages"`
	} `json:"_aPreviewMedia"`
}

// Thumb returns a small preview image URL, or "".
func (m GBMod) Thumb() string {
	for _, im := range m.Preview.Images {
		f := im.F220
		if f == "" {
			f = im.File
		}
		if im.Base != "" && f != "" {
			return im.Base + "/" + f
		}
	}
	return ""
}

// IsMap reports whether the submission is a world map rather than a mod.
func (m GBMod) IsMap() bool { return strings.EqualFold(m.Category.Name, "Maps") }

type GBFile struct {
	ID          int    `json:"_idRow"`
	Name        string `json:"_sFile"`
	Size        int64  `json:"_nFilesize"`
	Added       int64  `json:"_tsDateAdded"`
	Description string `json:"_sDescription"`
	DownloadURL string `json:"_sDownloadUrl"`
	MD5         string `json:"_sMd5Checksum"`
	Analysis    string `json:"_sAnalysisResult"`
	AVState     string `json:"_sAvState"`
	AVResult    string `json:"_sAvResult"`
	Version     string `json:"_sVersion"`
}

func (f GBFile) AddedTime() time.Time { return time.Unix(f.Added, 0) }

// Clean reports whether GameBanana's own malware analysis finished and passed.
func (f GBFile) Clean() bool {
	return f.AVState == "done" && f.AVResult == "clean" && f.Analysis == "ok"
}

// GBSort values accepted by GBList.
var GBSorts = map[string]string{
	"popular": "Generic_MostDownloaded",
	"new":     "Generic_Newest",
	"updated": "Generic_LatestModified",
	"liked":   "Generic_MostLiked",
}

// GBList lists WorldBox submissions, sorted as asked, optionally only those
// whose name contains query. When no name matches, it falls back to
// GameBanana's full-text search (which also reads descriptions but ranks
// poorly). more reports whether another page exists.
func GBList(query, sort string, page int) (mods []GBMod, more bool, err error) {
	query = strings.TrimSpace(query)
	q := url.Values{}
	q.Set("_nPage", fmt.Sprint(page))
	q.Set("_nPerpage", "30")
	q.Set("_aFilters[Generic_Game]", fmt.Sprint(GBGameID))
	if v, ok := GBSorts[sort]; ok {
		q.Set("_sSort", v)
	}
	if query != "" {
		q.Set("_aFilters[Generic_Name]", "contains,"+query)
	}
	mods, more, err = gbRecords(gbAPI + "/Mod/Index?" + q.Encode())
	if err != nil || len(mods) > 0 || query == "" || page > 1 {
		return mods, more, err
	}
	q = url.Values{}
	q.Set("_nPage", fmt.Sprint(page))
	q.Set("_sModelName", "Mod")
	q.Set("_sOrder", "best_match")
	q.Set("_idGameRow", fmt.Sprint(GBGameID))
	q.Set("_sSearchString", query)
	mods, _, err = gbRecords(gbAPI + "/Util/Search/Results?" + q.Encode())
	return mods, false, err
}

func gbRecords(u string) (mods []GBMod, more bool, err error) {
	var out struct {
		Meta struct {
			Complete bool `json:"_bIsComplete"`
		} `json:"_aMetadata"`
		Records []GBMod `json:"_aRecords"`
	}
	if err := getJSON(u, &out); err != nil {
		return nil, false, err
	}
	for _, m := range out.Records {
		if m.Game.ID == GBGameID {
			mods = append(mods, m)
		}
	}
	return mods, !out.Meta.Complete && len(out.Records) > 0, nil
}

// GBGetMod returns one WorldBox submission.
func GBGetMod(id int) (*GBMod, error) {
	var m GBMod
	u := fmt.Sprintf("%s/Mod/%d?_csvProperties=_idRow,_sName,_sProfileUrl,_sVersion,_tsDateAdded,_tsDateModified,_tsDateUpdated,_aSubmitter,_aRootCategory,_aGame,_aPreviewMedia", gbAPI, id)
	if err := getJSON(u, &m); err != nil {
		return nil, err
	}
	if m.Game.ID != GBGameID {
		return nil, fmt.Errorf("GameBanana mod %d is not a WorldBox mod", id)
	}
	return &m, nil
}

// GBFiles returns the downloadable files of a mod, newest first.
func GBFiles(id int) ([]GBFile, error) {
	var out struct {
		Files []GBFile `json:"_aFiles"`
	}
	if err := getJSON(fmt.Sprintf("%s/Mod/%d?_csvProperties=_aFiles", gbAPI, id), &out); err != nil {
		return nil, err
	}
	fs := out.Files
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0 && fs[j].Added > fs[j-1].Added; j-- {
			fs[j], fs[j-1] = fs[j-1], fs[j]
		}
	}
	return fs, nil
}

var reGBURL = regexp.MustCompile(`gamebanana\.com/mods/(\d+)`)

// ParseGBRef accepts "gb:123", "123" or a gamebanana.com/mods/123 link.
func ParseGBRef(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if m := reGBURL.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	s = strings.TrimPrefix(s, "gb:")
	id, err := strconv.Atoi(s)
	return id, err == nil && id > 0
}
