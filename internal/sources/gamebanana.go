package sources

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
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
			F530 string `json:"_sFile530"`
			F800 string `json:"_sFile800"`
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

// FromGameBanana reports whether the file downloads from GameBanana itself
// (https, gamebanana.com or one of its subdomains).
func (f GBFile) FromGameBanana() bool {
	u, err := url.Parse(f.DownloadURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "gamebanana.com" || strings.HasSuffix(h, ".gamebanana.com")
}

// Verify checks a downloaded copy against the MD5 checksum GameBanana
// lists for the file: a download changed on the way, or swapped on the
// server after GameBanana's virus scan, doesn't match.
func (f GBFile) Verify(path string) error {
	if f.MD5 == "" {
		return nil // older files have none
	}
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	h := md5.New()
	if _, err := io.Copy(h, fh); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, f.MD5) {
		return fmt.Errorf("%s isn't the file GameBanana lists (checksum %s, expected %s)", f.Name, got[:12], strings.ToLower(f.MD5)[:min(12, len(f.MD5))])
	}
	return nil
}

// Clean reports whether GameBanana's own malware analysis finished and passed.
func (f GBFile) Clean() bool {
	return f.AVState == "done" && f.AVResult == "clean" && f.Analysis == "ok"
}

// GBSort values accepted by GBList ("relevance" is GameBanana's search).
var GBSorts = map[string]string{
	"popular": "Generic_MostDownloaded",
	"new":     "Generic_Newest",
	"updated": "Generic_LatestModified",
	"liked":   "Generic_MostLiked",
}

// GBMapsCategory is WorldBox's "Maps" category: saved worlds, not mods.
const GBMapsCategory = 23647

// GBList lists WorldBox submissions, sorted as asked: maps only (maps) or
// everything but maps. With sort "relevance" it is GameBanana's search (best
// match first, descriptions too); otherwise names containing query. more
// reports whether another page exists.
func GBList(query, sort string, maps bool, page int) (mods []GBMod, more bool, err error) {
	query = strings.TrimSpace(query)
	keep := func(ms []GBMod) []GBMod {
		var out []GBMod
		for _, m := range ms {
			if m.IsMap() == maps {
				out = append(out, m)
			}
		}
		return out
	}
	if sort == "relevance" && query != "" {
		q := url.Values{}
		q.Set("_nPage", fmt.Sprint(page))
		q.Set("_sModelName", "Mod")
		q.Set("_sOrder", "best_match")
		q.Set("_idGameRow", fmt.Sprint(GBGameID))
		q.Set("_sSearchString", query)
		mods, more, err = gbRecords(gbAPI + "/Util/Search/Results?" + q.Encode())
		return keep(mods), more, err
	}
	q := url.Values{}
	q.Set("_nPage", fmt.Sprint(page))
	q.Set("_nPerpage", "30")
	q.Set("_aFilters[Generic_Game]", fmt.Sprint(GBGameID))
	if maps {
		q.Set("_aFilters[Generic_Category]", fmt.Sprint(GBMapsCategory))
	}
	v, ok := GBSorts[sort]
	if !ok {
		v = GBSorts["popular"]
	}
	q.Set("_sSort", v)
	if query != "" {
		q.Set("_aFilters[Generic_Name]", "contains,"+query)
	}
	mods, more, err = gbRecords(gbAPI + "/Mod/Index?" + q.Encode())
	return keep(mods), more, err
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

// GBProfile is a mod's full page: description, screenshots and files.
type GBProfile struct {
	GBMod
	Text        string   `json:"_sText"` // HTML
	Description string   `json:"_sDescription"`
	Downloads   int      `json:"_nDownloadCount"`
	Files       []GBFile `json:"_aFiles"`
}

// Screenshots returns the mod's images, large size first choice.
func (p GBProfile) Screenshots() []string {
	var out []string
	for _, im := range p.Preview.Images {
		f := im.F800
		if f == "" {
			f = im.F530
		}
		if f == "" {
			f = im.File
		}
		if im.Base != "" && f != "" {
			out = append(out, im.Base+"/"+f)
		}
	}
	return out
}

// GBGetProfile returns a WorldBox mod's full page, files newest first.
func GBGetProfile(id int) (*GBProfile, error) {
	var p GBProfile
	if err := getJSON(fmt.Sprintf("%s/Mod/%d/ProfilePage", gbAPI, id), &p); err != nil {
		return nil, err
	}
	if p.Game.ID != GBGameID {
		return nil, fmt.Errorf("GameBanana mod %d is not a WorldBox mod", id)
	}
	fs := p.Files
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0 && fs[j].Added > fs[j-1].Added; j-- {
			fs[j], fs[j-1] = fs[j-1], fs[j]
		}
	}
	return &p, nil
}

// GBAll lists every WorldBox submission (newest first).
func GBAll() ([]GBMod, error) {
	var all []GBMod
	for page := 1; page <= 200; page++ {
		q := url.Values{}
		q.Set("_nPage", fmt.Sprint(page))
		q.Set("_nPerpage", "50")
		q.Set("_aFilters[Generic_Game]", fmt.Sprint(GBGameID))
		q.Set("_sSort", "Generic_Newest")
		mods, more, err := gbRecords(gbAPI + "/Mod/Index?" + q.Encode())
		if err != nil {
			return all, err
		}
		all = append(all, mods...)
		if !more {
			break
		}
	}
	return all, nil
}

// GBStat is what GBStats returns for a mod.
type GBStat struct {
	Downloads int      `json:"_nDownloadCount"`
	Files     []GBFile `json:"_aFiles"`
}

// GBStats returns the download counts and files of mods, 50 per request.
func GBStats(ids []int) (map[int]GBStat, error) {
	out := map[int]GBStat{}
	for i := 0; i < len(ids); i += 50 {
		batch := ids[i:min(i+50, len(ids))]
		var csv []string
		for _, id := range batch {
			csv = append(csv, fmt.Sprint(id))
		}
		var rows []struct {
			ID int `json:"_idRow"`
			GBStat
		}
		u := fmt.Sprintf("%s/Mod/Multi?_csvRowIds=%s&_csvProperties=_idRow,_nDownloadCount,_aFiles", gbAPI, strings.Join(csv, ","))
		if err := getJSON(u, &rows); err != nil {
			return out, err
		}
		for _, r := range rows {
			out[r.ID] = r.GBStat
		}
	}
	return out, nil
}
