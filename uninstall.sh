#!/usr/bin/env bash

# Spotify Tray for Hyprland - Uninstall Script

echo "=== Spotify Tray Uninstall ==="

# Stop running instance
if pkill -f spotify-tray-wayland 2>/dev/null; then
    echo "[1/6] Stopped running tray"
else
    echo "[1/6] Tray not running"
fi

# Remove binaries
if rm -f "$HOME/.local/bin/spotify-tray-wayland" "$HOME/.local/bin/spotify-launcher.sh" 2>/dev/null; then
    echo "[2/6] Removed binaries"
else
    echo "[2/6] Binaries not found"
fi

# Remove smart-close script
if rm -f "$HOME/.config/hypr/UserScripts/smart-close.sh" 2>/dev/null; then
    echo "[3/6] Removed smart-close script"
else
    echo "[3/6] Smart-close script not found"
fi

# Remove desktop entry override (restores system Spotify entry)
if rm -f "$HOME/.local/share/applications/spotify.desktop" 2>/dev/null; then
    echo "[4/6] Removed desktop entry override"
    update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
else
    echo "[4/6] Desktop entry not found"
fi

# Remove Hyprland config
if rm -f "$HOME/.config/hypr/conf.d/spotify.conf" 2>/dev/null; then
    echo "[5/6] Removed Hyprland config"
else
    echo "[5/6] Hyprland config not found"
fi

# Remove old autostart entry (if exists)
rm -f "$HOME/.config/autostart/spotify-tray.desktop" 2>/dev/null

# Reload Hyprland
if hyprctl reload 2>/dev/null; then
    echo "[6/6] Reloaded Hyprland"
else
    echo "[6/6] Hyprland reload skipped"
fi

echo ""
echo "=== Uninstall Complete ==="
echo "Spotify will now use the system default behavior."
echo "Note: Super+Q keybind restored to default (kill window)."
