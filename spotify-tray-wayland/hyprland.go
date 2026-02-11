package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// HyprlandManager implements WindowManager for Hyprland
type HyprlandManager struct{}

// NewHyprlandManager creates a new HyprlandManager
func NewHyprlandManager() *HyprlandManager {
	return &HyprlandManager{}
}

// GetClients returns all Hyprland clients by parsing text output
func (h *HyprlandManager) GetClients() ([]HyprlandClient, error) {
	out, err := exec.Command("hyprctl", "clients").Output()
	if err != nil {
		return nil, fmt.Errorf("hyprctl clients failed: %w", err)
	}

	return parseHyprctlClients(out), nil
}

// parseHyprctlClients parses hyprctl clients text output efficiently
func parseHyprctlClients(data []byte) []HyprlandClient {
	var clients []HyprlandClient
	var current *HyprlandClient

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		// New window entry: "Window <address> -> <title>:"
		if strings.HasPrefix(line, "Window ") {
			if current != nil {
				clients = append(clients, *current)
			}
			current = &HyprlandClient{}

			// Parse: "Window 560770936780 -> title:"
			parts := strings.SplitN(line, " -> ", 2)
			if len(parts) >= 1 {
				// Extract address (hex without 0x prefix in output)
				addrPart := strings.TrimPrefix(parts[0], "Window ")
				current.Address = "0x" + strings.TrimSpace(addrPart)
			}
			if len(parts) >= 2 {
				current.Title = strings.TrimSuffix(parts[1], ":")
			}
			continue
		}

		if current == nil {
			continue
		}

		// Parse indented fields
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "class: ") {
			current.Class = strings.TrimPrefix(line, "class: ")
		} else if strings.HasPrefix(line, "workspace: ") {
			// Parse: "workspace: -98 (special:scratchpad)" or "workspace: 1 (1)"
			ws := strings.TrimPrefix(line, "workspace: ")
			if idx := strings.Index(ws, " ("); idx != -1 {
				// Extract ID
				idStr := ws[:idx]
				fmt.Sscanf(idStr, "%d", &current.Workspace.ID)
				// Extract name from parentheses
				if endIdx := strings.LastIndex(ws, ")"); endIdx > idx {
					current.Workspace.Name = ws[idx+2 : endIdx]
				}
			}
		}
	}

	// Don't forget the last client
	if current != nil {
		clients = append(clients, *current)
	}

	return clients
}

// MoveWindow moves a window to the specified workspace
func (h *HyprlandManager) MoveWindow(address, workspace string) error {
	const maxRetries = 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		out, err := exec.Command("hyprctl", "dispatch", "movetoworkspacesilent", workspace+","+address).Output()
		if err == nil && strings.TrimSpace(string(out)) == "ok" {
			return nil
		}
		if attempt == maxRetries {
			return fmt.Errorf("failed to move window after %d attempts: %v (output: %q)", maxRetries, err, string(out))
		}
	}
	return nil
}

// FocusWindow focuses the specified window
func (h *HyprlandManager) FocusWindow(address string) error {
	out, err := exec.Command("hyprctl", "dispatch", "focuswindow", address).Output()
	if err != nil {
		return fmt.Errorf("failed to focus window: %w", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("focuswindow returned: %q", string(out))
	}
	return nil
}

// CloseWindow closes a window by class
func (h *HyprlandManager) CloseWindow(class string) error {
	return exec.Command("hyprctl", "dispatch", "closewindow", "class:"+class).Run()
}

// LaunchSpotify starts the Spotify application
func (h *HyprlandManager) LaunchSpotify() error {
	cmd := exec.Command("spotify")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch spotify: %w", err)
	}
	// Reap the process in background
	go func() { _ = cmd.Wait() }()
	return nil
}
