# WorldBox Mod Manager (wbmm)

A simple mod manager for [WorldBox](https://store.steampowered.com/app/1206560/WorldBox__God_Simulator/).
Browse mods from **GameBanana** and the **Steam Workshop**, preview them,
install them in one click, switch them on and off, and keep them updated,
along with [NeoModLoader](https://github.com/WorldBoxOpenMods/ModLoader), the
loader WorldBox mods need.

Not affiliated with Maxim Karpenko, Valve, GameBanana or the NeoModLoader team.

> **Mods are code that runs on your PC.** wbmm checks GameBanana's virus scan
> before installing, but no scan catches everything. Install mods from authors
> you trust.

## Install

1. Download the latest build from
   [Releases](https://github.com/Trlydev/WBMM/releases/latest):
   - **Windows:** `wbmm-windows-amd64.exe`. Double-click it. SmartScreen may
     warn because the program isn't code-signed; choose *More info → Run anyway*.
   - **Linux:** `wbmm-linux-amd64`. Run
     `chmod +x wbmm-linux-amd64 && ./wbmm-linux-amd64`.
2. Click **Install** in the banner. wbmm installs itself as a normal app (no
   admin rights needed):
   - **Windows:** `%LocalAppData%\Programs\WorldBox Mod Manager`, with Start
     menu and desktop shortcuts and an *Apps & features* entry.
   - **Linux:** `~/.local/bin/wbmm`, with an app-menu entry, icon and desktop
     shortcut.

   You can delete the download afterwards. From a terminal: `wbmm install-app`
   and `wbmm uninstall-app`. Uninstalling leaves your mods and settings alone.

The window is a native app (Fyne; it needs OpenGL 2.1). Starting it again
brings up the open window.

## Using it

- **Browse:** GameBanana or Steam Workshop; search, sort, or paste a link.
  Click a mod to open its page: screenshots, description, stats, and its files.
  - *GameBanana* mods download into `worldbox/Mods`. Mods with several files
    (versions, ports) let you pick one.
  - *Workshop* mods: **Subscribe** subscribes through your Steam client (Steam
    must be running) and shows the download as it happens. Steam keeps the mod
    updated and removes it when you unsubscribe.
- **Installed:** every mod NeoModLoader will load, from the Mods folder and
  the Workshop. Switch mods on and off, update, open their folder or page,
  remove or unsubscribe.
- **Setup:** install or update NeoModLoader, the game folder, automatic
  updates, and the app install.

The first time, turn on **Experimental Mode** in WorldBox's settings, or mods
won't load. Switch mods on and off with WorldBox closed: NeoModLoader keeps its
mod list in memory while the game runs.

## Updates

Both are on by default and can be turned off in Setup.

- **wbmm updates itself.** It checks GitHub at start and every six hours,
  downloads the new build, checks it against the release's `SHA256SUMS.txt`
  **signed with the release key** (Ed25519, public key built into wbmm), swaps
  it in and offers a restart. `wbmm self-update` does it from a terminal.
- **GameBanana mods update themselves.** Same schedule. An update only follows
  the file you installed (ModernBox `m1` never turns into `m5`) and only
  installs when GameBanana's virus scan passed it. Anything held back shows an
  **Update** button under Installed. `wbmm updates -yes` does it from a
  terminal.
- **Workshop mods** are updated by Steam.

## How it works

| What | Where |
|---|---|
| NeoModLoader | `worldbox_Data/StreamingAssets/mods/NeoModLoader.dll`, from the loader's GitHub release marked latest, checked against the SHA-256 GitHub publishes |
| GameBanana mods | `worldbox/Mods/<mod id>/`, one folder per `mod.json` in the download |
| Workshop mods | `steamapps/workshop/content/1206560/<item id>/`, downloaded by Steam |
| On/off | NeoModLoader's own records, `StreamingAssets/mods/NML/mod_compile_records.json` (the in-game Mods window uses the same file) |
| wbmm's settings and log | `~/.config/wbmm` (Linux), `%AppData%\wbmm` (Windows) |

GameBanana is read through its public API; the Workshop through its public
pages and Steam's keyless `GetPublishedFileDetails`. Subscribing uses the
Steamworks library WorldBox ships (`ISteamUGC`), in a short helper process
under WorldBox's app ID, the way NeoModLoader's in-game Mod Hub does it, so
Steam shows you as playing WorldBox while a download runs. No accounts or API
keys.

## Command line

```sh
wbmm list                      # installed mods and their IDs
wbmm search modern             # GameBanana
wbmm search -workshop modern   # Steam Workshop
wbmm install gb:462076         # or a gamebanana.com/mods/... link; -file N picks a file
wbmm subscribe ws:3139755386   # through Steam; unsubscribe likewise
wbmm disable TUXXEGO_MODERNBOX # enable / disable / remove take the ID from list
wbmm updates [-yes]
wbmm nml [install]
wbmm self-update
```

Set `WBMM_GAME_DIR` to use a WorldBox folder Steam doesn't know about.

## Building

```sh
go build ./cmd/wbmm      # Go 1.25+, cgo, OpenGL headers on Linux
go test ./...
WBMM_LIVE=1 go test ./internal/nml                    # downloads the real NeoModLoader
WBMM_SHOTS=/tmp/shots go test -run Screenshots ./internal/gui   # renders each screen
```

Every push to `main` publishes a release, `v0.2.<n>`, built by GitHub Actions
for Windows and Linux, with signed checksums.

## License

GPL-3.0
