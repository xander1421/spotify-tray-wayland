module spotify-tray-wayland

go 1.26.0

require (
	fyne.io/systray v1.12.2
	github.com/godbus/dbus/v5 v5.2.4
)

require golang.org/x/sys v0.50.0 // indirect

replace fyne.io/systray => github.com/xander1421/systray v1.16.0
