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

**FFmpeg 7+ is required.** The default packages on Ubuntu/Debian are too old.

#### Linux (Debian/Ubuntu)

Ubuntu/Debian ship with FFmpeg 4-6, but this project requires FFmpeg 7+. Use the [ubuntuhandbook1 PPA](https://launchpad.net/~ubuntuhandbook1/+archive/ubuntu/ffmpeg7):

```bash
sudo add-apt-repository ppa:ubuntuhandbook1/ffmpeg7
sudo apt update
sudo apt install ffmpeg libavformat-dev libavcodec-dev libswresample-dev libswscale-dev libasound2-dev
```

#### Linux (Fedora/RHEL)

FFmpeg 7+ is available via [RPM Fusion](https://rpmfusion.org/):

```bash
sudo dnf install \
  https://download1.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm
sudo dnf install ffmpeg ffmpeg-devel alsa-lib-devel
```

#### Linux (Arch)

Arch ships FFmpeg 8+ in the official repos:

```bash
sudo pacman -S ffmpeg alsa-lib
```

#### macOS
```bash
brew install ffmpeg
```

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
