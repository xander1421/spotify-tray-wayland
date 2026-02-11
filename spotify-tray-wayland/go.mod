module spotify-tray-wayland

go 1.26

require (
	fyne.io/systray v1.11.0
	github.com/godbus/dbus/v5 v5.2.2
)

require golang.org/x/sys v0.41.0 // indirect

replace fyne.io/systray => github.com/xander1421/systray v1.15.0
