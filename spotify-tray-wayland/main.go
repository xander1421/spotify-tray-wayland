package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
)

const (
	updateInterval     = 3 * time.Second
	specialWorkspace   = "special:spotify"
	currentWorkspace   = "e+0" // Hyprland: relative to current
)

// App holds the application state with dependency injection
type App struct {
	wm     WindowManager
	player MediaPlayer

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
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	player, err := NewDBusPlayer()
	if err != nil {
		log.Fatalf("Failed to connect to D-Bus: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	app = &App{
		wm:     NewHyprlandManager(),
		player: player,
		ctx:    ctx,
		cancel: cancel,
	}

	// Handle signals - NOT in WaitGroup to avoid deadlock
	// (systray.Quit -> onExit -> wg.Wait would deadlock if this goroutine is in wg)
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

	// Non-blocking scroll handler
	systray.SetOnScroll(func(direction systray.ScrollDirection) {
		go func() {
			switch direction {
			case systray.ScrollUp:
				_ = a.player.Call("Next")
			case systray.ScrollDown:
				_ = a.player.Call("Previous")
			}
		}()
	})

	// Non-blocking middle-click handler
	systray.SetOnMiddleTapped(func() {
		go func() {
			_ = a.player.Call("PlayPause")
		}()
	})

	// Ensure Spotify is visible if already running but hidden
	go a.ensureSpotifyVisible(true)

	// Start background loops
	a.wg.Add(2)
	go a.updateLoop()
	go a.handleClicks()
}

func (a *App) onExit() {
	a.cancel()
	if a.player != nil {
		a.player.Close()
	}
	a.wg.Wait()
}

func (a *App) handleClicks() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.mShowHide.ClickedCh:
			go a.toggleWindow()
		case <-a.mPrev.ClickedCh:
			go func() { _ = a.player.Call("Previous") }()
		case <-a.mPlayPause.ClickedCh:
			go func() { _ = a.player.Call("PlayPause") }()
		case <-a.mNext.ClickedCh:
			go func() { _ = a.player.Call("Next") }()
		case <-a.mQuit.ClickedCh:
			go a.quitSpotify()
		}
	}
}

func (a *App) quitSpotify() {
	// Pause playback first
	_ = a.player.Call("Pause")
	// Close window cleanly
	_ = a.wm.CloseWindow("spotify")
	time.Sleep(500 * time.Millisecond)
	// Force kill if still running
	KillSpotify()
	systray.Quit()
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
	artist, title, status, _ := a.player.GetMetadata()

	if a.mTrackInfo == nil {
		return
	}

	if title != "" {
		icon := "▶"
		if status == "Paused" {
			icon = "⏸"
		}
		tooltip := fmt.Sprintf("%s %s - %s", icon, title, artist)
		systray.SetTooltip(tooltip)

		// Update menu track info
		trackInfo := fmt.Sprintf("%s %s", icon, title)
		if artist != "" {
			trackInfo = fmt.Sprintf("%s %s - %s", icon, title, artist)
		}
		// Truncate if too long
		if len(trackInfo) > 40 {
			trackInfo = trackInfo[:37] + "..."
		}
		a.mTrackInfo.SetTitle(trackInfo)
	} else {
		systray.SetTooltip("Spotify")
		a.mTrackInfo.SetTitle("Not playing")
	}
}

func (a *App) toggleWindow() {
	client, found := a.wm.FindSpotify()
	if !found {
		// Spotify not found, launch it
		if err := a.wm.LaunchSpotify(); err == nil {
			go a.ensureSpotifyVisible(true)
		}
		return
	}

	addr := "address:" + client.Address

	if strings.HasPrefix(client.Workspace.Name, "special") {
		// Show: move from special workspace to current
		if err := a.wm.MoveWindow(addr, currentWorkspace); err == nil {
			_ = a.wm.FocusWindow(addr)
		}
	} else {
		// Hide: move to special workspace
		_ = a.wm.MoveWindow(addr, specialWorkspace)
	}
}

// ensureSpotifyVisible waits for Spotify window and moves it if needed
func (a *App) ensureSpotifyVisible(forceMove bool) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(10 * time.Second)

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timeout:
			return
		case <-ticker.C:
			client, found := a.wm.FindSpotify()
			if !found {
				continue
			}

			// If it spawned in special workspace, move it to current
			if forceMove && strings.HasPrefix(client.Workspace.Name, "special") {
				addr := "address:" + client.Address
				if err := a.wm.MoveWindow(addr, currentWorkspace); err == nil {
					_ = a.wm.FocusWindow(addr)
				}
			}
			return
		}
	}
}

func getSpotifyIcon() []byte {
	home, _ := os.UserHomeDir()
	iconPaths := []string{
		"/usr/share/icons/hicolor/256x256/apps/spotify.png",
		"/usr/share/icons/hicolor/128x128/apps/spotify.png",
		"/usr/share/icons/hicolor/64x64/apps/spotify.png",
		"/usr/share/icons/hicolor/48x48/apps/spotify.png",
		"/usr/share/icons/hicolor/32x32/apps/spotify.png",
		home + "/.local/share/spotify-launcher/install/usr/share/spotify/icons/spotify-linux-128.png",
		home + "/.local/share/spotify-launcher/install/usr/share/spotify/icons/spotify-linux-64.png",
	}

	for _, path := range iconPaths {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
	}

	log.Println("Warning: no Spotify icon found")
	return nil
}
