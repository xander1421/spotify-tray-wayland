module spotify-tray-wayland

go 1.25

require (
	fyne.io/systray v1.11.0
	github.com/godbus/dbus/v5 v5.1.0
)

require golang.org/x/sys v0.15.0 // indirect

replace fyne.io/systray => github.com/xander1421/systray v0.0.0-20260201001841-1f6cb4a91604
