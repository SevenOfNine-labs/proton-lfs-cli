module github.com/SevenOfNine-labs/proton-lfs-cli

go 1.25.0

// Build and CI toolchain (actions/setup-go reads this line); `go` above is
// the language minimum required by golang.org/x/sys and gofrs/flock.
toolchain go1.25.14

// Keep the go tool out of the vendored JS/Go trees: the SDK's node_modules
// ships Go sources (flatted) and submodules/git-lfs is a separate module.
ignore (
	./submodules
	node_modules
)

require (
	fyne.io/systray v1.12.2
	github.com/gofrs/flock v0.13.1
)

require (
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
