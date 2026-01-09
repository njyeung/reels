TUI for instagram reels. Doomscrollbrainrotmaxxing

## Prerequisites

### Terminal
You need a terminal that supports the **Kitty graphics protocol**:
- [Kitty](https://sw.kovidgoyal.net/kitty/) (recommended)
- [WezTerm](https://wezfurlong.org/wezterm/)
- [Konsole](https://konsole.kde.org/) (experimental support)

### Browser
Chrome, Chromium, or Brave must be installed. The app uses headless browser automation to interact with Instagram.

### System Dependencies

#### Linux (Debian/Ubuntu)
```bash
sudo apt update
sudo apt install ffmpeg libavformat-dev libavcodec-dev libswresample-dev libswscale-dev libasound2-dev
```

#### Linux (Fedora/RHEL)
```bash
sudo dnf install ffmpeg ffmpeg-devel alsa-lib-devel
```

#### Linux (Arch)
```bash
sudo pacman -S ffmpeg alsa-lib
```

#### macOS
```bash
brew install ffmpeg
```
Audio works out of the box via Core Audio.

### Building

Requires Go 1.23+:

```bash
go build -o reels .
```

## Usage

```bash
./reels
```

### Controls
- `j` / `↓` - Next reel
- `k` / `↑` - Previous reel
- `Space` - Pause/resume
- `l` - Like/unlike
- `q` - Quit
