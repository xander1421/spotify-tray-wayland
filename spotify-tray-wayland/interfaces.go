package main

// WindowManager abstracts window management operations (Hyprland, etc.)
type WindowManager interface {
	GetClients() ([]HyprlandClient, error)
	FindSpotify() (HyprlandClient, bool)
	MoveWindow(address, workspace string) error
	FocusWindow(address string) error
	CloseWindow(class string) error
	LaunchSpotify() error
}

// MediaPlayer abstracts media player operations (D-Bus/MPRIS)
type MediaPlayer interface {
	GetMetadata() (artist, title, status string, err error)
	Call(method string) error
	Close()
}

// HyprlandClient represents a window from Hyprland's client list
type HyprlandClient struct {
	Address   string
	Class     string
	Title     string
	Workspace struct {
		ID   int
		Name string
	}
}
