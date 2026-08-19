//go:build !windows

package autostart

import "errors"

// Register 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Register(exePath string) error {
	return errors.New("autostart: not supported on this platform")
}

// Unregister 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Unregister() error {
	return errors.New("autostart: not supported on this platform")
}
