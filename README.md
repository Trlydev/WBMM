# WorldBox Mod Manager (wbmm)

A simple mod manager for [WorldBox](https://store.steampowered.com/app/1206560/WorldBox__God_Simulator/).
Browse mods from **GameBanana** and the **Steam Workshop**, install them in one
click, turn them on and off, and keep them updated. It also installs and
updates [NeoModLoader](https://github.com/WorldBoxOpenMods/ModLoader), the
loader WorldBox mods need.

Not affiliated with Maxim Karpenko, Valve, GameBanana or the NeoModLoader team.

> **Mods are code that runs on your PC.** wbmm checks GameBanana's virus scan
> before installing, but no scan catches everything. Install mods from authors
> you trust.

## Install

Download the latest build from
[Releases](https://github.com/Trlydev/WBMM/releases/latest):

- **Windows:** `wbmm-windows-amd64.exe`. Double-click it. SmartScreen may warn
  because the program isn't code-signed; choose *More info → Run anyway*.
- **Linux:** `wbmm-linux-amd64`. Run `chmod +x wbmm-linux-amd64 && ./wbmm-linux-amd64`.

It opens in an app window if you have Edge, Chrome, Brave, Vivaldi or
Chromium, and in a tab of your default browser otherwise. The page is only
served to your own PC (127.0.0.1) and every request needs a random
per-session key. Closing the window stops wbmm after a few minutes.

## Using it

- **Browse:** switch between GameBanana and Steam Workshop, search or paste a
  link.
  - *GameBanana* mods install straight into `worldbox/Mods`. Mods with several
    files (versions, ports) let you pick one.
  - *Workshop* mods are installed by Steam. **Subscribe in Steam** opens the mod
    in the Steam client; click Subscribe there and NeoModLoader loads it from
    Steam's Workshop folder.
- **Installed:** every mod NeoModLoader will see, from the Mods folder and the
  Workshop. Switch mods on and off, update GameBanana mods, open their folder,
  remove them (Workshop mods: unsubscribe in Steam).
- **Setup:** install or update NeoModLoader and set the game folder if Steam
  didn't find it.

The first time, turn on **Experimental Mode** in WorldBox's settings, or mods
won't load. Close WorldBox before switching mods on or off: NeoModLoader keeps
its mod list in memory and writes it back when the game runs.

## How it works

| What | Where |
|---|---|
| NeoModLoader | `worldbox/worldbox_Data/StreamingAssets/mods/NeoModLoader.dll`, from the loader's GitHub release marked latest, checked against the SHA-256 GitHub publishes |
| GameBanana mods | `worldbox/Mods/<mod id>/`, one folder per `mod.json` in the download |
| Workshop mods | `steamapps/workshop/content/1206560/<item id>/`, managed by Steam |
| On/off | NeoModLoader's own records, `StreamingAssets/mods/NML/mod_compile_records.json` (the in-game Mods window uses the same file) |
| wbmm's settings | `~/.config/wbmm` (Linux), `%AppData%\wbmm` (Windows) |

GameBanana is read through its public API; the Workshop through its public
pages and Steam's keyless `GetPublishedFileDetails`. No accounts or API keys.

## Command line

```sh
wbmm list                      # installed mods and their IDs
wbmm search modern             # GameBanana
wbmm search -workshop modern   # Steam Workshop
wbmm install gb:462076         # or a gamebanana.com/mods/... link; -file N picks a file
wbmm disable TUXXEGO_MODERNBOX # enable / disable / remove take the ID from list
wbmm updates [-yes]
wbmm nml [install]
```

Set `WBMM_GAME_DIR` to use a WorldBox folder Steam doesn't know about.

## Building

```sh
go build ./cmd/wbmm      # Go 1.25+, no cgo
go test ./...
WBMM_LIVE=1 go test ./internal/nml   # also downloads the real NeoModLoader
```

Every push to `main` publishes a release, `v0.1.<n>`, built by GitHub
Actions for Windows, Linux and macOS.

## License

GPL-3.0
