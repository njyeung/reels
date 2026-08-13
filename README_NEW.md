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
  <img src="https://img.shields.io/github/license/njyeung/reels" alt="License">
</p>

<p align="center">
  <img src="assets/banner.svg" alt="REELS TUI" width="100%">
</p>
<p align="center">
  <img src="assets/subtitle.svg" alt="Doomscrollbrainrotmaxxing in the terminal" width="500">
</p>

<!-- SHOT 01 — hero. Placeholder until the shoot lands. -->
<p align="center">
  <img src="assets/hero.gif" alt="Reels TUI playing an Instagram reel in a terminal" width="100%">
</p>

---

Reels TUI plays your real Instagram feed inside your terminal — actual video, actual
audio, rendered through the Kitty graphics protocol. Like, comment, repost, save,
share reels into your friends' DMs, and react to the ones they send you, without ever
leaving the terminal.

It's fully keyboard driven. Or mouse driven, if you'd rather click the heart.

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

### Pre-built binaries

Download the latest release from [GitHub Releases](https://github.com/njyeung/reels/releases):

| Platform | Binary |
|----------|--------|
| Linux (x86_64) | `reels-linux-amd64` |
| Linux (ARM64) | `reels-linux-arm64` |
| macOS (Apple Silicon) | `reels-darwin-arm64` |

> You'll need a terminal that supports the Kitty graphics protocol — Kitty, Ghostty,
> WezTerm, iTerm2, and st all work. See [Troubleshooting](#troubleshooting) for the
> full list.

## First run

Reels drives a real browser session, so the first launch is a one-time login.

**1. Start with the login flag.**

```bash
reels --login
```

A browser window opens. Log in to Instagram as you normally would.

**2. Click "Save Info" when Instagram offers.**

This is the step people miss. Instagram asks whether to save your login info once
you're through — click **Save Info**. Reels reuses that saved session on every later
launch, and skipping it means you'll land back on a login screen next time.

**3. Restart without the flag.**

```bash
reels
```

That's it — you're in the feed. If something went wrong, see below.

## Troubleshooting

<details>
<summary><b>"Syncing failed", or startup hangs before the feed appears</b></summary>

<br>

Nearly always means the saved session didn't stick, even if you clicked **Save Info**.

Relaunch with the browser visible:

```bash
reels --headed
```

If the window is sitting on an Instagram login screen, that's your answer — the
session wasn't persisted, and Reels can't log itself in. Redo the [first run](#first-run)
steps and confirm you click **Save Info**.

</details>

<details>
<summary><b>My terminal shows garbage, or no video at all</b></summary>

<br>

Reels needs a terminal that speaks the **Kitty graphics protocol**:

- [Kitty](https://sw.kovidgoyal.net/kitty/) (recommended)
- [Ghostty](https://ghostty.org/) (recommended)
- [WezTerm](https://wezfurlong.org/wezterm/) (recommended)
- [iTerm2](https://iterm2.com/) (recommended)
- [st](https://st.suckless.org/) (recommended)
- [Konsole](https://konsole.kde.org/)
- [Warp](https://www.warp.dev/)
- [wayst](https://github.com/91861/wayst)

Terminals outside this list (GNOME Terminal, Alacritty, the macOS default) can't
display images and won't work.

</details>

<details>
<summary><b>Linux ARM64: Chrome won't download</b></summary>

<br>

Chrome is fetched automatically on first run when no system Chrome or Chromium is
found, so most platforms need nothing. The exception is Linux ARM64, where Chrome For
Testing isn't published yet
([coming Q2 2026](https://blog.chromium.org/2026/03/bringing-chrome-to-arm64-linux-devices.html)).

On Linux ARM64, install Chrome, Chromium, or Brave manually before running Reels.

</details>

<details>
<summary><b>Nothing works and I want to start clean</b></summary>

<br>

Wipe the browser profile and log in again:

```bash
rm -rf ~/.local/share/reels/
reels --login
```

</details>

## Features

<!-- SHOTS 02-09 — placeholders until the shoot lands. -->

<table>
<tr>
<td width="50%">

**Scroll the feed**

`j` / `k` to move, or just use the scroll wheel.

<img src="assets/scroll.gif" alt="Scrolling the feed" width="100%">

</td>
<td width="50%">

**Seek within a reel**

`l` jumps forward 5 seconds, `h` goes back.

<img src="assets/seek.gif" alt="Seeking within a reel" width="100%">

</td>
</tr>
<tr>
<td width="50%">

**Like, repost, save**

`space` to like, `r` to repost, `b` to bookmark. `y` copies the link.

<img src="assets/hud.gif" alt="Liking, reposting and saving a reel" width="100%">

</td>
<td width="50%">

**Comments**

`c` opens the panel, `C` closes it. Expand any comment to read its replies — the reel
keeps playing the whole time.

<img src="assets/comments.gif" alt="Reading comments" width="100%">

</td>
</tr>
<tr>
<td width="50%">

**Mouse support**

Click the heart to like. Click the reel to pause. Click the caption to toggle the
navbar.

<img src="assets/mouse.gif" alt="Using the mouse" width="100%">

</td>
<td width="50%">

**Share to DMs**

`s` opens your suggested friends, `space` selects, `S` sends and closes.

<img src="assets/share.gif" alt="Sharing a reel to DMs" width="100%">

</td>
</tr>
<tr>
<td width="50%">

**Watch what friends sent you**

`d` opens your chats. Reels shared with you play with a blue border and your friend's
profile picture.

<img src="assets/dm.gif" alt="Watching reels shared by friends" width="100%">

</td>
<td width="50%">

**React to their reels**

`x` opens the react panel. Reactions float over the reel as animated emoji.

<img src="assets/react.gif" alt="Reacting to a friend's reel" width="100%">

</td>
</tr>
</table>

## Usage

```bash
reels
```

| Flag | Effect |
|------|--------|
| `--login` | Opens a visible browser to log in to Instagram. Reels won't drive the browser in this mode, which also makes it useful for debugging |
| `--headed` | Runs the browser visibly while Reels still controls it. Use this to diagnose sync failures |
| `--config` | Opens the keybind editor. Doesn't launch a browser |
| `--version` | Prints the version and exits |

## Keybinds

Every action is rebindable, and each one accepts multiple bindings. Open/close pairs
can share a key to make it a toggle.

Edit them in the TUI:

```bash
reels --config
```

<!-- SHOT 10 — placeholder until the shoot lands. -->
<p align="center">
  <img src="assets/config.gif" alt="Editing keybinds with reels --config" width="80%">
</p>

Or edit `~/.config/reels/reels.conf` by hand.

**See [KEYBINDS.md](KEYBINDS.md) for the full list of actions and their defaults.**

## Configuration

Settings live in `~/.config/reels/reels.conf`, created on first run.

```ini
show_navbar = true
retina_scale = 2        # auto-detects 2 on macOS, 1 on Linux
reel_width = 270
reel_height = 480
reel_size_step = 30
volume = 1
gif_cell_height = 5
panel_shrink_steps = 4  # reel_size_steps to shrink by when a panel opens
```

Keybinds go in the same file — see [KEYBINDS.md](KEYBINDS.md).

## File paths

| What | Where |
|------|-------|
| Settings | `~/.config/reels/reels.conf` |
| Cache | `~/.cache/reels/` |
| Browser data | `~/.local/share/reels/` |
| Logs | `~/.local/state/reels/reels.log` |

## Building from source

Requires Go 1.25+ and FFmpeg 8+ development libraries.

Pre-built binaries ship with FFmpeg statically linked. For development, dynamically
linking against a system FFmpeg makes building and iteration faster (simply
`go build -o reels`). You can still build using Docker, but I highly recommend
installing the correct versions of FFmpeg following the directions below:

**macOS:** Requires `ffmpeg-full` from [Homebrew](https://brew.sh)
(`brew install ffmpeg-full`), [MacPorts](https://ports.macports.org/port/ffmpeg/), or
FFmpeg 8+ built from [source](https://github.com/ffmpeg/ffmpeg). The standard
`brew install ffmpeg` is missing required framework link flags.

**Linux:** Requires FFmpeg 8+ development libraries from your package manager (e.g.
`sudo pacman -S ffmpeg` on Arch, `sudo apt install ffmpeg` on Debian/Ubuntu). This
usually works fine as long as your packages are updated.

```bash
# brew install ffmpeg-full      on macOS
# sudo apt install ffmpeg       on Linux
# ffmpeg -version               should be 8+
git clone https://github.com/njyeung/reels.git
cd reels
go build -o reels .
```

See [CONTRIBUTING.md](CONTRIBUTING.md) to get started.

---

<!-- SHOT 11 — credibility strip. Reusing existing captures for now. -->
<p align="center">
  <img src="assets/demo_popos.gif" width="32%" />
  <img src="assets/demo_macos.gif" width="32%">
  <img src="assets/demo_arch.gif" width="24%" />
</p>
<p align="center">
  <sub>Pop!_OS · macOS · Arch</sub>
</p>
