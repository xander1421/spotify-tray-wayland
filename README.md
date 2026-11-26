# Spotify Tray for Wayland/Hyprland

A native system tray application for Spotify on Wayland, specifically designed for Hyprland. Written in Go using D-Bus for media control.

## Features

- **System tray icon** with Spotify branding
- **Scroll controls**: Scroll up/down on tray icon for next/previous track
- **Middle-click**: Play/pause toggle
- **Right-click menu**:
  - Current track info display
  - Show/Hide Spotify window
  - Previous / Play-Pause / Next controls
  - Quit Spotify
- **Window minimize**: Hide Spotify to a special workspace (like Windows minimize to tray)
- **Optional smart close**: Use your existing close shortcut to hide Spotify to the tray
- **Auto-start**: Tray launches automatically with Spotify

## Requirements

- Hyprland (Wayland compositor) — Lua config (0.55+) and legacy `hyprland.conf` both supported
- Go 1.26+ (for building)
- Spotify (official Linux client)
- D-Bus session bus

## Installation

### Arch Linux (AUR)

```bash
yay -S spotify-tray-wayland-bin   # pre-built binary
# or
yay -S spotify-tray-wayland-git   # build from source
```

Run `spotify-tray-setup` as your desktop user to configure optional smart close. If the installer asks you to enable the Spotify launcher first, run `spotify-tray-setup --install-desktop`.

### Manual Installation

```bash
git clone https://github.com/xander1421/spotify-tray-wayland.git
cd spotify-tray-wayland
./setup.sh
```

The setup script installs Spotify Tray to `~/.local/bin/` and sets up the Spotify desktop launcher. When run in a terminal, it offers smart close and a Spotify workspace shortcut.

## Usage

After installation, just launch Spotify from your app menu. The tray icon will appear automatically.

### Tray Controls

| Action | Effect |
|--------|--------|
| Scroll Up | Next track |
| Scroll Down | Previous track |
| Middle Click | Play/Pause |
| Left Click | Open menu |

### Keyboard Shortcuts

Run `spotify-tray-setup` inside Hyprland to configure keyboard controls. Setup offers Super+grave to open or close the Spotify workspace when Hyprland reports the shortcut is free. If the combination is in use, choose another shortcut or skip. Install `jq` for this check.

Use **Show/Hide Spotify** in the tray or run `spotify-tray-wayland toggle` to move the Spotify window between the special workspace and your current workspace. To bind that action yourself, add a shortcut of your choice to your Hyprland config.

Lua:

```lua
hl.bind("<your shortcut>", hl.dsp.exec_cmd("spotify-tray-wayland toggle"))
```

Legacy `hyprland.conf`:

```ini
bind = <modifiers>, <key>, exec, spotify-tray-wayland toggle
```

Specify the full path to the executable if your Hyprland session uses a different PATH.

To enable smart close, choose option 2 in setup and confirm your detected close shortcut. Upgrades preserve configured shortcuts and remove the old unconditional Super+M and Super+grave bindings. Run setup again to choose a workspace shortcut after upgrading. If the package manager could not identify your desktop user, run `spotify-tray-setup --migrate` in your session.

### Menu Options

- **Track Info**: Shows current song (artist - title)
- **Show/Hide Spotify**: Toggle window visibility
- **Previous/Play-Pause/Next**: Playback controls
- **Quit**: Close Spotify and tray

## Uninstallation

```bash
./uninstall.sh
```

This removes the binary, launcher, desktop entry override, and Hyprland config.

## How It Works

1. **D-Bus/MPRIS**: Uses the MPRIS D-Bus interface to control Spotify playback and get track metadata
2. **StatusNotifierItem**: Creates a system tray icon using the SNI protocol (native Wayland tray support)
3. **Hyprland Socket IPC**: Direct Unix socket communication with Hyprland (no subprocess spawning)
4. **Optional smart-close script**: Uses your confirmed close shortcut to hide focused Spotify windows and close other focused windows

## Security

- **D-Bus method allowlist**: Only safe MPRIS methods (Play, Pause, PlayPause, Next, Previous, Stop) are permitted—dangerous methods like OpenUri or Quit from untrusted sources are blocked
- **Address validation**: Hyprland window addresses are validated against a strict regex pattern to prevent command injection
- **No shell commands for process control**: Uses D-Bus `org.mpris.MediaPlayer2.Quit` to terminate Spotify instead of `pkill`/`kill`—cannot accidentally affect other processes

## Resilience

The app is designed to survive system events that break connections:

| Event | Handling |
|-------|----------|
| **Suspend/Resume** | Socket timeouts (500ms) prevent indefinite hangs; stale connections are detected and refreshed |
| **D-Bus restart** | Automatic reconnection with exponential backoff (1s → 10s max) |
| **Hyprland restart** | Socket path auto-refresh after 3 consecutive failures |
| **Panel restart** | Tray icon auto-re-registers via `NameOwnerChanged` signal (forked systray library) |

## Performance

v1.1.0 introduced significant performance optimizations:

| Operation | Before | After | Improvement |
|-----------|--------|-------|-------------|
| Hyprland IPC | exec.Command | Unix socket | ~55x faster |
| Find Spotify window | Parse all clients | Direct byte scan | ~3x faster |
| Memory allocations | ~40 per call | ~13 per call | ~3x fewer |

**Technical details:**
- **Socket IPC**: Connects directly to `/run/user/{uid}/hypr/{sig}/.socket.sock` instead of spawning `hyprctl` processes
- **V7 Minimal Parser**: Single-pass byte scanning, only extracts needed fields (address, class, workspace)
- **V11 Direct Search**: Scans raw bytes for `"class: Spotify"` pattern without parsing all windows
- **Buffer Pooling**: Reuses 16KB buffers via `sync.Pool` to reduce GC pressure

## Building Manually

```bash
cd spotify-tray-wayland
go build -o spotify-tray-wayland .
```

## Dependencies

The Go module uses:
- `fyne.io/systray` - System tray (StatusNotifierItem) support
- `github.com/godbus/dbus/v5` - D-Bus bindings for MPRIS control

## Troubleshooting

### Tray icon doesn't appear
- Ensure your Wayland bar supports StatusNotifierItem (Waybar, etc.)
- Check if the process is running: `pgrep -f spotify-tray-wayland`

### No track info / controls don't work
- Spotify must be running for D-Bus communication
- Check D-Bus: `dbus-send --print-reply --dest=org.mpris.MediaPlayer2.spotify /org/mpris/MediaPlayer2 org.freedesktop.DBus.Properties.Get string:'org.mpris.MediaPlayer2.Player' string:'Metadata'`

### Show/Hide doesn't work
- Requires Hyprland (uses direct socket IPC)
- Check if Spotify window is detected: `hyprctl clients | grep -i spotify`
- Check Hyprland socket exists: `ls /run/user/$(id -u)/hypr/`

### Tray disappears after panel restart
- The app auto-re-registers with the new panel instance
- If not working, restart the tray: `pkill -x spotify-tray-wayland && spotify-tray-wayland &`

## License

GPL-3.0 - See [LICENSE](LICENSE)

## Disclaimer

This software is unofficial and not affiliated with Spotify Technology S.A. It adds functionality missing in the official Linux client.

Spotify is a registered trademark of Spotify AB.
