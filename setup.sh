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

echo "=== Spotify Tray Setup ==="

# Check dependencies
echo "Checking dependencies..."
command -v go >/dev/null 2>&1 || error_exit "Go is not installed. Install with: sudo pacman -S go"
command -v hyprctl >/dev/null 2>&1 || error_exit "Hyprland is not installed or not in PATH"
command -v spotify >/dev/null 2>&1 || warn "Spotify not found. Install spotify-launcher or spotify from AUR"

echo "Building Spotify Tray..."
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

success "      Built successfully"

# 3. Install binaries to ~/.local/bin
echo "Installing to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR" || error_exit "Failed to create $INSTALL_DIR"

temporary_binary=$(mktemp "$INSTALL_DIR/.spotify-tray-wayland.XXXXXX")
install -m755 spotify-tray-wayland "$temporary_binary"
mv -fT "$temporary_binary" "$INSTALL_DIR/spotify-tray-wayland"
install -m755 "$SCRIPT_DIR/aur/spotify-tray-wayland-bin/spotify-tray-setup" "$INSTALL_DIR/spotify-tray-setup"

# Create launcher script
cat > "$INSTALL_DIR/spotify-launcher.sh" << 'EOF'
#!/usr/bin/env bash
# Spotify Launcher - starts tray with Spotify

# Start tray if not running
if ! pgrep -u "$(id -u)" -f '^([^ ]*/)?spotify-tray-wayland$' >/dev/null; then
    ~/.local/bin/spotify-tray-wayland &
fi

# Start Spotify
exec spotify "$@"
EOF
chmod +x "$INSTALL_DIR/spotify-launcher.sh" || error_exit "Failed to make launcher script executable"

success "      Installed binaries"

"$INSTALL_DIR/spotify-tray-setup" --install-desktop "$INSTALL_DIR/spotify-launcher.sh"
if [[ -t 0 && -t 1 ]]; then
    "$INSTALL_DIR/spotify-tray-setup"
fi

rm -f "$HOME/.config/autostart/spotify-tray.desktop"
success "Setup complete. Launch Spotify from your app menu."
