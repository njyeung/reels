# Config

Reels stores its settings and keybinds in one file:

```bash
~/.config/reels/reels.conf
```

The file is created with platform-appropriate defaults the first time Reels runs. Changes take effect the next time you start Reels.

There are two types of configs used in the app:

- **Settings** are edited directly in `reels.conf`.
- **Keybinds** can be edited with `reels --config` or directly in the file.

## Settings

Settings must be edited directly in `reels.conf`.

| Setting | Default | Description |
|---------|---------|-------------|
| `show_navbar` | `true` | Show the condensed control bar when Reels starts. Toggling the navbar while using Reels updates this value |
| `retina_scale` | `2` on macOS, `1` elsewhere | Scale rendered video for high-density displays. Try `1` if video is too large or rendering is slow |
| `reel_width` | `270` | Initial reel width in pixels |
| `reel_height` | `480` | Initial reel height in pixels |
| `reel_size_step` | `30` | Number of pixels added to or removed from the reel width when resizing. Height changes proportionally |
| `volume` | `1` | Initial volume from `0` (silent) to `1` (full volume). Changing the volume while using Reels updates this value |
| `gif_cell_height` | `5` | Terminal-cell height used for animated GIFs in comments |
| `panel_shrink_steps` | `4` | Number of `reel_size_step` increments by which the reel shrinks when a panel opens |

Resizing the reel, changing the volume, or toggling the navbar while using Reels updates `reels.conf` automatically.

## Keybinds

The quickest way to change keybinds is the built-in editor:

```bash
reels --config
```

You can also edit keybinds by hand.

### Actions

| `reels.conf` bind | Default | Action |
|-------------------|---------|--------|
| `key_next` | `j` | Play the next reel; scroll down when a panel is open |
| `key_previous` | `k` | Play the previous reel; scroll up when a panel is open |
| `key_seek_backward` | `h` | Seek backward 5 seconds |
| `key_seek_forward` | `l` | Seek forward 5 seconds |
| `key_pause` | `p` | Pause or resume playback |
| `key_like` | `space` | Like or unlike the current reel |
| `key_repost` | `r` | Repost or unrepost the current reel |
| `key_save` | `b` | Save or unsave the current reel |
| `key_select` | `space` | Select an item in the share or friends panel; overrides other binds while either panel is open |
| `key_comments_open` | `c` | Open the comments panel |
| `key_comments_close` | `C` | Close the comments panel |
| `key_share_open` | `s` | Open the share panel |
| `key_share_close` | `S` | Send to selected friends and close the share panel |
| `key_friends_open` | `d` | Open the DM friends panel and browse reels shared by friends |
| `key_friends_close` | `D` | Close the DM friends panel or exit friend mode |
| `key_react_open` | `x` | Open the reaction panel in friend mode |
| `key_react_close` | `X` | Close the reaction panel |
| `key_copy_link` | `y` | Copy the current reel's link |
| `key_mute` | `m` | Mute or unmute playback |
| `key_vol_up` | `]` | Increase volume |
| `key_vol_down` | `[` | Decrease volume |
| `key_reel_size_inc` | `=` | Enlarge the reel |
| `key_reel_size_dec` | `-` | Shrink the reel |
| `key_navbar` | `e` | Toggle the condensed control bar |
| `key_help_open` | `?` | Open the help panel |
| `key_help_close` | `?` | Close the help panel |
| `key_quit` | `q`, `ctrl+c` | Quit Reels |

### Key names

Most keys are written as they appear: `j`, `?`, `=`, `enter`, or `ctrl+c`.
Use `space` for the space bar and `escape` for Escape.

### Binding multiple keys

Repeat an action to assign it more than one key:

```ini
key_next = j
key_next = down
```

### Toggling panels

Open and close actions can share a key, turning that key into a toggle. For example:

```ini
key_help_open = ?
key_help_close = ?
```

## Mouse controls

Mouse controls are fixed and cannot be rebound.

| Click target | Action |
|--------------|--------|
| Heart icon | Like or unlike |
| Comment icon | Toggle the comments panel |
| Repost icon | Repost or unrepost |
| Bookmark icon | Save or unsave |
| Link icon | Copy the reel link |
| Caption | Toggle the navbar |
| Reel | Pause or resume playback |
| Row in an open panel | Select that row |
| Scroll wheel | Play the next or previous reel |

## Default config

```ini
# default config (created on first run)

show_navbar = true

# auto detects 2 on macOS, 1 on Linux by default
retina_scale = 2

# reels will be scaled within this bounding box
reel_width = 270
reel_height = 480

reel_size_step = 30
volume = 1
gif_cell_height = 5
panel_shrink_steps = 4

# configurable keybinds
key_next = j
key_previous = k
key_pause = p
key_mute = m
key_like = space
key_repost = r
key_navbar = e
key_vol_up = ]
key_vol_down = [
key_reel_size_inc = =
key_reel_size_dec = -
key_copy_link = y
key_save = b
key_quit = q
key_quit = ctrl+c
key_seek_forward = l
key_seek_backward = h
key_select = space
key_share_open = s
key_share_close = S
key_comments_open = c
key_comments_close = C
key_help_open = ?
key_help_close = ?
key_friends_open = d
key_friends_close = D
key_react_open = x
key_react_close = X
```
