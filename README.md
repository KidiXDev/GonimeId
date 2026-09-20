<p align="center">
    <a href="https://github.com/KidiXDev/GonimeId/blob/main/LICENSE"><img src="https://img.shields.io/github/license/KidiXDev/GonimeId" alt="License"></a>
    <img src="https://img.shields.io/github/last-commit/KidiXDev/GonimeId" alt="Last commit">
    <a href="https://github.com/KidiXDev/GonimeId/actions"><img src="https://github.com/KidiXDev/GonimeId/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
</p>

# GonimeId

GonimeId is a terminal app (TUI) for watching and downloading **Indonesian-subtitled anime** in mpv.
Search once, pick a title, pick an episode, pick a resolution — it plays.

## Sources

| Source                                  | Resolutions  | Notes                                                                                 |
| --------------------------------------- | ------------ | ------------------------------------------------------------------------------------- |
| [Otakudesu](https://otakudesu.blog)     | 360p – 1080p | Download-section files (Pixeldrain) are preferred; streaming mirrors are the fallback |
| [Samehadaku](https://v2.samehadaku.how) | 360p – 1080p | Pixeldrain servers, Blogspot fallback                                                 |
| [Nimegami](https://nimegami.id)         | 360p – 1080p | Direct berkasdrive files; one page per season                                         |
| [YLnime](https://ylnime.com)            | 360p – 1080p | Direct MP4/HLS mirrors                                                                |

All four are searched together by default. Dead or geo-locked files are probed and
skipped before anything reaches mpv.

## Features

- Search across all sources, one picker
- Resolution picker (remembered for the session) or `--quality 1080p`
- Play in mpv with skip-intro/outro (AniSkip) and resume tracking (SQLite build)
- Download single episodes, ranges, or everything — Plex/Jellyfin folder naming
- Real-time Anime4K upscaling in mpv
- Discord Rich Presence

## Prerequisites

- [mpv](https://mpv.io/) — required
- `ffmpeg`/`ffprobe` — optional, only for some downloads

```bash
# Arch
sudo pacman -S mpv ffmpeg
# Debian
sudo apt install mpv ffmpeg
# macOS
brew install mpv ffmpeg
```

## Install

From source (Go 1.25+):

```bash
go install github.com/KidiXDev/GonimeId/cmd/gonimeid@latest
```

Or clone and build:

```bash
git clone https://github.com/KidiXDev/GonimeId.git
cd GonimeId
CGO_ENABLED=0 go build -o gonimeid ./cmd/gonimeid
```

With watch-progress tracking (needs SQLite headers): `cd build && ./buildlinux-with-sqlite.sh`.

## Usage

```bash
gonimeid                       # interactive: search → title → episode → quality → play
gonimeid "one piece"           # search directly (use spaces, not hyphens)
gonimeid --source nimegami "frieren"    # otakudesu | samehadaku | nimegami | ylnime
gonimeid --quality 1080p "frieren"       # download default; playback still asks per source

gonimeid -d "one piece" 1      # download episode 1
gonimeid -d -r "naruto" 1-12   # download a range
gonimeid -d -a "frieren"       # download everything
gonimeid -d -o ~/Anime "bleach" 3

gonimeid --upscale video.mp4   # Anime4K upscale a file
gonimeid --update              # self-update from GitHub releases
gonimeid --help
```

In the play menu, **Play** is the first item; Esc goes back one step everywhere.

### Environment

| Variable                              | Effect                                                 |
| ------------------------------------- | ------------------------------------------------------ |
| `GONIMEID_DISABLED_SOURCES=Otakudesu` | turn a source off without rebuilding (comma-separated) |
| `GONIMEID_ENABLED_SOURCES=…`          | opt in to a source shipped disabled                    |
| `GONIMEID_STRICT_SOURCE=1`            | never fall back to another source                      |

Debug logs: run with `--debug`; the log path is printed at startup
(`~/.local/share/gonimeid/logs/` on Linux).

## Troubleshooting

**Nothing plays / bounces back to the episode list** — run with `--debug` and
read the lines after `Loading episode...`. Usually the chosen file was removed
upstream; pick another resolution or the other source.

**Search returns nothing for a multi-word title** — type it with spaces
(`"one piece"`), not hyphens.

**TLS errors behind a corporate proxy / custom CA** — point Go at the CA bundle:

```bash
export SSL_CERT_FILE=/path/to/corporate-ca.pem
export SSL_CERT_DIR=/path/to/ca-certificates.d
```

## Contributing

See [docs/Development.md](docs/Development.md) and
[docs/ADDING_A_SOURCE.md](docs/ADDING_A_SOURCE.md)

## Credits

- [GoAnime](https://github.com/alvarorichard/GoAnime) — the upstream project this fork is built on
- [mpv](https://mpv.io/), [Anime4K](https://github.com/bloc97/Anime4K), [AniSkip](https://api.aniskip.com/)

## License

MIT — see [LICENSE](LICENSE).
