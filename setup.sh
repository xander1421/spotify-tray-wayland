#!/usr/bin/env bash
set -euo pipefail

# Spotify Tray for Hyprland - Setup Script
# Installs tray icon with scroll/middle-click support and smart-close keybind

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Error handler
error_exit() {
    echo -e "${RED}ERROR: $1${NC}" >&2
    exit 1
}

# Warning (non-fatal)
warn() {
    echo -e "${YELLOW}WARNING: $1${NC}" >&2
}

# Success message
success() {
    echo -e "${GREEN}$1${NC}"
}

# Trap errors and show where they occurred
trap 'error_exit "Script failed at line $LINENO. Command: $BASH_COMMAND"' ERR

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config"
APPS_DIR="$HOME/.local/share/applications"
HYPR_SCRIPTS="$CONFIG_DIR/hypr/UserScripts"
HYPR_DIR="$CONFIG_DIR/hypr"

echo "=== Spotify Tray Setup ==="

# Check dependencies
echo "[0/7] Checking dependencies..."
command -v go >/dev/null 2>&1 || error_exit "Go is not installed. Install with: sudo pacman -S go"
command -v hyprctl >/dev/null 2>&1 || error_exit "Hyprland is not installed or not in PATH"
command -v spotify >/dev/null 2>&1 || warn "Spotify not found. Install spotify-launcher or spotify from AUR"

# Check Hyprland config exists
[[ -d "$HYPR_DIR" ]] || error_exit "Hyprland config directory not found at $HYPR_DIR"
[[ -f "$HYPR_DIR/hyprland.conf" ]] || error_exit "hyprland.conf not found at $HYPR_DIR/hyprland.conf"

echo "      Dependencies OK"

# Detect user's killactive keybind
detect_killactive_keybind() {
    local killactive_line modifier key

    # Search all hyprland configs for killactive binding
    killactive_line=$(grep -rh "killactive" "$HYPR_DIR" 2>/dev/null | grep -E "^bind" | head -1 || true)

    if [[ -z "$killactive_line" ]]; then
        # Default fallback
        echo "SUPER Q"
        return
    fi

    # Extract modifier and key from bind line
    # Format: bind[d] = MODIFIER, KEY, [description,] killactive
    # Remove everything after the = and parse
    local bind_part
    bind_part=$(echo "$killactive_line" | sed 's/.*=\s*//' | tr -d ' ')

    # Get first two comma-separated values (modifier, key)
    modifier=$(echo "$bind_part" | cut -d',' -f1)
    key=$(echo "$bind_part" | cut -d',' -f2)

    # Resolve $mainMod variable if present
    if [[ "$modifier" == *'$mainMod'* ]] || [[ "$modifier" == *'${mainMod}'* ]]; then
        # Find mainMod definition
        local mainmod_def
        mainmod_def=$(grep -rh '^\$mainMod\s*=' "$HYPR_DIR" 2>/dev/null | head -1 | sed 's/.*=\s*//' | tr -d ' ' || true)
        mainmod_def=${mainmod_def:-SUPER}
        modifier=$(echo "$modifier" | sed "s/\\\$mainMod/$mainmod_def/g" | sed "s/\${mainMod}/$mainmod_def/g")
    fi

    echo "$modifier $key"
}

echo "[1/7] Detecting keybinds..."
read -r CLOSE_MOD CLOSE_KEY <<< "$(detect_killactive_keybind)"
echo "      Found killactive keybind: $CLOSE_MOD + $CLOSE_KEY"

# 2. Build the tray application
echo "[2/7] Building spotify-tray..."
cd "$SCRIPT_DIR/spotify-tray-wayland"

# Check source files exist
for file in main.go dbus.go hyprland.go interfaces.go; do
    [[ -f "$file" ]] || error_exit "Source file $file not found in $SCRIPT_DIR/spotify-tray-wayland"
done

# Build with explicit file list (avoid experimental files)
if ! go build -o spotify-tray-wayland main.go dbus.go hyprland.go interfaces.go 2>&1; then
    error_exit "Go build failed. Check the output above for errors."
fi

# Verify binary was created
[[ -f "spotify-tray-wayland" ]] || error_exit "Binary was not created after build"
[[ -x "spotify-tray-wayland" ]] || chmod +x spotify-tray-wayland

# Verify binary has correct workspace (sanity check)
if strings spotify-tray-wayland | grep -q "special:spotify"; then
    warn "Binary contains old 'special:spotify' - expected 'special:minimized'"
fi

success "      Built successfully"

# 3. Install binaries to ~/.local/bin
echo "[3/7] Installing to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR" || error_exit "Failed to create $INSTALL_DIR"

cp spotify-tray-wayland "$INSTALL_DIR/" || error_exit "Failed to copy binary to $INSTALL_DIR"
chmod +x "$INSTALL_DIR/spotify-tray-wayland" || error_exit "Failed to make binary executable"

# Create launcher script
cat > "$INSTALL_DIR/spotify-launcher.sh" << 'EOF'
#!/usr/bin/env bash
# Spotify Launcher - starts tray with Spotify

# Start tray if not running
if ! pgrep -f spotify-tray-wayland >/dev/null; then
    ~/.local/bin/spotify-tray-wayland &
fi

# Start Spotify
exec spotify "$@"
EOF
chmod +x "$INSTALL_DIR/spotify-launcher.sh" || error_exit "Failed to make launcher script executable"

success "      Installed binaries"

# 4. Create desktop entry (overrides system Spotify)
echo "[4/7] Creating desktop entry..."
mkdir -p "$APPS_DIR" || error_exit "Failed to create $APPS_DIR"

cat > "$APPS_DIR/spotify.desktop" << EOF
[Desktop Entry]
Type=Application
Name=Spotify
GenericName=Music Player
Icon=spotify
TryExec=spotify
Exec=$INSTALL_DIR/spotify-launcher.sh %U
Terminal=false
MimeType=x-scheme-handler/spotify;
Categories=Audio;Music;Player;AudioVideo;
StartupWMClass=spotify
EOF

# Update desktop database (non-critical)
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$APPS_DIR" 2>/dev/null || warn "Failed to update desktop database (non-critical)"
fi

success "      Created desktop entry"

# 5. Install smart-close script (Super+Q minimizes Spotify, kills other windows)
echo "[5/7] Installing smart-close script..."
mkdir -p "$HYPR_SCRIPTS" || error_exit "Failed to create $HYPR_SCRIPTS"

cat > "$HYPR_SCRIPTS/smart-close.sh" << 'EOF'
#!/bin/bash
# Smart close: minimize Spotify to tray, kill other windows
# Part of spotify-tray-wayland

class=$(hyprctl activewindow | grep "class:" | cut -d' ' -f2)

if [[ "${class,,}" == "spotify" ]]; then
    hyprctl dispatch movetoworkspacesilent special:spotify
else
    hyprctl dispatch killactive
fi
EOF
chmod +x "$HYPR_SCRIPTS/smart-close.sh" || error_exit "Failed to make smart-close.sh executable"

success "      Installed smart-close script"

# 6. Create Hyprland config snippet
echo "[6/7] Configuring Hyprland keybinds..."
mkdir -p "$CONFIG_DIR/hypr/conf.d" || error_exit "Failed to create conf.d directory"

cat > "$CONFIG_DIR/hypr/conf.d/spotify.conf" << EOF
# Spotify Tray Configuration
# Auto-generated by spotify-tray setup script

# Smart close: minimize Spotify to tray, kill other windows
# Detected keybind: $CLOSE_MOD + $CLOSE_KEY
unbind = $CLOSE_MOD, $CLOSE_KEY
bind = $CLOSE_MOD, $CLOSE_KEY, exec, ~/.config/hypr/UserScripts/smart-close.sh

# Super+\` (grave): Toggle Spotify workspace
bind = SUPER, grave, togglespecialworkspace, spotify
EOF

# Check if conf.d is sourced in main config
if ! grep -q "source.*conf.d" "$CONFIG_DIR/hypr/hyprland.conf" 2>/dev/null; then
    echo "" >> "$CONFIG_DIR/hypr/hyprland.conf"
    echo "# Source additional configs" >> "$CONFIG_DIR/hypr/hyprland.conf"
    echo "source = ~/.config/hypr/conf.d/*.conf" >> "$CONFIG_DIR/hypr/hyprland.conf"
    echo "      Added conf.d source to hyprland.conf"
fi

success "      Created Hyprland config"

# 7. Cleanup and reload
echo "[7/7] Finalizing..."

# Cleanup old autostart (tray now starts with Spotify)
rm -f "$CONFIG_DIR/autostart/spotify-tray.desktop" 2>/dev/null || true

# Reload Hyprland config
if ! hyprctl reload 2>&1; then
    warn "Failed to reload Hyprland config. You may need to reload manually or restart Hyprland."
else
    echo "      Hyprland config reloaded"
fi

success "      Done"

echo ""
success "=== Setup Complete ==="
echo ""
echo "Features:"
echo "  - Tray icon starts WITH Spotify (not at login)"
echo "  - Scroll up/down: Next/Previous track"
echo "  - Middle-click: Play/Pause"
echo "  - Show/Hide menu: Toggle Spotify window"
echo "  - $CLOSE_MOD+$CLOSE_KEY: Minimizes Spotify to tray (kills other windows)"
echo ""
echo "Keybinds:"
echo "  - $CLOSE_MOD+$CLOSE_KEY: Smart close (minimize Spotify / kill others)"
echo "  - Super+\`: Toggle Spotify workspace (show/hide)"
echo ""
echo "Launch Spotify from your app menu to test!"
