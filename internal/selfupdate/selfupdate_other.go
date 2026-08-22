//go:build !windows && !darwin

package selfupdate

import "errors"

// Result 描述一次 Run 的结果(与 windows 版一致,为跨平台编译提供)。
type Result struct {
	Updated bool
	Detail  string
}

// Run 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Run(baseURL, currentVersion, dir, stateDir string) (Result, error) {
	return Result{}, errors.New("selfupdate: not supported on this platform")
}
