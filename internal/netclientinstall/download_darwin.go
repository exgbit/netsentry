//go:build darwin

package netclientinstall

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// installedNetclientExePath 是 macOS 上 netclient 的固定安装路径(实机确认:
// `netclient install` 会把自己放到这里,并注册 com.gravitl.netclient 的
// LaunchDaemon)。
const installedNetclientExePath = "/usr/local/bin/netclient"

// netclientConfigDir 是 macOS 上 netclient 的配置目录(root 权限)。
const netclientConfigDir = "/etc/netclient"

const (
	installTimeout   = 4 * time.Minute
	joinTimeout      = 4 * time.Minute
	uninstallTimeout = 3 * time.Minute
)

// DarwinDownloadURL 构造指定版本 netclient macOS 二进制的下载地址,按当前
// CPU 架构(arm64/amd64)选择——netclient 上游没有 universal 产物。
func DarwinDownloadURL(version string) string {
	return fmt.Sprintf("https://downloads.netmaker.io/releases/download/%s/netclient-darwin-%s", version, runtime.GOARCH)
}

func runWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out := make(chan struct {
		b   []byte
		err error
	}, 1)
	go func() {
		b, err := cmd.CombinedOutput()
		out <- struct {
			b   []byte
			err error
		}{b, err}
	}()
	select {
	case r := <-out:
		return r.b, r.err
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("%s 执行超过 %s 被终止", name, timeout)
	}
}

// Run 下载 netclient、安装(注册 LaunchDaemon)并加入网络。要求以 root 运行。
func Run(token, port, name string) error {
	fmt.Println("下载 netclient(约 40MB)...")
	tmp, err := download(DarwinDownloadURL(PinnedVersion))
	if err != nil {
		return fmt.Errorf("download netclient: %w", err)
	}
	defer os.Remove(tmp)
	fmt.Println("下载完成,安装中...")

	if err := os.Rename(tmp, installedNetclientExePath); err != nil {
		// /tmp 和 /usr/local/bin 可能跨文件系统,Rename 失败退回复制
		if err := copyFile(tmp, installedNetclientExePath); err != nil {
			return fmt.Errorf("install netclient binary: %w", err)
		}
	}
	if err := os.Chmod(installedNetclientExePath, 0o755); err != nil {
		return fmt.Errorf("chmod netclient: %w", err)
	}

	if out, err := runWithTimeout(installTimeout, installedNetclientExePath, "install"); err != nil {
		return fmt.Errorf("netclient install: %w: %s", err, strings.TrimSpace(string(out)))
	}

	args := []string{"join", "-t", token}
	if port != "" {
		args = append(args, "-p", port)
	}
	if name != "" {
		args = append(args, "--name", name)
	}
	if out, err := runWithTimeout(joinTimeout, installedNetclientExePath, args...); err != nil {
		return fmt.Errorf("netclient join: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Installed 报告本机是否已经存在安装好的 netclient。
func Installed() bool {
	_, err := os.Stat(installedNetclientExePath)
	return err == nil
}

// Uninstall 卸载 netclient 本体:`netclient uninstall` 会注销 LaunchDaemon、
// 清掉配置和网络接口;二进制文件和配置目录残留在这里收尾删除(mac 上没有
// Windows WinSW 那种异步收尾竞态,直接删即可)。没装过不算错误。
func Uninstall() error {
	if !Installed() {
		return nil
	}
	if out, err := runWithTimeout(uninstallTimeout, installedNetclientExePath, "uninstall"); err != nil {
		return fmt.Errorf("netclient uninstall: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Remove(installedNetclientExePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", installedNetclientExePath, err)
	}
	if err := os.RemoveAll(netclientConfigDir); err != nil {
		return fmt.Errorf("remove %s: %w", netclientConfigDir, err)
	}
	return nil
}

// download 下载 url 到临时文件并返回路径,瞬时网络错误重试 3 次(与 Windows 版
// 同样的教训:一次失败就让整个 setup 前功尽弃不值得)。
func download(url string) (string, error) {
	var lastErr error
	for i := 1; i <= 3; i++ {
		path, err := downloadOnce(url)
		if err == nil {
			return path, nil
		}
		lastErr = err
		if i < 3 {
			fmt.Printf("下载失败,3 秒后重试(%d/3):%v\n", i, err)
			time.Sleep(3 * time.Second)
		}
	}
	return "", lastErr
}

func downloadOnce(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http get %s: unexpected status %s", url, resp.Status)
	}
	f, err := os.CreateTemp("", "netclient-download-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
