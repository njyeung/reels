# reels

Instagram reels in the terminal.

## Install

```bash
npm install -g @reels/tui
```

## Prerequisites

- **Terminal**: [Kitty](https://sw.kovidgoyal.net/kitty/), [WezTerm](https://wezfurlong.org/wezterm/), or [Konsole](https://konsole.kde.org/) (Kitty graphics protocol required)
- **Browser**: Chrome, Chromium, or Brave
- **FFmpeg**: FFmpeg 8+

## Usage

```bash
reels
```

### Flags
- `--headed` - Run browser in headed mode (visible browser window)
- `--login` - Open browser window to log in to Instagram

### Controls
- `j` / `k` - Next / previous reel
- `Space` - Pause/resume
- `l` - Like/unlike
- `c` - Toggle comments
- `e` - Toggle navbar
- `m` - Mute
- `]` / `[` - Volume up/down
- `=` / `-` - Enlarge/shrink video
- `q` - Quit

All keybinds are configurable in `reels.conf`.

## Supported Platforms

| Platform | Package |
|----------|---------|
| Linux x64 | `@reels/linux-x64` |
| macOS ARM64 | `@reels/darwin-arm64` |

## Links

- [GitHub](https://github.com/njyeung/reels)
- [Homebrew](https://github.com/njyeung/homebrew-tap)
- [AUR](https://aur.archlinux.org/packages/reels-bin)
