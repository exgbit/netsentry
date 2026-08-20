//go:build !windows

package netpriority

import "errors"

// Fix 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Fix() (Result, error) {
	return Result{}, errors.New("netpriority: not supported on this platform")
}
