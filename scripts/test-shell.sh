#!/usr/bin/env bash
# Regression tests for spotify-tray-setup and uninstall.sh (PR #3 review findings).
# Usage: scripts/test-shell.sh [repo-root]
set -uo pipefail

REPO=$(cd "${1:-$(dirname "$0")/..}" && pwd)
SETUP="$REPO/aur/spotify-tray-wayland-bin/spotify-tray-setup"
fails=0

# Missing tools would make setup skip steps and look like regressions.
for tool in jq python3 script; do
    command -v "$tool" > /dev/null || { echo "test-shell.sh needs $tool" >&2; exit 2; }
done
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

pass() { echo "ok   - $1"; }
fail() { echo "FAIL - $1"; fails=$((fails + 1)); }

# Fresh fake HOME with stubbed hyprctl/pkill/update-desktop-database.
# hyprctl logs "<signature>|<args>" to $T/hyprctl.log and prints an empty bind list.
new_env() {
    T=$(mktemp -d -p "$ROOT")
    export HOME="$T/home with space"
    mkdir -p "$HOME/.config/hypr/conf.d" "$T/bin" "$T/run"
    cat > "$T/bin/hyprctl" << EOF
#!/usr/bin/env bash
echo "\${HYPRLAND_INSTANCE_SIGNATURE:-}|\$*" >> "$T/hyprctl.log"
[[ "\$*" == "-j binds" ]] && echo '[]'
exit 0
EOF
    printf '#!/bin/sh\nexit 1\n' > "$T/bin/pkill"
    printf '#!/bin/sh\nexit 0\n' > "$T/bin/update-desktop-database"
    chmod +x "$T/bin"/*
    export PATH="$T/bin:$ORIG_PATH"
    export XDG_RUNTIME_DIR="$T/run"
    unset HYPRLAND_INSTANCE_SIGNATURE
}
ORIG_PATH=$PATH

# Run setup interactively: stdin/stdout must be a TTY, so drive it through script(1).
run_interactive() {
    printf "$1" | script -qec "bash $(printf '%q' "$SETUP")" /dev/null > "$T/out" 2>&1
}

# --- uninstall.sh removes the source line in the forms setup.sh detects ---
new_env
cat > "$HOME/.config/hypr/hyprland.conf" << 'EOF'
source = ~/.config/hypr/conf.d/*.conf
source = ~/.config/hypr/conf.d/spotify.conf
  source=~/.config/hypr/conf.d/spotify.conf
source = "~/.config/hypr/conf.d/spotify.conf" # spotify tray
EOF
bash "$REPO/uninstall.sh" > /dev/null 2>&1
if [[ "$(cat "$HOME/.config/hypr/hyprland.conf")" == 'source = ~/.config/hypr/conf.d/*.conf' ]]; then
    pass "uninstall removes spotify.conf source variants, keeps conf.d/*.conf"
else
    fail "uninstall left: $(grep -c . "$HOME/.config/hypr/hyprland.conf") lines: $(tr '\n' ';' < "$HOME/.config/hypr/hyprland.conf")"
fi

# --- --migrate reloads the live compositor without HYPRLAND_INSTANCE_SIGNATURE (sudo -H case) ---
new_env
mkdir -p "$T/run/hypr/sig123"
python3 -c 'import socket,sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$T/run/hypr/sig123/.socket.sock"
printf '%s\n' '# Spotify Tray Configuration' 'unbind = SUPER, M' \
    'bind = SUPER, M, movetoworkspacesilent, special:spotify' \
    'bind = SUPER, grave, togglespecialworkspace, spotify' > "$HOME/.config/hypr/conf.d/spotify.conf"
bash "$SETUP" --migrate > /dev/null 2>&1
if grep -q 'SUPER, [Mg]' "$HOME/.config/hypr/conf.d/spotify.conf"; then
    fail "--migrate left obsolete bindings"
elif grep -qx 'sig123|reload' "$T/hyprctl.log" 2>/dev/null; then
    pass "--migrate removes old bindings and reloads with discovered instance signature"
else
    fail "--migrate reload missing signature: $(cat "$T/hyprctl.log" 2>/dev/null)"
fi

# --- --upgrade installs a missing desktop entry; Exec quoted for HOME with spaces ---
new_env
bash "$SETUP" --upgrade > /dev/null 2>&1
desktop="$HOME/.local/share/applications/spotify.desktop"
if [[ -f "$desktop" ]]; then
    pass "--upgrade creates missing desktop entry"
else
    fail "--upgrade did not create desktop entry"
fi
bash "$SETUP" --install-desktop "$HOME/.local/bin/spotify-launcher.sh" > /dev/null 2>&1
if grep -qxF "Exec=\"$HOME/.local/bin/spotify-launcher.sh\" %U" "$desktop" 2>/dev/null; then
    pass "desktop entry quotes launcher path"
else
    fail "desktop Exec line: $(grep Exec= "$desktop" 2>/dev/null)"
fi

# --- workspace binding appended to snippet lacking trailing newline stays on its own line ---
new_env
: > "$HOME/.config/hypr/hyprland.conf"
printf '# Spotify Tray Configuration\nbind = SUPER, P, exec, true' > "$HOME/.config/hypr/conf.d/spotify.conf"
run_interactive '1\n\n'
if grep -qx 'bind = SUPER, P, exec, true' "$HOME/.config/hypr/conf.d/spotify.conf" &&
    grep -qx 'bindd = SUPER, grave, Spotify Tray workspace shortcut, togglespecialworkspace, spotify' "$HOME/.config/hypr/conf.d/spotify.conf"; then
    pass "workspace binding not merged into unterminated last line"
else
    fail "snippet: $(tr '\n' ';' < "$HOME/.config/hypr/conf.d/spotify.conf")"
fi

# --- CAPS modifier accepted by conflict checker ---
new_env
: > "$HOME/.config/hypr/hyprland.conf"
run_interactive '1\nCAPS + x\n'
if grep -qx 'bindd = CAPS, x, Spotify Tray workspace shortcut, togglespecialworkspace, spotify' "$HOME/.config/hypr/conf.d/spotify.conf" 2>/dev/null; then
    pass "CAPS + key workspace shortcut accepted"
else
    fail "CAPS shortcut rejected: $(tail -c 300 "$T/out" | tr -d '\r' | tr '\n' ' ')"
fi

exit $(( fails > 0 ))
