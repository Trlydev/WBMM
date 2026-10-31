// Package steam subscribes to and unsubscribes from WorldBox's Steam
// Workshop items through the user's running Steam client, the way the game
// (and NeoModLoader's in-game Mod Hub) does: with the Steamworks library
// WorldBox ships, under WorldBox's app ID. Steam then downloads, updates
// and removes the files itself, in its Workshop folder.
//
// Each operation runs in a short-lived helper process ("wbmm steam-helper"):
// while a process has the Steamworks API open, Steam shows the user as
// playing WorldBox, and a library crash can't take the window down with it.
package steam

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// AppID is WorldBox's Steam app ID.
const AppID = "1206560"

// Item state flags (EItemState).
const (
	StateSubscribed      = 1
	StateInstalled       = 4
	StateNeedsUpdate     = 8
	StateDownloading     = 16
	StateDownloadPending = 32
)

// ErrNoLibrary means the game has no Steamworks library wbmm can load, e.g.
// the Windows build of WorldBox run through Proton on Linux.
var ErrNoLibrary = errors.New("this WorldBox install has no Steamworks library wbmm can use")

// LibPath returns the Steamworks library WorldBox ships for this OS.
func LibPath(gameDir string) (string, error) {
	plugins := filepath.Join(gameDir, "worldbox_Data", "Plugins")
	var names []string
	switch runtime.GOOS {
	case "windows":
		names = []string{`x86_64\steam_api64.dll`, "steam_api64.dll"}
	case "linux":
		names = []string{"libsteam_api.so", "x86_64/libsteam_api.so"}
	default:
		return "", ErrNoLibrary
	}
	for _, n := range names {
		p := filepath.Join(plugins, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", ErrNoLibrary
}

// Progress is one report from a running operation.
type Progress struct {
	ID    string `json:"id,omitempty"`
	State uint32 `json:"state,omitempty"`
	Done  uint64 `json:"done,omitempty"`
	Total uint64 `json:"total,omitempty"`
	// Phase is "waiting" (Steam hasn't started), "downloading" or "done".
	Phase string `json:"phase,omitempty"`
	Error string `json:"error,omitempty"`
	// Items answers "list": every subscribed item and its state.
	Items map[string]uint32 `json:"items,omitempty"`
}

// One operation at a time: two helpers would both hold the API open.
var mu sync.Mutex

// marker starts each line the helper means for wbmm; Steam's library also
// prints to the same output.
const marker = "WBMM-STEAM "

// Subscribe subscribes to a Workshop item and waits until Steam has
// downloaded it, reporting progress.
func Subscribe(gameDir, id string, progress func(Progress)) error {
	_, err := run(gameDir, "subscribe", id, progress)
	return err
}

// Unsubscribe unsubscribes from a Workshop item. Steam deletes its files.
func Unsubscribe(gameDir, id string) error {
	_, err := run(gameDir, "unsubscribe", id, nil)
	return err
}

// Update asks Steam to download an item's newest version now.
func Update(gameDir, id string, progress func(Progress)) error {
	_, err := run(gameDir, "update", id, progress)
	return err
}

// Subscriptions returns every WorldBox Workshop item the account is
// subscribed to, with its state flags.
func Subscriptions(gameDir string) (map[string]uint32, error) {
	last, err := run(gameDir, "list", "", nil)
	if err != nil {
		return nil, err
	}
	return last.Items, nil
}

func run(gameDir, op, id string, progress func(Progress)) (Progress, error) {
	if id != "" && !validID(id) {
		return Progress{}, fmt.Errorf("bad Workshop id %q", id)
	}
	lib, err := LibPath(gameDir)
	if err != nil {
		return Progress{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Progress{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	cmd := exec.Command(exe, "steam-helper", op, lib, id)
	// Steamworks reads the app ID from here when the program wasn't
	// started by Steam.
	cmd.Env = append(os.Environ(), "SteamAppId="+AppID, "SteamGameId="+AppID)
	cmd.Dir = os.TempDir()
	hideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Progress{}, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return Progress{}, err
	}
	var last Progress
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), marker)
		if !ok {
			continue
		}
		var p Progress
		if json.Unmarshal([]byte(line), &p) != nil {
			continue
		}
		last = p
		if progress != nil && p.Error == "" {
			progress(p)
		}
	}
	werr := cmd.Wait()
	if last.Error != "" {
		return last, errors.New(last.Error)
	}
	if last.Phase != "done" {
		if werr != nil {
			return last, fmt.Errorf("the Steam helper stopped unexpectedly (%v)", werr)
		}
		return last, errors.New("the Steam helper stopped before finishing")
	}
	return last, nil
}

func validID(id string) bool {
	if len(id) < 6 || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ---------- helper process ----------

// RunHelper is the "steam-helper" command: args are op, library, item ID.
// It writes progress lines and exits.
func RunHelper(args []string) int {
	emit := func(p Progress) {
		b, _ := json.Marshal(p)
		fmt.Println(marker + string(b))
	}
	if len(args) < 2 {
		emit(Progress{Error: "steam-helper: missing arguments"})
		return 2
	}
	op, lib, id := args[0], args[1], ""
	if len(args) > 2 {
		id = args[2]
	}
	if err := helper(op, lib, id, emit); err != nil {
		emit(Progress{ID: id, Error: err.Error()})
		return 1
	}
	return 0
}

func helper(op, lib, id string, emit func(Progress)) error {
	api, err := load(lib)
	if err != nil {
		return fmt.Errorf("couldn't load Steam's library: %w", err)
	}
	if !api.isSteamRunning() {
		return errors.New("Steam isn't running. Start Steam and try again")
	}
	if !api.init() {
		return errors.New("Steam didn't connect. Make sure you're logged in to Steam and own WorldBox")
	}
	defer api.shutdown()
	ugc := api.ugc()
	if ugc == 0 {
		return errors.New("Steam's Workshop interface isn't available")
	}
	var fid uint64
	if id != "" {
		if _, err := fmt.Sscan(id, &fid); err != nil {
			return fmt.Errorf("bad Workshop id %q", id)
		}
	}

	switch op {
	case "list":
		n := api.numSubscribed(ugc)
		items := map[string]uint32{}
		if n > 0 {
			ids := make([]uint64, n)
			n = api.subscribed(ugc, &ids[0], n)
			for _, x := range ids[:n] {
				items[fmt.Sprint(x)] = api.state(ugc, x)
			}
		}
		emit(Progress{Phase: "done", Items: items})
		return nil

	case "subscribe":
		api.subscribe(ugc, fid)
		return waitInstalled(api, ugc, fid, id, emit)

	case "update":
		if !api.download(ugc, fid, true) {
			return errors.New("Steam wouldn't start the download (is the item subscribed?)")
		}
		return waitInstalled(api, ugc, fid, id, emit)

	case "unsubscribe":
		api.unsubscribe(ugc, fid)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			api.runCallbacks()
			if api.state(ugc, fid)&StateSubscribed == 0 {
				emit(Progress{ID: id, Phase: "done"})
				return nil
			}
			time.Sleep(250 * time.Millisecond)
		}
		return errors.New("Steam didn't confirm the unsubscribe; try again")
	}
	return fmt.Errorf("unknown steam-helper operation %q", op)
}

// waitInstalled reports download progress until Steam has the item's
// current version installed.
func waitInstalled(api *lib, ugc uintptr, fid uint64, id string, emit func(Progress)) error {
	start := time.Now()
	lastEmit := time.Time{}
	for {
		api.runCallbacks()
		st := api.state(ugc, fid)
		busy := st&(StateDownloading|StateDownloadPending|StateNeedsUpdate) != 0
		if st&StateSubscribed != 0 && st&StateInstalled != 0 && !busy {
			emit(Progress{ID: id, State: st, Phase: "done"})
			return nil
		}
		if st&StateSubscribed == 0 && time.Since(start) > time.Minute {
			return errors.New("Steam didn't subscribe to the item. Check that it still exists and that you can see it")
		}
		if time.Since(start) > 2*time.Hour {
			return errors.New("the download is taking more than two hours; Steam keeps going in the background")
		}
		if time.Since(lastEmit) > 300*time.Millisecond {
			p := Progress{ID: id, State: st, Phase: "waiting"}
			if st&StateDownloading != 0 {
				p.Phase = "downloading"
				var done, total uint64
				if api.downloadInfo(ugc, fid, &done, &total) {
					p.Done, p.Total = done, total
				}
			}
			emit(p)
			lastEmit = time.Now()
		}
		time.Sleep(200 * time.Millisecond)
	}
}
