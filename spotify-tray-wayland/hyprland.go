// hyprland.go - Hyprland window manager integration via Unix socket IPC
//
// Performance optimizations (v1.1.0):
//   - Socket IPC: Direct communication with Hyprland socket (~55x faster than exec.Command)
//   - V7 Minimal Parser: Single-pass byte scanning, only extracts needed fields
//   - V11 Direct Search: FindSpotify scans for "class: Spotify" without parsing all clients
//   - Buffer Pooling: 16KB pooled buffers reduce GC pressure
//
// Architecture:
//   - Primary: Unix socket at /run/user/{uid}/hypr/{sig}/.socket.sock
//   - Fallback: exec.Command("hyprctl", ...) for systems without socket access

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const (
	// socketTimeout covers the entire socket operation (dial + write + read)
	// Keep it short for local Unix sockets - they should respond instantly
	socketTimeout = 500 * time.Millisecond

	// maxConsecutiveFailures before attempting socket path refresh
	maxConsecutiveFailures = 3
)

// addressRegex validates Hyprland window addresses (0x followed by hex digits)
var addressRegex = regexp.MustCompile(`^(address:)?0x[0-9a-fA-F]+$`)

// hyprBufferPool provides reusable 16KB buffers for socket reads.
// Avoids allocations on every IPC call (~50 windows fit in 16KB).
var hyprBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 16384) // 16KB - enough for ~50 windows
		return &b
	},
}

// HyprlandManager implements WindowManager for Hyprland using direct socket IPC
type HyprlandManager struct {
	mu                  sync.Mutex
	socketPath          string
	consecutiveFailures int
}

// validateAddress checks if an address is in the expected Hyprland format.
// This prevents injection attacks through malformed addresses.
func validateAddress(addr string) error {
	if !addressRegex.MatchString(addr) {
		return fmt.Errorf("invalid address format: %q", addr)
	}
	return nil
}

// NewHyprlandManager creates a new socket-based HyprlandManager
func NewHyprlandManager() *HyprlandManager {
	socketPath := findHyprlandSocket()
	return &HyprlandManager{socketPath: socketPath}
}

// findHyprlandSocket locates the Hyprland IPC socket.
// First tries HYPRLAND_INSTANCE_SIGNATURE, then scans for any available socket.
func findHyprlandSocket() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}

	// Try environment variable first
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig != "" {
		paths := []string{
			fmt.Sprintf("%s/hypr/%s/.socket.sock", runtimeDir, sig),
			fmt.Sprintf("/tmp/hypr/%s/.socket.sock", sig),
		}
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	// Fallback: scan for any Hyprland socket (handles restarts where env is stale)
	hyprDirs := []string{
		filepath.Join(runtimeDir, "hypr"),
		"/tmp/hypr",
	}

	for _, dir := range hyprDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sockPath := filepath.Join(dir, entry.Name(), ".socket.sock")
			if _, err := os.Stat(sockPath); err == nil {
				return sockPath
			}
		}
	}

	return "" // Will fall back to exec
}

// refreshSocketPath re-detects the Hyprland socket path.
// Called when consecutive socket failures suggest the path is stale.
func (h *HyprlandManager) refreshSocketPath() {
	h.mu.Lock()
	defer h.mu.Unlock()

	oldPath := h.socketPath
	h.socketPath = findHyprlandSocket()
	h.consecutiveFailures = 0

	if h.socketPath != oldPath {
		if h.socketPath != "" {
			log.Printf("Hyprland socket refreshed: %s", h.socketPath)
		} else {
			log.Println("Hyprland socket not found, using exec fallback")
		}
	}
}

// sendCommand sends a command to Hyprland via socket and returns response.
// Uses timeouts on all operations to prevent indefinite blocking during suspend/resume.
// Tracks failures and refreshes socket path if needed.
func (h *HyprlandManager) sendCommand(cmd string) ([]byte, error) {
	h.mu.Lock()
	socketPath := h.socketPath
	h.mu.Unlock()

	if socketPath == "" {
		return h.sendCommandExec(cmd)
	}

	// Use DialTimeout to prevent blocking if socket is unresponsive
	conn, err := net.DialTimeout("unix", socketPath, socketTimeout)
	if err != nil {
		h.recordFailure()
		// Fall back to exec on socket error
		return h.sendCommandExec(cmd)
	}
	defer conn.Close()

	// Set deadline for entire operation (write + read)
	// This prevents hanging if Hyprland is frozen (e.g., during suspend)
	if err := conn.SetDeadline(time.Now().Add(socketTimeout)); err != nil {
		return nil, fmt.Errorf("failed to set socket deadline: %w", err)
	}

	if _, err := conn.Write([]byte(cmd)); err != nil {
		h.recordFailure()
		// Check if this is a timeout - fall back to exec
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return h.sendCommandExec(cmd)
		}
		return nil, err
	}

	// Read into pooled buffer
	bufPtr := hyprBufferPool.Get().(*[]byte)
	buf := *bufPtr

	var total int
	for {
		n, err := conn.Read(buf[total:])
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			hyprBufferPool.Put(bufPtr)
			h.recordFailure()
			// Check if this is a timeout - fall back to exec
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return h.sendCommandExec(cmd)
			}
			return nil, err
		}
		if total >= len(buf) {
			break
		}
	}

	// Copy result before returning buffer to pool
	result := make([]byte, total)
	copy(result, buf[:total])
	hyprBufferPool.Put(bufPtr)

	// Reset failure counter on success
	h.mu.Lock()
	h.consecutiveFailures = 0
	h.mu.Unlock()

	return result, nil
}

// recordFailure tracks consecutive socket failures and triggers refresh if needed
func (h *HyprlandManager) recordFailure() {
	h.mu.Lock()
	h.consecutiveFailures++
	shouldRefresh := h.consecutiveFailures >= maxConsecutiveFailures
	h.mu.Unlock()

	if shouldRefresh {
		h.refreshSocketPath()
	}
}

// sendCommandExec falls back to hyprctl for systems without socket access
func (h *HyprlandManager) sendCommandExec(cmd string) ([]byte, error) {
	// Parse command for exec fallback
	// "clients" -> hyprctl clients
	// "dispatch movetoworkspacesilent ..." -> hyprctl dispatch movetoworkspacesilent ...
	args := []string{}
	for _, part := range bytes.Fields([]byte(cmd)) {
		args = append(args, string(part))
	}
	return exec.Command("hyprctl", args...).Output()
}

// GetClients returns all Hyprland clients using socket IPC + V7 minimal parsing
func (h *HyprlandManager) GetClients() ([]HyprlandClient, error) {
	data, err := h.sendCommand("clients")
	if err != nil {
		return nil, fmt.Errorf("failed to get clients: %w", err)
	}
	return parseClientsMinimal(data), nil
}

// parseClientsMinimal is the V7 minimal parser - only extracts needed fields
func parseClientsMinimal(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	clients := make([]HyprlandClient, 0, 8)
	idx := -1

	i := 0
	for i < len(data) {
		// Find next line
		lineEnd := i
		for lineEnd < len(data) && data[lineEnd] != '\n' {
			lineEnd++
		}
		line := data[i:lineEnd]
		i = lineEnd + 1

		if len(line) < 7 {
			continue
		}

		// Check first byte to decide what to do
		switch line[0] {
		case 'W': // Window
			if line[6] == ' ' {
				clients = append(clients, HyprlandClient{})
				idx = len(clients) - 1
				// Parse address
				rest := line[7:]
				for j := 0; j < len(rest)-3; j++ {
					if rest[j] == ' ' && rest[j+1] == '-' {
						clients[idx].Address = "0x" + bytesToStr(rest[:j])
						break
					}
				}
			}
		case '\t', ' ': // Indented field
			if idx < 0 {
				continue
			}
			// Skip whitespace
			for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
				line = line[1:]
			}
			if len(line) < 7 {
				continue
			}
			switch line[0] {
			case 'c': // class
				if line[5] == ':' {
					clients[idx].Class = bytesToStr(line[7:])
				}
			case 'w': // workspace
				if len(line) >= 11 && line[9] == ':' {
					ws := line[11:]
					for j := 0; j < len(ws); j++ {
						if ws[j] == '(' {
							clients[idx].Workspace.ID = parseIntFast(ws[:j-1])
							for k := len(ws) - 1; k > j; k-- {
								if ws[k] == ')' {
									clients[idx].Workspace.Name = bytesToStr(ws[j+1 : k])
									break
								}
							}
							break
						}
					}
				}
			}
		}
	}

	return clients
}

// bytesToStr converts bytes to string (copies to ensure safety)
func bytesToStr(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

// parseIntFast parses integer without fmt.Sscanf overhead
func parseIntFast(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	neg := false
	i := 0
	if b[0] == '-' {
		neg = true
		i = 1
	}
	n := 0
	for ; i < len(b); i++ {
		if b[i] >= '0' && b[i] <= '9' {
			n = n*10 + int(b[i]-'0')
		}
	}
	if neg {
		return -n
	}
	return n
}

// MoveWindow moves a window to the specified workspace via socket.
// Validates address format to prevent injection attacks.
func (h *HyprlandManager) MoveWindow(address, workspace string) error {
	// Security: validate address format
	if err := validateAddress(address); err != nil {
		return err
	}

	const maxRetries = 3
	cmd := fmt.Sprintf("dispatch movetoworkspacesilent %s,%s", workspace, address)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		out, err := h.sendCommand(cmd)
		if err == nil && bytes.Equal(bytes.TrimSpace(out), []byte("ok")) {
			return nil
		}
		if attempt == maxRetries {
			return fmt.Errorf("failed to move window after %d attempts: %v (output: %q)", maxRetries, err, string(out))
		}
	}
	return nil
}

// FocusWindow focuses the specified window via socket.
// Validates address format to prevent injection attacks.
func (h *HyprlandManager) FocusWindow(address string) error {
	// Security: validate address format
	if err := validateAddress(address); err != nil {
		return err
	}

	cmd := fmt.Sprintf("dispatch focuswindow %s", address)
	out, err := h.sendCommand(cmd)
	if err != nil {
		return fmt.Errorf("failed to focus window: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(out), []byte("ok")) {
		return fmt.Errorf("focuswindow returned: %q", string(out))
	}
	return nil
}

// CloseWindow closes a window by class via socket
func (h *HyprlandManager) CloseWindow(class string) error {
	cmd := fmt.Sprintf("dispatch closewindow class:%s", class)
	_, err := h.sendCommand(cmd)
	return err
}

// LaunchSpotify starts the Spotify application (requires exec, not socket)
func (h *HyprlandManager) LaunchSpotify() error {
	cmd := exec.Command("spotify")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch spotify: %w", err)
	}
	// Reap the process in background
	go func() { _ = cmd.Wait() }()
	return nil
}

// FindSpotify locates the Spotify window using V11 direct byte scanning.
// Instead of parsing all clients, it scans raw socket response for "class: Spotify"
// and only parses that window block. ~3x faster than GetClients + loop.
// Uses timeouts to prevent blocking during suspend/resume.
func (h *HyprlandManager) FindSpotify() (HyprlandClient, bool) {
	h.mu.Lock()
	socketPath := h.socketPath
	h.mu.Unlock()

	if socketPath == "" {
		// Fallback: use GetClients and search
		clients, err := h.GetClients()
		if err != nil {
			return HyprlandClient{}, false
		}
		for _, c := range clients {
			if isSpotifyClass(c.Class) {
				return c, true
			}
		}
		return HyprlandClient{}, false
	}

	// Fast path: direct socket + V11 search with timeout
	conn, err := net.DialTimeout("unix", socketPath, socketTimeout)
	if err != nil {
		h.recordFailure()
		return HyprlandClient{}, false
	}
	defer conn.Close()

	// Set deadline for entire operation
	if err := conn.SetDeadline(time.Now().Add(socketTimeout)); err != nil {
		return HyprlandClient{}, false
	}

	if _, err := conn.Write([]byte("clients")); err != nil {
		h.recordFailure()
		return HyprlandClient{}, false
	}

	// Read into pooled buffer
	bufPtr := hyprBufferPool.Get().(*[]byte)
	defer hyprBufferPool.Put(bufPtr)
	buf := *bufPtr

	var total int
	for {
		n, err := conn.Read(buf[total:])
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			return HyprlandClient{}, false
		}
		if total >= len(buf) {
			break
		}
	}

	return searchSpotifyDirect(buf[:total])
}

// searchSpotifyDirect implements V11 direct search
func searchSpotifyDirect(data []byte) (HyprlandClient, bool) {
	// Scan for "class: Spotify" or "class: spotify"
	for i := 0; i < len(data)-14; i++ {
		if data[i] == 'c' && data[i+1] == 'l' && data[i+2] == 'a' &&
			data[i+3] == 's' && data[i+4] == 's' && data[i+5] == ':' && data[i+6] == ' ' {
			// Check if it's "spotify" (case insensitive)
			name := data[i+7:]
			if len(name) >= 7 && isSpotifyBytes(name[:7]) {
				return parseSpotifyWindow(data, i)
			}
		}
	}
	return HyprlandClient{}, false
}

// isSpotifyBytes checks if bytes match "spotify" case-insensitively
func isSpotifyBytes(b []byte) bool {
	return (b[0]|0x20) == 's' &&
		(b[1]|0x20) == 'p' &&
		(b[2]|0x20) == 'o' &&
		(b[3]|0x20) == 't' &&
		(b[4]|0x20) == 'i' &&
		(b[5]|0x20) == 'f' &&
		(b[6]|0x20) == 'y'
}

// isSpotifyClass checks string class name
func isSpotifyClass(class string) bool {
	if len(class) != 7 {
		return false
	}
	b := []byte(class)
	return isSpotifyBytes(b)
}

// parseSpotifyWindow parses just the Spotify window from data
func parseSpotifyWindow(data []byte, classPos int) (HyprlandClient, bool) {
	var client HyprlandClient
	client.Class = "Spotify"

	// Backtrack to find Window line
	for i := classPos; i >= 0; i-- {
		isLineStart := i == 0 || data[i-1] == '\n'
		if isLineStart && i+7 < len(data) && data[i] == 'W' && data[i+6] == ' ' {
			// Parse address
			lineEnd := i
			for lineEnd < len(data) && data[lineEnd] != '\n' {
				lineEnd++
			}
			line := data[i:lineEnd]
			if len(line) >= 7 {
				rest := line[7:]
				for j := 0; j < len(rest)-3; j++ {
					if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
						client.Address = "0x" + string(rest[:j])
						break
					}
				}
			}

			// Find workspace (between Window line and class line)
			for k := i; k < classPos && k < len(data)-11; k++ {
				if data[k] == 'w' && data[k+1] == 'o' && data[k+9] == ':' && data[k+10] == ' ' {
					ws := data[k+11:]
					lineEnd := 0
					for lineEnd < len(ws) && ws[lineEnd] != '\n' {
						lineEnd++
					}
					ws = ws[:lineEnd]
					for j := 0; j < len(ws); j++ {
						if ws[j] == '(' {
							client.Workspace.ID = parseIntFast(ws[:j-1])
							for m := len(ws) - 1; m > j; m-- {
								if ws[m] == ')' {
									client.Workspace.Name = string(ws[j+1 : m])
									break
								}
							}
							break
						}
					}
					break
				}
			}
			break
		}
	}

	return client, client.Address != ""
}

// Legacy parseHyprctlClients for compatibility (used by benchmarks)
func parseHyprctlClients(data []byte) []HyprlandClient {
	return parseClientsMinimal(data)
}
