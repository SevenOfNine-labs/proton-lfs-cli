module github.com/SevenOfNine-labs/proton-lfs-cli

go 1.26.0

// Keep the go tool out of the vendored JS/Go trees: the SDK's node_modules
// ships Go sources (flatted) and submodules/git-lfs is a separate module.
ignore (
	./submodules
	node_modules
)

require (
	fyne.io/systray v1.12.2
	github.com/gofrs/flock v0.13.2
)

require (
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/sys v0.49.0 // indirect
)
