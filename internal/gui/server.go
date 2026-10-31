// Package gui is wbmm's window: a page served to this computer only
// (127.0.0.1, with a random per-session key on every request) and shown in
// an app-style browser window.
package gui

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Trlydev/WBMM/internal/game"
	"github.com/Trlydev/WBMM/internal/manager"
	"github.com/Trlydev/WBMM/internal/sources"
)

//go:embed web
var webFS embed.FS

// Repo is wbmm's own GitHub repository, checked for new releases.
const Repo = "Trlydev/WBMM"

type server struct {
	m       *manager.Manager
	key     string
	version string

	pingMu   sync.Mutex
	lastPing time.Time
}

// Run serves the window until it has been closed for a while.
func Run(m *manager.Manager, version string) error {
	if u, ok := existingWindow(); ok {
		return openWindow(u)
	}
	kb := make([]byte, 16)
	rand.Read(kb)
	s := &server{m: m, key: hex.EncodeToString(kb), version: version, lastPing: time.Now()}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	u := fmt.Sprintf("http://%s/?k=%s", ln.Addr(), s.key)
	saveRunning(u)
	defer clearRunning()

	go func() {
		if err := openWindow(u); err != nil {
			fmt.Println("Open this address in your browser:", u)
		}
	}()
	go s.exitWhenClosed(ln)
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	err = srv.Serve(ln)
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// exitWhenClosed stops the server once the page has stopped pinging (the
// window was closed) for three minutes. Browsers slow timers down in
// minimized windows, so a shorter wait could close a window still open.
func (s *server) exitWhenClosed(ln net.Listener) {
	for range time.Tick(5 * time.Second) {
		s.pingMu.Lock()
		idle := time.Since(s.lastPing)
		s.pingMu.Unlock()
		if idle > 3*time.Minute {
			ln.Close()
			return
		}
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(static)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		files.ServeHTTP(w, r)
	})
	api := func(pattern string, h func(*http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !s.authorized(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			v, err := h(r)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			if err != nil {
				out := map[string]any{"error": err.Error()}
				var nc *manager.NotCleanError
				if errors.As(err, &nc) {
					out["notClean"] = true
				}
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(out)
				return
			}
			json.NewEncoder(w).Encode(v)
		})
	}
	api("POST /api/ping", s.ping)
	api("GET /api/status", s.status)
	api("POST /api/settings", s.settings)
	api("GET /api/installed", s.installed)
	api("GET /api/browse", s.browse)
	api("GET /api/gbfiles", s.gbFiles)
	api("POST /api/install", s.install)
	api("POST /api/remove", s.remove)
	api("POST /api/toggle", s.toggle)
	api("GET /api/updates", s.updates)
	api("POST /api/nml", s.installNML)
	api("POST /api/open", s.open)
	mux.HandleFunc("GET /api/icon", s.icon)
	return mux
}

// authorized checks the session key: the X-Key header for API calls, the
// k parameter for images. Requests from other sites are refused outright.
func (s *server) authorized(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	k := r.Header.Get("X-Key")
	if k == "" {
		k = r.URL.Query().Get("k")
	}
	return subtle.ConstantTimeCompare([]byte(k), []byte(s.key)) == 1
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func (s *server) ping(*http.Request) (any, error) {
	s.pingMu.Lock()
	s.lastPing = time.Now()
	s.pingMu.Unlock()
	return map[string]bool{"ok": true}, nil
}

type statusOut struct {
	Version     string `json:"version"`
	GameDir     string `json:"gameDir"`
	GameDirSet  string `json:"gameDirSet"`
	GameError   string `json:"gameError,omitempty"`
	Running     bool   `json:"running"`
	ModsDir     string `json:"modsDir,omitempty"`
	WorkshopDir string `json:"workshopDir,omitempty"`
	AppUpdate   string `json:"appUpdate,omitempty"`
	AppURL      string `json:"appUrl,omitempty"`
}

var (
	appRelMu   sync.Mutex
	appRel     *sources.Release
	appRelTime time.Time
)

// latestApp returns wbmm's newest release, checked at most hourly.
func latestApp() *sources.Release {
	appRelMu.Lock()
	defer appRelMu.Unlock()
	if time.Since(appRelTime) > time.Hour {
		appRelTime = time.Now()
		if r, err := sources.LatestRelease(Repo); err == nil {
			appRel = r
		}
	}
	return appRel
}

func (s *server) status(r *http.Request) (any, error) {
	out := statusOut{Version: s.version, Running: game.Running(), GameDirSet: s.m.Config().GameDir}
	if gd, err := s.m.GameDir(); err != nil {
		out.GameError = err.Error()
	} else {
		out.GameDir, out.ModsDir, out.WorkshopDir = gd, game.ModsDir(gd), game.WorkshopDir(gd)
	}
	if rel := latestApp(); rel != nil && rel.Tag != s.version && strings.HasPrefix(s.version, "v") && newer(rel.Tag, s.version) {
		out.AppUpdate, out.AppURL = rel.Tag, rel.URL
	}
	if r.URL.Query().Get("nml") == "1" {
		st, err := s.m.LoaderStatus()
		if err != nil {
			return map[string]any{"status": out}, nil
		}
		return map[string]any{"status": out, "nml": st}, nil
	}
	return map[string]any{"status": out}, nil
}

// newer reports whether version tag a is after b ("v0.1.10" > "v0.1.9").
func newer(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return len(pa) > len(pb)
}

func (s *server) settings(r *http.Request) (any, error) {
	var in struct {
		GameDir string `json:"gameDir"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, s.m.SetGameDir(in.GameDir)
}

func (s *server) installed(*http.Request) (any, error) {
	mods, err := s.m.List()
	if err != nil {
		return nil, err
	}
	if mods == nil {
		mods = []manager.Mod{}
	}
	return mods, nil
}

// Card is one browse result, from either source.
type Card struct {
	Source    string `json:"source"` // "gamebanana" or "workshop"
	ID        string `json:"id"`
	Name      string `json:"name"`
	Author    string `json:"author,omitempty"`
	Thumb     string `json:"thumb,omitempty"`
	URL       string `json:"url"`
	Category  string `json:"category,omitempty"`
	Updated   int64  `json:"updated,omitempty"`
	Likes     int    `json:"likes,omitempty"`
	Views     int    `json:"views,omitempty"`
	Subs      int    `json:"subs,omitempty"`
	IsMap     bool   `json:"isMap,omitempty"`
	Installed bool   `json:"installed"`
}

func (s *server) browse(r *http.Request) (any, error) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	query, sort := q.Get("q"), q.Get("sort")
	cards := []Card{}
	var more bool
	switch q.Get("src") {
	case "workshop":
		if id, ok := sources.ParseWSRef(query); ok {
			items, err := sources.WSDetails([]string{id})
			if err != nil {
				return nil, err
			}
			return s.wsCards(items, false), nil
		}
		items, m, err := sources.WSBrowse(query, sort, page)
		if err != nil {
			return nil, err
		}
		more = m
		cards = s.wsCards(items, more).Cards
	default:
		if strings.Contains(query, "gamebanana.com/mods/") {
			id, _ := sources.ParseGBRef(query)
			mod, err := sources.GBGetMod(id)
			if err != nil {
				return nil, err
			}
			return browseOut{Cards: s.gbCards([]sources.GBMod{*mod})}, nil
		}
		mods, m, err := sources.GBList(query, sort, page)
		if err != nil {
			return nil, err
		}
		more = m
		cards = s.gbCards(mods)
	}
	return browseOut{Cards: cards, More: more}, nil
}

type browseOut struct {
	Cards []Card `json:"cards"`
	More  bool   `json:"more"`
}

func (s *server) gbCards(mods []sources.GBMod) []Card {
	have := s.m.InstalledGB()
	cards := []Card{}
	for _, m := range mods {
		upd := m.Updated
		if upd == 0 {
			upd = m.Modified
		}
		cards = append(cards, Card{
			Source: "gamebanana", ID: strconv.Itoa(m.ID), Name: m.Name, Author: m.Submitter.Name,
			Thumb: m.Thumb(), URL: m.URL, Category: m.Category.Name, Updated: upd,
			Likes: m.Likes, Views: m.Views, IsMap: m.IsMap(), Installed: have[m.ID],
		})
	}
	return cards
}

func (s *server) wsCards(items []sources.WSItem, more bool) browseOut {
	have := s.m.Subscribed()
	cards := []Card{}
	for _, it := range items {
		cards = append(cards, Card{
			Source: "workshop", ID: it.ID, Name: it.Title, Thumb: it.Preview, URL: it.URL(),
			Updated: it.Updated, Subs: it.Subs, Views: it.Views,
			IsMap: !it.HasTag("Mod"), Installed: have[it.ID],
		})
	}
	return browseOut{Cards: cards, More: more}
}

func (s *server) gbFiles(r *http.Request) (any, error) {
	id, ok := sources.ParseGBRef(r.URL.Query().Get("id"))
	if !ok {
		return nil, errors.New("bad mod id")
	}
	files, err := sources.GBFiles(id)
	if err != nil {
		return nil, err
	}
	type file struct {
		sources.GBFile
		Clean bool `json:"clean"`
	}
	out := []file{}
	for _, f := range files {
		out = append(out, file{f, f.Clean()})
	}
	return out, nil
}

func (s *server) install(r *http.Request) (any, error) {
	var in struct {
		GBID   string `json:"gbId"`
		FileID int    `json:"fileId"`
		Force  bool   `json:"force"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	id, ok := sources.ParseGBRef(in.GBID)
	if !ok {
		return nil, errors.New("bad mod id")
	}
	names, err := s.m.InstallGB(id, in.FileID, in.Force)
	if err != nil {
		return nil, err
	}
	log.Printf("installed gb:%d file %d: %s", id, in.FileID, strings.Join(names, ", "))
	return map[string]any{"installed": names}, nil
}

func (s *server) remove(r *http.Request) (any, error) {
	var in struct {
		UID string `json:"uid"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, s.m.Remove(in.UID)
}

func (s *server) toggle(r *http.Request) (any, error) {
	var in struct {
		UID     string `json:"uid"`
		Enabled bool   `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, s.m.SetEnabled(in.UID, in.Enabled)
}

func (s *server) updates(*http.Request) (any, error) {
	ups, err := s.m.Updates()
	if ups == nil {
		ups = []manager.Update{}
	}
	out := map[string]any{"updates": ups}
	if err != nil {
		out["warning"] = err.Error()
	}
	return out, nil
}

func (s *server) installNML(*http.Request) (any, error) {
	name, err := s.m.InstallLoader()
	if err != nil {
		return nil, err
	}
	return map[string]string{"installed": name}, nil
}

// open shows a mod page, a Workshop item in the Steam client, or a mod
// folder. Only known sites and folders wbmm manages are opened.
func (s *server) open(r *http.Request) (any, error) {
	var in struct {
		What string `json:"what"` // "url", "steam", "folder"
		URL  string `json:"url"`
		ID   string `json:"id"`
		UID  string `json:"uid"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	ok := map[string]bool{"ok": true}
	switch in.What {
	case "steam":
		if _, valid := sources.ParseWSRef("ws:" + in.ID); !valid {
			return nil, errors.New("bad Workshop id")
		}
		// The Steam client's page has the Subscribe button that makes
		// Steam download the mod; the website is the fallback.
		if err := openURL("steam://url/CommunityFilePage/" + in.ID); err != nil {
			return ok, openURL("https://steamcommunity.com/sharedfiles/filedetails/?id=" + in.ID)
		}
		return ok, nil
	case "url":
		if !allowedURL(in.URL) {
			return nil, errors.New("not a known mod site")
		}
		return ok, openURL(in.URL)
	case "folder":
		gd, err := s.m.GameDir()
		if err != nil {
			return nil, err
		}
		dir := game.ModsDir(gd)
		if in.UID != "" {
			md, err := s.m.Find(in.UID)
			if err != nil {
				return nil, err
			}
			dir = md.Folder
		}
		os.MkdirAll(dir, 0o755)
		return ok, openFolder(dir)
	}
	return nil, errors.New("unknown target")
}

func allowedURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" {
		return false
	}
	switch strings.ToLower(p.Hostname()) {
	case "gamebanana.com", "steamcommunity.com", "github.com":
		return true
	}
	return false
}

// icon serves a mod's icon image from its folder.
func (s *server) icon(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	md, err := s.m.Find(r.URL.Query().Get("uid"))
	if err != nil || md.Icon == "" {
		http.NotFound(w, r)
		return
	}
	switch strings.ToLower(filepath.Ext(md.Icon)) {
	case ".png", ".jpg", ".jpeg":
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, md.Icon)
}
