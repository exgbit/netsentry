//go:build !windows

package elevate

import "errors"

// IsElevated 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func IsElevated() (bool, error) {
	return false, errors.New("elevate: not supported on this platform")
}

// RelaunchElevated 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func RelaunchElevated(args []string) error {
	return errors.New("elevate: not supported on this platform")
}
