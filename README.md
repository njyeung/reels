<p align="center">
  <img src="assets/banner.svg" alt="REELS TUI" width="100%">
</p>

<p align="center">
  <img src="assets/popos-demo.gif" alt="Reels TUI playing on PopOS" width="33%">
  <img src="assets/macos-demo.gif" alt="Reels TUI playing on MacOS" width="33%">
  <img src="assets/arch-demo.gif" alt="Reels TUI playing on Arch Linux" width="33%">
</p>

<br>

<h3 align="center">
  Reels TUI brings the full Instagram Reels experience to your terminal. Scroll your feed, browse comments, interact with your friends, and more!
</h3>

<p align="center">
  <a href="https://github.com/njyeung/reels"><img src="https://img.shields.io/github/stars/njyeung/reels" alt="Stars"></a>
  <a href="https://www.npmjs.com/package/@reels/tui"><img src="https://img.shields.io/endpoint?url=https://proud-sun-d44c.nickjyeung.workers.dev&logo=npm" alt="npm"></a>
  <a href="https://aur.archlinux.org/packages/reels-bin"><img src="https://img.shields.io/aur/version/reels-bin" alt="AUR"></a>
  <a href="https://github.com/njyeung/homebrew-tap"><img src="https://img.shields.io/badge/brew-njyeung/tap-orange?logo=homebrew" alt="Homebrew"></a>
  <a href="https://github.com/njyeung/reels/releases/latest"><img src="https://img.shields.io/github/v/release/njyeung/reels" alt="Latest Release"></a>
</p>
<p align="center">
  <img src="https://img.shields.io/github/last-commit/njyeung/reels" alt="Last Commit">
  <img src="https://img.shields.io/badge/macOS-supported-blue?logo=apple" alt="macOS">
  <img src="https://img.shields.io/badge/Linux-supported-blue?logo=linux" alt="Linux">
  <img src="https://img.shields.io/github/license/njyeung/reels" alt="License">
</p>

<br>

## Installation

### npm (macOS ARM64 / Linux x86_64 & ARM64)

```bash
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
yay -S reels-bin
reels
```

## Usage

```bash
reels
```

| Flag | Effect |
| --- | --- |
| `--login` | Opens a visible browser to log in to Instagram. |
| `--headed` | Runs the browser visibly while Reels still controls it. Use this to diagnose sync failures |
| `--config` | Opens the keybind editor. Doesn't launch a browser |
| `--version` | Prints the version and exits | 

## Terminal
You need a terminal that supports the **Kitty graphics protocol**:
- [Kitty](https://sw.kovidgoyal.net/kitty/) (recommended)
- [Ghostty](https://ghostty.org/) (recommended)
- [WezTerm](https://wezfurlong.org/wezterm/) (recommended)
- [iTerm2](https://iterm2.com/) (recommended)
- [st](https://st.suckless.org/) (recommended)
- [Konsole](https://konsole.kde.org/)
- [Warp](https://www.warp.dev/)
- [wayst](https://github.com/91861/wayst)

## Features

<table>
<tr>
<td width="50%">

**Like, repost, save**

`space` to like, `r` to repost, `b` to bookmark, `y` to copy the link. Or reach for the mouse and click the icons directly.

<video src="https://github.com/user-attachments/assets/2e05b6a6-cd42-4068-9ac9-c2613fe5b389" width="100%" muted autoplay loop playsinline controls>
</video>

</td>
<td width="50%">

**Comments**

`c` opens the panel, so you can read comments and laugh at the gifs. Expand any comment to read its replies.

<video src="https://github.com/user-attachments/assets/d3f006c5-70ad-45e3-ab43-7e3b84ec9284" width="100%" muted autoplay loop playsinline controls>
</video>

</td>
</tr>
<tr>
<td width="50%">

**Share to DMs**

`s` opens your suggested friends, `space` selects, `S` closes the panel and sends the reel to the selected friends.

<video src="https://github.com/user-attachments/assets/b08d6107-fbb1-44ef-b6cd-8f73bd6f244c" width="100%" muted autoplay loop playsinline controls>
Sharing a reel to DMs
</video>

</td>
<td width="50%">

**Watch and react to what friends sent you**

`d` to view reels your friends have shared with you. `x` to send a reaction back to their dms.

<video src="https://github.com/user-attachments/assets/ede9f0d2-b51b-49f5-b067-84fc8e2cc028" width="100%" muted autoplay loop playsinline controls>
</video>

</td>
</tr>
</table>

## Config

All keybinds are configurable. Each action supports multiple binds. Open/close pairs can be bound to the same key to toggle.

The config TUI is a wrapper for `~/.config/reels/reels.conf` (which may also be edited by hand):

```bash
reels --config
```

<table align="center">
<tr>
<td>
<video src="https://github.com/user-attachments/assets/2111b13a-ac95-4b29-95dd-b145f4ff5e17" width="50%">
</video>
</td>
</tr>
</table>


**See [CONFIG.md](CONFIG.md) for the full list of settings and their defaults.**

## File paths

| What | Where |
|------|-------|
| Settings | `~/.config/reels/reels.conf` |
| Cache | `~/.cache/reels/` |
| Browser data | `~/.local/share/reels/` |
| Logs | `~/.local/state/reels/reels.log` |


## Pre-built Binaries
Download the latest release from [GitHub Releases](https://github.com/njyeung/reels/releases):

| Platform | Binary |
|----------|--------|
| Linux (x86_64) | `reels-linux-amd64` |
| Linux (ARM64) | `reels-linux-arm64` |
| macOS (Apple Silicon) | `reels-darwin-arm64` |

## Building from source (For Developers)

Requires Go 1.25+ and FFmpeg 8+ development libraries.

Pre-built binaries ship with FFmpeg statically linked. For development, dynamically linking against a system FFmpeg makes building and iteration faster (simply `go build -o reels`). You can still build using docker, but I highly recommend installing the correct versions of FFmpeg following the directions below:

**macOS:** Requires `ffmpeg-full` from [Homebrew](https://brew.sh) (`brew install ffmpeg-full`), [MacPorts](https://ports.macports.org/port/ffmpeg/), or FFmpeg 8+ built from [source](https://github.com/ffmpeg/ffmpeg). The standard `brew install ffmpeg` is missing required framework link flags.

**Linux:** Requires FFmpeg 8+ development libraries from your package manager (e.g. `sudo pacman -S ffmpeg` on Arch, `sudo apt install ffmpeg` on Debian/Ubuntu). This usually works fine as long as your packages are updated.

```bash
# brew install ffmpeg-full      on macOS
# sudo apt install ffmpeg       on Linux
# ffmpeg -version               should be 8+
git clone https://github.com/njyeung/reels.git
cd reels
go build -o reels .
```

## Troubleshooting

<details>
<summary><b>My terminal shows no video at all</b></summary>

<br>

It is likely that your terminal does not properly support the **Kitty graphics protocol**. See [Terminal](#Terminal).

</details>


<details>
<summary><b>"could not complete initial sync" on startup</b></summary>

<br>

Almost always means the saved session didn't stick, even if you clicked **Save Info**.

Diagnose by relaunch with the browser visible:

```bash
reels --headed
```

If the window is sitting on an Instagram login screen, log in and click **Save Info** again.

</details>


<details>
<summary><b>Linux ARM64: Chrome doesn't download</b></summary>

<br>

Chrome is automatically downloaded on first run if no system Chrome/Chromium is found; No action is needed for most platforms. The exception is Linux ARM64, where Chrome For Testing isn't available yet ([coming Q2 2026!](https://blog.chromium.org/2026/03/bringing-chrome-to-arm64-linux-devices.html)).

On Linux ARM64, install Chrome, Chromium, or Brave manually before running Reels.

</details>


<details><summary><b>High CPU usage</b></summary>

<br>

Try using one of the [recommended terminals](#terminal). These terminals support reading image data from a shared memory object rather than having Reels TUI base64 encode and print it to stdout.

</details>


<details>
<summary><b>Nothing works</b></summary>

<br>

Sometimes, the Chrome profile may be left in an unrecoverable state. Wipe the chrome-data and restart:

```bash
rm -rf ~/.local/share/reels/
reels
```

</details>

<p align="center">
  <img src="assets/subtitle.svg" alt="Doomscrollbrainrotmaxxing in the terminal" width="500">
</p>
