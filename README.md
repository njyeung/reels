<p align="center">
  <a href="https://www.npmjs.com/package/@reels/tui"><img src="https://img.shields.io/endpoint?url=https://proud-sun-d44c.nickjyeung.workers.dev&logo=npm" alt="npm"></a>
  <a href="https://aur.archlinux.org/packages/reels-bin"><img src="https://img.shields.io/aur/version/reels-bin" alt="AUR"></a>
  <a href="https://github.com/njyeung/homebrew-tap"><img src="https://img.shields.io/badge/brew-njyeung/tap-orange?logo=homebrew" alt="Homebrew"></a>
  <a href="https://github.com/njyeung/reels/releases/latest"><img src="https://img.shields.io/github/v/release/njyeung/reels" alt="Latest Release"></a>
</p>
<p align="center">
  <a href="https://github.com/njyeung/reels"><img src="https://img.shields.io/github/stars/njyeung/reels" alt="Stars"></a>
  <img src="https://img.shields.io/github/last-commit/njyeung/reels" alt="Last Commit">
  <img src="https://img.shields.io/badge/macOS-supported-blue?logo=apple" alt="macOS">
  <img src="https://img.shields.io/badge/Linux-supported-blue?logo=linux" alt="Linux">
  <a href="https://goreportcard.com/report/github.com/njyeung/reels"><img src="https://goreportcard.com/badge/github.com/njyeung/reels" alt="Go Report Card"></a>
  <img src="https://img.shields.io/github/license/njyeung/reels" alt="License">
</p>

<p align="center">
  <img src="assets/banner.svg" alt="REELS TUI" width="100%">
</p>
<p align="center">
  <img src="assets/demo_popos.gif" width="35%" />
  <img src="assets/demo_macos.gif" width="35%">
  <img src="assets/demo_arch.gif" width="26%" />
</p>
<p align="center">
  <img src="assets/subtitle.svg" alt="Doomscrollbrainrotmaxxing in the terminal" width="500">
</p>

---

## Prerequisites

### Terminal
You need a terminal that supports the **Kitty graphics protocol**:
- [Kitty](https://sw.kovidgoyal.net/kitty/) (most performant)
- [WezTerm](https://wezfurlong.org/wezterm/)
- [Konsole](https://konsole.kde.org/)

### Browser
Chrome, Chromium, or Brave must be installed. The app uses headless browser automation to interact with Instagram.

### FFmpeg
**macOS:** 
Requires [`ffmpeg-full`](https://formulae.brew.sh/formula/ffmpeg-full) from Homebrew - `brew install ffmpeg-full`. The standard `brew install ffmpeg` will **not work**. You may also build FFmpeg 8+ from [`source`](https://github.com/ffmpeg/ffmpeg) or use [`MacPorts`](https://ports.macports.org/port/ffmpeg/), as long as the **Apple framework dependencies (VideoToolbox, AudioToolbox, etc.) are properly included**. The Homebrew install method handles installing `ffmpeg-full` **automatically**; if installing via npm, you must have `ffmpeg-full` with the proper Apple framework dependencies **installed separately** (either via homebrew, source, or MacPorts).

**Linux:** 
Any FFmpeg 8+ from your package manager (e.g. `pacman -S ffmpeg`, `apt install ffmpeg`).

## Usage

```bash
reels
```

### Flags
- `--headed` - Run browser in headed mode (visible browser window)
- `--login` - Open browser window to log in to Instagram

### Controls

| reels.conf bind | Default | Action |
|-----------------|---------|--------|
| `key_next` | `j` | Next reel (scrolls panels when open) |
| `key_previous` | `k` | Previous reel (scrolls panels when open) |
| `key_seek_backward` | `h` | Seek backward |
| `key_seek_forward` | `l` | Seek forward |
| `key_like` | `space` | Like/unlike |
| `key_share_select` | `space` | Select friend in share panel, overrides any other bind while share panel is open |
| `key_pause` | `p` | Pause/resume |
| `key_save` | `b` | Save/Unsave |
| `key_navbar` | `e` | Toggle friendly navbar |
| `key_comments_open` | `c` | Open comments |
| `key_comments_close` | `C` | Close comments |
| `key_share_open` | `s` | Open share panel |
| `key_share_close` | `S` | Close Share panel & sends to friends' DMs (if any are selected) |
| `key_copy_link` | `y` | Copy reel link to clipboard |
| `key_mute` | `m` | Mute |
| `key_vol_up` | `]` | Volume up |
| `key_vol_down` | `[` | Volume down |
| `key_reel_size_inc` | `=` | Enlarge video |
| `key_reel_size_dec` | `-` | Shrink video |
| `key_help_open` | `?` | Help panel shows the current keybinds |
| `key_help_close`| `?` | Close help panel |
| `key_quit` | `q` | Quit |
| `key_quit` | `ctrl+c` | Quit |

All keybinds are configurable in `reels.conf`. Each action supports multiple binds. Open/close pairs (like `key_comments_open` and `key_comments_close`) can be bound to the same key to toggle.

## Installation

### npm (macOS ARM64 / Linux x86_64 & ARM64)

**macOS** *requires [Homebrew](https://brew.sh)*:
```bash
# brew install ffmpeg-full    # (or build from source or MacPorts)
npm install -g @reels/tui
reels
```

**Linux:**
```bash
# sudo pacman -S ffmpeg      # Arch
# sudo apt install ffmpeg    # Debian/Ubuntu
npm install -g @reels/tui
reels
```

### Homebrew (macOS ARM64 / Linux x86_64 & ARM64)

```bash
brew tap njyeung/tap
brew install reels
reels
```

### AUR (Arch Linux x86_64 & ARM64)

```bash
sudo pacman -Syu ffmpeg # make sure you're on ffmpeg 8+
yay -S reels-bin
reels
```

### Pre-built Binaries

Download the latest release from [GitHub Releases](https://github.com/njyeung/reels/releases):

| Platform | Binary |
|----------|--------|
| Linux (x86_64) | `reels-linux-amd64` |
| Linux (ARM64) | `reels-linux-arm64` |
| macOS (Apple Silicon) | `reels-darwin-arm64` |

**macOS:** Requires `ffmpeg-full` from Homebrew - `brew install ffmpeg-full`. The standard `brew install ffmpeg` is missing framework link flags needed for compilation. You may build from source or use MacPorts if you know what you're doing.

**Linux:** Requires FFmpeg 8+ (e.g. `sudo pacman -S ffmpeg` on Arch, `sudo apt install ffmpeg` on Debian/Ubuntu)

### Building from Source

Requires Go 1.25+ and FFmpeg 8+ development libraries (ffmpeg-full works for macOS).

```bash
git clone https://github.com/njyeung/reels.git
cd reels
go build -o reels .
```

## File Paths

- Settings: `~/.config/reels/reels.conf`
- Cache: `~/.cache/reels/`
- Chrome Data: `~/.local/shared/reels/`

## Default settings

```
# Default config (created on first run)

show_navbar = true
retina_scale = 2    # auto detects 2 on macOS, 1 on Linux by default
reel_width = 270
reel_height = 480
reel_size_step = 30
volume = 1
gif_cell_height = 5
panel_shrink_steps = 4  # how many reel_size_steps to shrink when opening a panel

# Configurable keybinds (multiple binds per action supported)
key_next = j
key_previous = k
key_pause = p
key_mute = m
key_like = space
key_navbar = e
key_vol_up = ]
key_vol_down = [
key_reel_size_inc = =
key_reel_size_dec = -
key_copy_link = y
key_save = b
key_seek_forward = l
key_seek_backward = h
key_share_open = s
key_share_close = S
key_share_select = space
key_comments_open = c
key_comments_close = C
key_help_open = ?
key_help_close = ?
key_quit = q
key_quit = ctrl+c
```
