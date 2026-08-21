//go:build !windows

package netclientinstall

import "errors"

// Run 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Run(token, port, name string) error {
	return errors.New("netclientinstall: not supported on this platform")
}

// Uninstall 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Uninstall() error {
	return errors.New("netclientinstall: not supported on this platform")
}
