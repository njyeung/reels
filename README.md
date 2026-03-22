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
  <pre align="center">    
██████╗ ███████╗███████╗██╗     ███████╗    ████████╗██╗   ██╗██╗
██╔══██╗██╔════╝██╔════╝██║     ██╔════╝    ╚══██╔══╝██║   ██║██║
██████╔╝█████╗  █████╗  ██║     ███████╗       ██║   ██║   ██║██║
██╔══██╗██╔══╝  ██╔══╝  ██║     ╚════██║       ██║   ██║   ██║██║
██║  ██║███████╗███████╗███████╗███████║       ██║   ╚██████╔╝██║
╚═╝  ╚═╝╚══════╝╚══════╝╚══════╝╚══════╝       ╚═╝    ╚═════╝ ╚═╝
  </pre>
</p>
<p align="center">
  <em>Doomscrollbrainrotmaxxing in the terminal.</em>
</p>

---

<p align="center">
  <img src="screenshot-2.png" width="41%">
  <img src="demo.gif" width="25%" />
  <img src="screenshot-1.png" width="27%" />
</p>

---

## Prerequisites

### Terminal
You need a terminal that supports the **Kitty graphics protocol**:
- [Kitty](https://sw.kovidgoyal.net/kitty/) (recommended — most performant)
- [WezTerm](https://wezfurlong.org/wezterm/)
- [Konsole](https://konsole.kde.org/)

### Browser
Chrome, Chromium, or Brave must be installed. The app uses headless browser automation to interact with Instagram.

### FFmpeg
**macOS:** 
Requires [`ffmpeg-full`](https://formulae.brew.sh/formula/ffmpeg-full) from Homebrew - `brew install ffmpeg-full`. The standard `brew install ffmpeg` will **not** work due to missing framework dependencies. You may also build FFmpeg 8+ from [`source`](https://github.com/ffmpeg/ffmpeg) or use [`MacPorts`](https://ports.macports.org/port/ffmpeg/), as long as the **Apple framework dependencies (VideoToolbox, AudioToolbox, etc.) are properly included**. The Homebrew install method handles installing `ffmpeg-full` **automatically**; if installing via npm, you must have `ffmpeg-full` with the proper Apple framework dependencies **installed separately** (either via homebrew, source, or MacPorts).

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
- `j` - Next reel (scroll comments when open)
- `k` - Previous reel (scroll comments when open)
- `Space` - Pause/resume
- `l` - Like/unlike
- `e` - Toggle Navbar
- `c` - Toggle Comments
- `m` - Mute
- `]` - Volume up
- `[` - Volume down
- `s` - Share reel via DM
- `y` - Copy reel link to clipboard
- `=` - Enlarge Video
- `-` - Shrink Video
- `?` - Help
- `q` - Quit

All keybinds are configurable in `reels.conf`. Each action supports multiple binds.

## Installation

### npm (macOS ARM64 / Linux x86_64)

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

### Homebrew (macOS ARM64 / Linux x86_64)

```bash
brew tap njyeung/tap
brew install reels
reels
```

### AUR (Arch Linux x86_64)

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
| macOS (Apple Silicon) | `reels-darwin-arm64` |

**macOS:** Requires `ffmpeg-full` from Homebrew — `brew install ffmpeg-full`, or FFmpeg 8+ built from source with shared libraries enabled. The standard `brew install ffmpeg` is missing framework link flags needed for compilation.

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
key_pause = space
key_mute = m
key_like = l
key_comments = c
key_navbar = e
key_vol_up = ]
key_vol_down = [
key_reel_size_inc = =
key_reel_size_dec = -
key_share = s
key_copy_link = y
key_quit = q
key_quit = ctrl+c
```
