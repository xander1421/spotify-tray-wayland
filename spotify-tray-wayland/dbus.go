package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	spotifyDest     = "org.mpris.MediaPlayer2.spotify"
	spotifyObjPath  = "/org/mpris/MediaPlayer2"
	playerInterface = "org.mpris.MediaPlayer2.Player"

	// Timeouts for D-Bus operations
	dbusTimeout     = 1 * time.Second
	metadataTimeout = 500 * time.Millisecond
	pingTimeout     = 100 * time.Millisecond

	// Reconnection backoff settings
	initialBackoff = 1 * time.Second
	maxBackoff     = 10 * time.Second
	backoffFactor  = 2
)

// DBusPlayer implements MediaPlayer using D-Bus with automatic reconnection
// and thread-safe access to the connection.
type DBusPlayer struct {
	mu            sync.RWMutex
	conn          *dbus.Conn
	lastReconnect time.Time
	backoff       time.Duration
}

// NewDBusPlayer creates a new D-Bus media player connection
func NewDBusPlayer() (*DBusPlayer, error) {
	p := &DBusPlayer{
		backoff: initialBackoff,
	}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

// connect establishes a D-Bus session connection (must hold write lock)
func (p *DBusPlayer) connect() error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus: %w", err)
	}
	p.conn = conn
	p.backoff = initialBackoff // Reset backoff on successful connect
	return nil
}

// reconnect attempts to reconnect with exponential backoff.
// Returns true if reconnection was attempted (regardless of success).
func (p *DBusPlayer) reconnect() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if we should wait before reconnecting (exponential backoff)
	if time.Since(p.lastReconnect) < p.backoff {
		return false
	}

	// Close existing connection if any
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}

	p.lastReconnect = time.Now()

	if err := p.connect(); err != nil {
		// Increase backoff for next attempt
		p.backoff *= backoffFactor
		if p.backoff > maxBackoff {
			p.backoff = maxBackoff
		}
		log.Printf("D-Bus reconnect failed (next attempt in %v): %v", p.backoff, err)
		return true
	}

	log.Println("D-Bus reconnected successfully")
	return true
}

// ensureConnected checks connection health and reconnects if needed.
// Uses a fast Ping to verify the connection is alive.
func (p *DBusPlayer) ensureConnected() error {
	p.mu.RLock()
	conn := p.conn
	p.mu.RUnlock()

	if conn == nil {
		p.reconnect()
		p.mu.RLock()
		conn = p.conn
		p.mu.RUnlock()
		if conn == nil {
			return fmt.Errorf("no D-Bus connection")
		}
	}

	// Quick health check - Ping the bus
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.Peer.Ping", 0).Err
	if err != nil {
		// Connection is stale, attempt reconnect
		log.Printf("D-Bus connection stale: %v", err)
		p.reconnect()

		p.mu.RLock()
		conn = p.conn
		p.mu.RUnlock()
		if conn == nil {
			return fmt.Errorf("D-Bus reconnection failed")
		}
	}

	return nil
}

// allowedMethods is the whitelist of MPRIS methods we allow calling.
// This prevents accidental or malicious calls to dangerous methods like OpenUri or Quit.
var allowedMethods = map[string]bool{
	"Play":      true,
	"Pause":     true,
	"PlayPause": true,
	"Next":      true,
	"Previous":  true,
	"Stop":      true,
}

// Call executes a D-Bus method with timeout and automatic reconnection.
// Only methods in the allowlist are permitted.
func (p *DBusPlayer) Call(method string) error {
	// Security: validate method against allowlist
	if !allowedMethods[method] {
		return fmt.Errorf("method %q not allowed", method)
	}

	if err := p.ensureConnected(); err != nil {
		return err
	}

	p.mu.RLock()
	conn := p.conn
	p.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no D-Bus connection")
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbusTimeout)
	defer cancel()

	obj := conn.Object(spotifyDest, spotifyObjPath)
	call := obj.CallWithContext(ctx, playerInterface+"."+method, 0)

	if call.Err != nil {
		// Don't report error if Spotify isn't running
		if strings.Contains(call.Err.Error(), "ServiceUnknown") {
			return nil
		}
		// Check for connection errors that warrant reconnection
		if isConnectionError(call.Err) {
			p.reconnect()
		}
		return fmt.Errorf("D-Bus call %s failed: %w", method, call.Err)
	}
	return nil
}

// GetMetadata returns current track info with timeout and automatic reconnection
func (p *DBusPlayer) GetMetadata() (artist, title, status string, err error) {
	if err := p.ensureConnected(); err != nil {
		return "", "", "", nil // Graceful degradation
	}

	p.mu.RLock()
	conn := p.conn
	p.mu.RUnlock()

	if conn == nil {
		return "", "", "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()

	obj := conn.Object(spotifyDest, spotifyObjPath)

	// Get playback status
	statusCall := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, playerInterface, "PlaybackStatus")
	if statusCall.Err == nil {
		var variant dbus.Variant
		if err := statusCall.Store(&variant); err == nil {
			if s, ok := variant.Value().(string); ok {
				status = s
			}
		}
	} else if isConnectionError(statusCall.Err) {
		p.reconnect()
		return "", "", "", nil
	}

	// Get metadata
	metaCall := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, playerInterface, "Metadata")
	if metaCall.Err != nil {
		if isConnectionError(metaCall.Err) {
			p.reconnect()
		}
		return "", "", status, nil
	}

	var metaVariant dbus.Variant
	if err := metaCall.Store(&metaVariant); err != nil {
		return "", "", status, nil
	}

	metadata, ok := metaVariant.Value().(map[string]dbus.Variant)
	if !ok {
		return "", "", status, nil
	}

	if v, ok := metadata["xesam:artist"]; ok {
		if artists, ok := v.Value().([]string); ok && len(artists) > 0 {
			artist = strings.Join(artists, ", ")
		}
	}
	if v, ok := metadata["xesam:title"]; ok {
		if t, ok := v.Value().(string); ok {
			title = t
		}
	}

	return artist, title, status, nil
}

// Close closes the D-Bus connection
func (p *DBusPlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
}

// isConnectionError checks if an error indicates a broken D-Bus connection
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "no such") ||
		strings.Contains(errStr, "EOF")
}

// Quit asks Spotify to quit gracefully via MPRIS D-Bus interface.
// This is the proper way to terminate a media player per the MPRIS spec.
func (p *DBusPlayer) Quit() error {
	if err := p.ensureConnected(); err != nil {
		return err
	}

	p.mu.RLock()
	conn := p.conn
	p.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no D-Bus connection")
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbusTimeout)
	defer cancel()

	// Call org.mpris.MediaPlayer2.Quit (root interface, not Player)
	obj := conn.Object(spotifyDest, spotifyObjPath)
	call := obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Quit", 0)

	if call.Err != nil {
		// Ignore if Spotify isn't running
		if strings.Contains(call.Err.Error(), "ServiceUnknown") {
			return nil
		}
		if isConnectionError(call.Err) {
			p.reconnect()
		}
		return fmt.Errorf("D-Bus Quit failed: %w", call.Err)
	}
	return nil
}
