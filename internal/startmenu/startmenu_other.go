//go:build !windows

package startmenu

import "errors"

// Register 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Register(trayExePath string) error {
	return errors.New("startmenu: not supported on this platform")
}

// Unregister 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Unregister() error {
	return errors.New("startmenu: not supported on this platform")
}
