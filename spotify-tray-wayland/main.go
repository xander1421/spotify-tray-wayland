package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

const (
	spotifyDest     = "org.mpris.MediaPlayer2.spotify"
	spotifyObjPath  = "/org/mpris/MediaPlayer2"
	playerInterface = "org.mpris.MediaPlayer2.Player"
	updateInterval  = 3 * time.Second
)

// App holds the application state
type App struct {
	conn       *dbus.Conn
	mTrackInfo *systray.MenuItem
	mShowHide  *systray.MenuItem
	mPrev      *systray.MenuItem
	mPlayPause *systray.MenuItem
	mNext      *systray.MenuItem
	mQuit      *systray.MenuItem

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var app *App

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to D-Bus: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	app = &App{
		conn:   conn,
		ctx:    ctx,
		cancel: cancel,
	}

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigChan:
			systray.Quit()
		case <-ctx.Done():
		}
		signal.Stop(sigChan)
	}()

	systray.Run(app.onReady, app.onExit)
}

func (a *App) onReady() {
	systray.SetIcon(getSpotifyIcon())
	systray.SetTitle("Spotify")
	systray.SetTooltip("Spotify - Click for controls")

	// Track info at top (disabled, just for display)
	a.mTrackInfo = systray.AddMenuItem("Not playing", "Current track")
	a.mTrackInfo.Disable()
	systray.AddSeparator()

	// Menu items
	a.mShowHide = systray.AddMenuItem("Show/Hide Spotify", "Toggle Spotify window")
	systray.AddSeparator()

	a.mPrev = systray.AddMenuItem("⏮ Previous          Scroll ↓", "Previous track (or scroll down on icon)")
	a.mPlayPause = systray.AddMenuItem("⏯ Play/Pause   Middle Click", "Toggle playback (or middle-click icon)")
	a.mNext = systray.AddMenuItem("⏭ Next               Scroll ↑", "Next track (or scroll up on icon)")

	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("Quit", "Quit Spotify")

	// Set up scroll handler: scroll up = next, scroll down = previous
	systray.SetOnScroll(func(direction systray.ScrollDirection) {
		switch direction {
		case systray.ScrollUp:
			a.callMethod("Next")
		case systray.ScrollDown:
			a.callMethod("Previous")
		}
	})

	// Set up middle-click handler: play/pause
	systray.SetOnMiddleTapped(func() {
		a.callMethod("PlayPause")
	})

	// Update tooltip with current track
	a.wg.Add(2)
	go a.updateLoop()
	go a.handleClicks()
}

func (a *App) onExit() {
	a.cancel()
	a.wg.Wait()
}

func (a *App) handleClicks() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.mShowHide.ClickedCh:
			a.toggleWindow()
		case <-a.mPrev.ClickedCh:
			a.callMethod("Previous")
		case <-a.mPlayPause.ClickedCh:
			a.callMethod("PlayPause")
		case <-a.mNext.ClickedCh:
			a.callMethod("Next")
		case <-a.mQuit.ClickedCh:
			_ = exec.Command("pkill", "spotify").Run()
			systray.Quit()
		}
	}
}

func (a *App) callMethod(method string) {
	if a.conn == nil {
		return
	}
	obj := a.conn.Object(spotifyDest, spotifyObjPath)
	call := obj.Call(playerInterface+"."+method, 0)
	if call.Err != nil {
		fmt.Fprintf(os.Stderr, "D-Bus call %s failed: %v\n", method, call.Err)
	}
}

func (a *App) getMetadata() (artist, title, status string) {
	if a.conn == nil {
		return "", "", ""
	}

	obj := a.conn.Object(spotifyDest, spotifyObjPath)

	// Get status
	statusVar, err := obj.GetProperty(playerInterface + ".PlaybackStatus")
	if err == nil {
		if s, ok := statusVar.Value().(string); ok {
			status = s
		}
	}

	// Get metadata
	metaVar, err := obj.GetProperty(playerInterface + ".Metadata")
	if err != nil {
		return "", "", status
	}

	metadata, ok := metaVar.Value().(map[string]dbus.Variant)
	if !ok {
		return "", "", status
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

	return artist, title, status
}

func (a *App) updateLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	// Initial update
	a.updateTooltip()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.updateTooltip()
		}
	}
}

func (a *App) updateTooltip() {
	artist, title, status := a.getMetadata()
	if title != "" {
		icon := "▶"
		if status == "Paused" {
			icon = "⏸"
		}
		tooltip := fmt.Sprintf("%s %s - %s", icon, title, artist)
		systray.SetTooltip(tooltip)

		// Update menu track info (if menu is initialized)
		if a.mTrackInfo != nil {
			trackInfo := fmt.Sprintf("%s %s", icon, title)
			if artist != "" {
				trackInfo = fmt.Sprintf("%s %s - %s", icon, title, artist)
			}
			// Truncate if too long
			if len(trackInfo) > 40 {
				trackInfo = trackInfo[:37] + "..."
			}
			a.mTrackInfo.SetTitle(trackInfo)
		}
	} else {
		systray.SetTooltip("Spotify")
		if a.mTrackInfo != nil {
			a.mTrackInfo.SetTitle("Not playing")
		}
	}
}

func (a *App) toggleWindow() {
	out, err := exec.Command("hyprctl", "clients", "-j").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hyprctl failed: %v\n", err)
		return
	}

	var clients []struct {
		Address   string `json:"address"`
		Class     string `json:"class"`
		Workspace struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"workspace"`
	}

	if err := json.Unmarshal(out, &clients); err != nil {
		fmt.Fprintf(os.Stderr, "JSON parse failed: %v\n", err)
		return
	}

	for _, client := range clients {
		if strings.EqualFold(client.Class, "spotify") {
			addr := "address:" + client.Address
			if strings.HasPrefix(client.Workspace.Name, "special") {
				// Show: move from special workspace to current
				_ = exec.Command("hyprctl", "dispatch", "movetoworkspacesilent", "e+0,"+addr).Run()
				_ = exec.Command("hyprctl", "dispatch", "focuswindow", addr).Run()
			} else {
				// Hide: move to special workspace
				_ = exec.Command("hyprctl", "dispatch", "movetoworkspacesilent", "special:spotify,"+addr).Run()
			}
			return
		}
	}

	// Spotify not found, launch it
	_ = exec.Command("spotify").Start()
}

func getSpotifyIcon() []byte {
	home, _ := os.UserHomeDir()
	iconPaths := []string{
		"/usr/share/icons/hicolor/symbolic/apps/spotify-symbolic.svg",
		"/usr/share/icons/hicolor/22x22/apps/spotify.png",
		"/usr/share/icons/hicolor/24x24/apps/spotify.png",
		"/usr/share/icons/hicolor/32x32/apps/spotify.png",
		"/usr/share/icons/hicolor/48x48/apps/spotify.png",
		"/usr/share/icons/hicolor/128x128/apps/spotify.png",
		home + "/.local/share/spotify-launcher/install/usr/share/spotify/icons/spotify-linux-32.png",
		home + "/.local/share/spotify-launcher/install/usr/share/spotify/icons/spotify-linux-48.png",
	}

	for _, path := range iconPaths {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
	}

	fmt.Fprintln(os.Stderr, "Warning: no Spotify icon found")
	return nil
}
