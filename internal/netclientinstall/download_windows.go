//go:build windows

package netclientinstall

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"netsentry/internal/winexec"
)

// installedNetclientExePath 是 netclient 自身 install 步骤把自己拷贝到的固定安装路径
// (netclient 的 functions.Install() 逻辑决定的,不是本包能选择的)。
const installedNetclientExePath = `C:\Program Files (x86)\Netclient\netclient.exe`

// Run 执行"下载 → 安装 → 加入网络"三步(不含联动安装 guard 那部分——那部分需要
// main 包里的 doInstall/backup,放在 main.go 的 setup-netclient 处理逻辑里做,
// 避免 netclientinstall 反过来依赖 main 包导致循环依赖)。
func Run(token string) error {
	tmpPath, err := download(DownloadURL(PinnedVersion))
	if err != nil {
		return fmt.Errorf("download netclient: %w", err)
	}
	defer os.Remove(tmpPath)

	if out, err := winexec.Hidden(tmpPath, "install").CombinedOutput(); err != nil {
		return fmt.Errorf("netclient install: %w: %s", err, out)
	}

	if out, err := winexec.Hidden(installedNetclientExePath, "join", "-t", token).CombinedOutput(); err != nil {
		return fmt.Errorf("netclient join: %w: %s", err, out)
	}

	return nil
}

// download 把 url 下载到一个带 .exe 后缀的临时文件,返回临时文件路径。
func download(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("http get %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http get %s: unexpected status %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp("", "netclient-download-*.exe")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close %s: %w", tmpPath, err)
	}

	// Windows 上可执行性不看权限位,这里设置只是跨平台卫生习惯,不是必需的。
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("chmod %s: %w", tmpPath, err)
	}

	return tmpPath, nil
}
