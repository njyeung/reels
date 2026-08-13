# Keybinds

Every action in Reels is rebindable. Bindings live in `~/.config/reels/reels.conf`,
which is created with the defaults below on first run.

The quickest way to change them is the built-in editor:

```bash
reels --config
```

It writes the same file, so you can mix and match with editing by hand.

## How binding works

- **Multiple binds per action.** Repeat the same key to bind several:

  ```ini
  key_next = j
  key_next = down
  ```

- **Toggles.** Open/close pairs can share a key. Bind `key_help_open` and
  `key_help_close` to `?` and the panel toggles on the same press.

- **Key names** are written as you'd expect — `j`, `?`, `=`, `ctrl+c`. Two keys have
  spelled-out names because they can't appear literally in the config: `space` and
  `escape`.

## Actions

| reels.conf bind | Default | Action |
|-----------------|---------|--------|
| `key_next` | `j` | Next reel (scrolls panels when open) |
| `key_previous` | `k` | Previous reel (scrolls panels when open) |
| `key_seek_backward` | `h` | Seek backward by 5 seconds |
| `key_seek_forward` | `l` | Seek forward by 5 seconds |
| `key_like` | `space` | Like/unlike |
| `key_repost` | `r` | Repost/unrepost current reel |
| `key_select` | `space` | Select friend in share/friends panel. Overrides any other bind while either panel is open |
| `key_pause` | `p` | Pause/resume current reel |
| `key_save` | `b` | Save/Unsave (bookmark) current reel |
| `key_navbar` | `e` | Toggle navbar, a condensed version of the help menu |
| `key_comments_open` | `c` | Open comments |
| `key_comments_close` | `C` | Close comments |
| `key_share_open` | `s` | Open share panel. Allows you to share reels with Instagram's suggested top friends |
| `key_share_close` | `S` | Close share panel & send to friends' DMs (if any are selected) |
| `key_friends_open` | `d` | Open DM friends panel to view reels shared by friends |
| `key_friends_close` | `D` | Close DM friends panel / exit friend mode |
| `key_react_open` | `x` | Open react panel to react to a friend's reel (friend mode only) |
| `key_react_close` | `X` | Close react panel (friend mode only) |
| `key_copy_link` | `y` | Copy reel link to clipboard |
| `key_mute` | `m` | Mute current reel |
| `key_vol_up` | `]` | Volume up |
| `key_vol_down` | `[` | Volume down |
| `key_reel_size_inc` | `=` | Enlarge video |
| `key_reel_size_dec` | `-` | Shrink video |
| `key_help_open` | `?` | Open help panel, showing the current keybinds |
| `key_help_close` | `?` | Close help panel |
| `key_quit` | `q` | Quit |
| `key_quit` | `ctrl+c` | Quit |

## Mouse

Mouse support isn't rebindable — it maps onto whatever the actions above are bound to.

| Click target | Action |
|--------------|--------|
| Heart icon | Like/unlike |
| Comment icon | Toggle the comments panel |
| Repost icon | Repost/unrepost |
| Bookmark icon | Save/unsave |
| Link icon | Copy reel link |
| Caption | Toggle the navbar |
| The reel itself | Pause/resume |
| A row in any open panel | Select |
| Scroll wheel | Next/previous reel |

## Defaults

The keybind portion of a freshly generated `reels.conf`:

```ini
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
key_seek_forward = l
key_seek_backward = h
key_share_open = s
key_share_close = S
key_select = space
key_friends_open = d
key_friends_close = D
key_react_open = x
key_react_close = X
key_comments_open = c
key_comments_close = C
key_help_open = ?
key_help_close = ?
key_quit = q
key_quit = ctrl+c
```

For non-keybind settings (reel size, volume, navbar, retina scale), see
[Configuration](README.md#configuration).
