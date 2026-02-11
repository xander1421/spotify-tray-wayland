package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	spotifyDest     = "org.mpris.MediaPlayer2.spotify"
	spotifyObjPath  = "/org/mpris/MediaPlayer2"
	playerInterface = "org.mpris.MediaPlayer2.Player"
	dbusTimeout     = 1 * time.Second
	metadataTimeout = 500 * time.Millisecond
)

// DBusPlayer implements MediaPlayer using D-Bus
type DBusPlayer struct {
	conn *dbus.Conn
}

// NewDBusPlayer creates a new D-Bus media player connection
func NewDBusPlayer() (*DBusPlayer, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to D-Bus: %w", err)
	}
	return &DBusPlayer{conn: conn}, nil
}

// Call executes a D-Bus method with timeout
func (p *DBusPlayer) Call(method string) error {
	if p.conn == nil {
		return fmt.Errorf("no D-Bus connection")
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbusTimeout)
	defer cancel()

	obj := p.conn.Object(spotifyDest, spotifyObjPath)
	call := obj.CallWithContext(ctx, playerInterface+"."+method, 0)

	if call.Err != nil {
		// Don't report error if Spotify isn't running
		if !strings.Contains(call.Err.Error(), "ServiceUnknown") {
			return fmt.Errorf("D-Bus call %s failed: %w", method, call.Err)
		}
	}
	return nil
}

// GetMetadata returns current track info with timeout
func (p *DBusPlayer) GetMetadata() (artist, title, status string, err error) {
	if p.conn == nil {
		return "", "", "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()

	obj := p.conn.Object(spotifyDest, spotifyObjPath)

	// Get playback status
	statusCall := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, playerInterface, "PlaybackStatus")
	if statusCall.Err == nil {
		var variant dbus.Variant
		if err := statusCall.Store(&variant); err == nil {
			if s, ok := variant.Value().(string); ok {
				status = s
			}
		}
	}

	// Get metadata
	metaCall := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, playerInterface, "Metadata")
	if metaCall.Err != nil {
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
	if p.conn != nil {
		p.conn.Close()
	}
}

// KillSpotify force-kills the Spotify process
func KillSpotify() {
	_ = exec.Command("pkill", "-9", "spotify").Run()
}
