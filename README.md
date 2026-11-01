# WorldBox Mod Manager (wbmm)

A simple mod manager for [WorldBox](https://store.steampowered.com/app/1206560/WorldBox__God_Simulator/).
Browse mods from **GameBanana** and the **Steam Workshop**, preview them,
install them in one click, switch them on and off, and keep them updated,
along with [NeoModLoader](https://github.com/WorldBoxOpenMods/ModLoader), the
loader WorldBox mods need.

Not affiliated with Maxim Karpenko, Valve, GameBanana or the NeoModLoader team.

> **Mods are code that runs on your PC.** wbmm checks every mod with its own
> safety check (below) and GameBanana's virus scan, but no scan catches
> everything. Install mods from authors you trust.

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

The window is laid out like a mod manager: a sidebar, a list, and the
selected item's details beside it.

- **Play WorldBox** starts the game through Steam, after checking that
  NeoModLoader is there.
- **My mods:** every mod NeoModLoader will load (the Mods folder, the game's
  own mods folder and the Workshop) with an on/off switch each. Select one for
  its safety check result, missing requirements or clashes, updates, its
  folder, and Remove or Unsubscribe, and **what happened at the last game
  start**: wbmm reads NeoModLoader's lines in the game's log, so a mod that
  didn't compile or load says why, in plain words ("written for a different
  WorldBox version"), with the compiler's errors.
- **Get mods:** GameBanana or Steam Workshop mods; search, sort, or paste a
  link. Select one for its screenshots, description, stats and files.
  - *GameBanana* mods download into `worldbox/Mods`. Mods with several files
    (versions, ports) let you pick one.
  - *Workshop* mods: **Subscribe** subscribes through your Steam client (Steam
    must be running) and shows the download as it happens. Steam keeps the mod
    updated and removes it when you unsubscribe.
- **Worlds:** Workshop worlds (saved maps), kept apart from mods. See the
  ones you're subscribed to, or browse and subscribe to more.
- **Modpacks:** save your mods as a modpack, share it, open someone else's.
  Saved packs work as profiles: apply one (optionally switching off what
  isn't in it) to swap sets of mods in one click.
- **Settings:** NeoModLoader, the game folder, automatic updates, and the app
  install.

The first time, turn on **Experimental Mode** in WorldBox's settings, or mods
won't load. Switch mods on and off with WorldBox closed: NeoModLoader keeps its
mod list in memory while the game runs.

## Modpacks (.wbm)

A modpack is one short line of text, like
`WBM1.4vatVAjPL8pJccqvYOL4M1Pm8pMCTt_8lNSiPKf8Csapa7vP87D5F5Rk5qYCBgA` (two
mods; twenty take about 300 characters): small enough to paste into a chat.
Saved as a file it's a `.wbm`, and the installed app opens `.wbm` files on a
double-click. Inside, compressed: each mod's source (GameBanana or the
Workshop), its ID there, which file, whether it's on, and its name. No
URLs and no code: opening one shows what it would install, switch on or off,
and what it can't fetch (mods installed by hand), and applying it installs
from GameBanana and the Workshop as always, through the safety check.
`internal/modpack` has the format.

## Popularity

Neither site says what's trending, so a daily GitHub Action
(`.github/workflows/popularity.yml`, `cmd/popindex`) records every WorldBox
mod's GameBanana downloads and Workshop subscriptions (all Workshop mods and
the 1,500 most subscribed worlds) and publishes the gains over a day, week
and month to the `popularity-data` branch. "Most downloaded", "Most
subscribed" and "Trending this week/month" sort by those numbers, and each
row shows the number it's sorted by. No server, no keys; the branch is
rewritten each day so it never grows.

## Safety check

NeoModLoader compiles a mod's C# with the whole .NET framework available, or
loads its DLLs as they are, and runs them inside the game with no sandbox. So
wbmm checks every mod's code before it can run:

- **GameBanana mods** before they reach the Mods folder.
- **Workshop mods** right after Steam downloads them. A risky one is switched
  off in NeoModLoader's list before the game can load it.
- **Every installed mod** again whenever its files change (once a minute),
  which catches Steam's silent Workshop updates: the 2026 People Playground
  and PlateUp! worms spread through updates of existing mods.

The scanner (`internal/scan`, built on SWG's) reads the code NeoModLoader
would actually load, following its rules exactly (checked against its
source): `.cs` files outside the folders it skips (matched by exact name, as
it does), top-level DLLs, `Assemblies/`, every DLL of a BepInEx-type mod,
mod folders in the game's own mods folder, and zip files waiting in `Mods/`
(a risky one is renamed so NeoModLoader skips it). Bundled libraries count as
known only by hash (official NuGet releases, NeoModLoader's releases, the
game's own files); text is read with split strings joined and escapes
decoded, and data files are checked for stolen-data destinations. Ordinary modding (Harmony, reflection, config files, even
network calls) is at most "worth a look". **Serious** findings are what malware
needs and mods don't: reading browser passwords, Discord tokens or Steam
sessions; Discord webhooks and Telegram bots; autostart entries; hidden
programs; encoded PowerShell; downloading and running code in the same place;
Workshop uploads and Steam chat messages (how mod worms spread); writing into
other mods or NeoModLoader. A serious finding holds the mod until you review
it and allow that version: it's switched off in NeoModLoader's list, kept off
even if another mod needs it (NeoModLoader would switch it back on), and a
changed mod is checked again before it can be switched on.

Three independent reviews (security, logic, and a red team looking for ways
around the scanner) went over it before 0.2.2. It was calibrated on ~240 real WorldBox mods from GameBanana and GitHub: only
NeoModLoader itself and one mod's self-updater (it runs a hidden PowerShell
script to replace itself with whatever its author's server sends) come out
serious. The tests use harmless samples that only name what malware does.
`wbmm scan PATH`, `wbmm check` and `wbmm accept` do it from a terminal.

## Updates

All are on by default and can be turned off in Settings.

- **wbmm updates itself.** It checks GitHub at start and every six hours,
  downloads the new build, checks it against the release's `SHA256SUMS.txt`
  **signed with the release key** (Ed25519, public key built into wbmm; the
  signed file names its version, so an old release can't be passed off as
  new), swaps it in and offers a restart. `wbmm self-update` does it from a terminal.
- **GameBanana mods update themselves.** Same schedule. An update only follows
  the file you installed (ModernBox `m1` never turns into `m5`) and only
  installs when GameBanana's virus scan passed it. Anything held back shows an
  **Update** button under Installed. `wbmm updates -yes` does it from a
  terminal.
- **NeoModLoader updates itself**, same schedule, with WorldBox closed. The
  version it replaces is kept: if mods break, Settings goes back to it, and
  that release is then skipped until a newer one comes out.
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
wbmm scan ./SomeMod.zip        # safety check a folder or archive
wbmm check                     # check every installed mod; hold risky ones
wbmm pack save "Modern war"    # save the installed mods as a modpack
wbmm pack apply war.wbm -only  # install a modpack (-only: switch off the rest)
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

Every push to `main` publishes a release, `v0.3.<n>`: one Linux job builds
both programs (Windows with MinGW) with a cached build, beside the tests, in
about a minute; checksums are signed with the release key.

## License

GPL-3.0
