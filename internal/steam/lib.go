package steam

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// lib is the part of the Steamworks flat API wbmm uses, loaded at run time
// (no cgo). WorldBox ships SDK interface ISteamUGC v014.
type lib struct {
	isSteamRunning func() bool
	init           func() bool
	shutdown       func()
	runCallbacks   func()
	ugc            func() uintptr
	numSubscribed  func(ugc uintptr) uint32
	subscribed     func(ugc uintptr, ids *uint64, max uint32) uint32
	state          func(ugc uintptr, id uint64) uint32
	downloadInfo   func(ugc uintptr, id uint64, done, total *uint64) bool
	subscribe      func(ugc uintptr, id uint64) uint64
	unsubscribe    func(ugc uintptr, id uint64) uint64
	download       func(ugc uintptr, id uint64, highPriority bool) bool
}

func load(path string) (l *lib, err error) {
	h, err := openLibrary(path)
	if err != nil {
		return nil, err
	}
	// RegisterLibFunc panics on a missing symbol; report it instead.
	defer func() {
		if r := recover(); r != nil {
			l, err = nil, fmt.Errorf("%v", r)
		}
	}()
	l = &lib{}
	for name, fn := range map[string]any{
		"SteamAPI_IsSteamRunning":                  &l.isSteamRunning,
		"SteamAPI_Init":                            &l.init,
		"SteamAPI_Shutdown":                        &l.shutdown,
		"SteamAPI_RunCallbacks":                    &l.runCallbacks,
		"SteamAPI_SteamUGC_v014":                   &l.ugc,
		"SteamAPI_ISteamUGC_GetNumSubscribedItems": &l.numSubscribed,
		"SteamAPI_ISteamUGC_GetSubscribedItems":    &l.subscribed,
		"SteamAPI_ISteamUGC_GetItemState":          &l.state,
		"SteamAPI_ISteamUGC_GetItemDownloadInfo":   &l.downloadInfo,
		"SteamAPI_ISteamUGC_SubscribeItem":         &l.subscribe,
		"SteamAPI_ISteamUGC_UnsubscribeItem":       &l.unsubscribe,
		"SteamAPI_ISteamUGC_DownloadItem":          &l.download,
	} {
		purego.RegisterLibFunc(fn, h, name)
	}
	return l, nil
}
