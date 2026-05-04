# Changelog

## [1.3.0]
- View Reels shared by friends in DMs
- Renamed key_share_select to key_select, unified key for share and dm menu
- Fix: bug with prefetching reels

## [1.2.11]
- Add automatic self-update for npm-installed binaries on launch

## [1.2.10]
- Color @mentions blue in captions and comments
- Add heart/repost badge icons on floating reaction profile pictures
- Fix: resizing reel now repositions comment gifs
- Fix: Update Instagram comments pagination doc_id to match new frontend

## [1.2.9]
- Add reposting reels (default r)
- Show friend who have reposted/liked the current reel
- Fix: Update Instagram comments pagination doc_id to match new frontend
- Fix: fallback when mp4s have no audio stream
- Fix: video centering off-by-one
- Fix: colors 

## [1.2.8]
- Auto install Chrome if not found on system (except for Linux ARM64)
- Black box test harness, does not introduce any new code into user binary

## [1.2.7]
- Statically link ffmpeg for Linux and macOS
- No longer requires ffmpeg as a prerequisite

## [1.2.6]
- Adding seeking and progress overlay
- Add separate open/close binds for comments, share, and help panels. Configurable in reels.conf 
- Updated colors to align with instagram's colors a bit more
- Fix: video dimension and position calculations (off by one)
- Fix: prefetch index
- Fix: Removed redundant calls to updateVideoPosition

## [1.2.5]
- Add arm Linux support
- Add save button
- Optimize shared memory writing
- Fix: Disable actions (liking, opening panels) triggering while SyncTo is working
- Fix: Instagram's new send button
- Fix: comment prefetching to adjust to new comments layout
- Fix: video centering on non-16:9 reels
- Fix: video ready signal not firing at the right time

## [1.2.4]

- Use @rpath for macOS FFmpeg linking to support broader install locations
- Fix rendering loop responsiveness when paused for gifs and images
- Fix spelling

## [1.2.3]

- Fix loading text jitter
- Update loading messages

## [1.2.2]

- Add loading screen
- Add shared memory rendering package for macOS
- Fix video offset persistence
- Fix flickering profile pictures

## [1.2.1]

- Add help panel

## [1.2.0]

- Add DM sharing to friends
- Refactor image pipeline (profile pictures, frame pruning)
- Add comments pagination with loading indicator
- Unify file paths for macOS and Linux

## [1.1.7]

- Fix rendering bug
- Optimize profile picture rendering

## [1.1.6]

- Add sharing via link
- Add gif comment support
- Fix comments
- Fix cleanup race condition
- Refactor rendering loop

## [1.1.5]

- Add shared memory rendering for terminals with kitty protocol support
- Add user-defined keybinds via reels.conf
- Add npm package
- Fix shared memory cleanup on quit

## [1.1.4]

- Clean up comments UI

## [1.1.3]

- Add comments support
- Add music info display
- Add reel resizing with - and =
- Fix scrolling stabilization

## [1.1.2]

- Fix settings bug

## [1.1.1]

- Fix dynamic TUI sizing based on reel dimensions
- Add hardware decoding
- Fix verified badge placement
- Fix loading spinner position

## [1.1.0]

- Add adjustable video width and height
- Add retina display support
- Add persistent settings
- Add profile picture rendering
- Add AUR and Homebrew distribution
- Fix TUI layout centering
- Fix terminal resize positioning

## [1.0.1]

- Fix login flow

## [1.0.0]

- Initial release
