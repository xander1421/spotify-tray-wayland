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
- **Auto-start**: Tray launches automatically with Spotify

## Requirements

- Hyprland (Wayland compositor)
- Go 1.21+ (for building)
- Spotify (official Linux client)
- D-Bus session bus

## Installation

### Arch Linux (AUR)

```bash
yay -S spotify-tray-wayland-bin   # pre-built binary
# or
yay -S spotify-tray-wayland-git   # build from source
```

### Manual Installation

```bash
git clone https://github.com/xander1421/spotify-tray-wayland.git
cd spotify-tray-wayland
./setup.sh
```

The setup script will:
1. Build the Go binary
2. Install to `~/.local/bin/`
3. Create a launcher script that starts the tray with Spotify
4. Override the Spotify desktop entry to use the launcher
5. Add Hyprland keybinds for window minimizing

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

| Shortcut | Action |
|----------|--------|
| Super+M | Minimize focused window |
| Super+` | Show minimized windows |

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
3. **Hyprland IPC**: Uses `hyprctl` to show/hide the Spotify window via special workspaces

## Project Structure

```
spotify-tray-wayland/
├── spotify-tray-wayland/   # Go source code
│   ├── main.go             # Main application
│   └── go.mod              # Go module definition
├── setup.sh                # Installation script
├── uninstall.sh            # Removal script
├── LICENSE                 # GPL-3.0
└── README.md
```

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
- Requires Hyprland (uses `hyprctl` commands)
- Check if Spotify window is detected: `hyprctl clients | grep -i spotify`

## License

GPL-3.0 - See [LICENSE](LICENSE)

## Disclaimer

This software is unofficial and not affiliated with Spotify Technology S.A. It adds functionality missing in the official Linux client.

Spotify is a registered trademark of Spotify AB.
