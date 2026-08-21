//go:build windows

package netclientinstall

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

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
		return fmt.Errorf("netclient install: %w: %s", err, winexec.DecodeConsoleOutput(out))
	}

	if out, err := winexec.Hidden(installedNetclientExePath, "join", "-t", token).CombinedOutput(); err != nil {
		return fmt.Errorf("netclient join: %w: %s", err, winexec.DecodeConsoleOutput(out))
	}

	return nil
}

// Uninstall 卸载已经安装的 netclient 本体(`netclient.exe uninstall`,真机验证过
// v1.6.0 确实有这个子命令,会移除 netclient 的服务、配置文件和网络接口)。
// netclient.exe 不存在时不算错误、直接跳过——可能本来就没装,或者已经被手动
// 卸载过。这是 Run 的反向操作,配合 main.go 的 runUninstall 一起用,让
// "netsentry uninstall" 也能把它联动安装的 netclient 一起清干净,而不是卸载
// NetSentry 之后留下一个不再被管理、但还在运行的 netclient。
func Uninstall() error {
	if _, err := os.Stat(installedNetclientExePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", installedNetclientExePath, err)
	}

	out, err := winexec.Hidden(installedNetclientExePath, "uninstall").CombinedOutput()
	if err != nil {
		return fmt.Errorf("netclient uninstall: %w: %s", err, winexec.DecodeConsoleOutput(out))
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

	// 直接在终端里手动跑 setup-netclient 的时候(真机反馈过),下载这几十 MB
	// 的安装包期间命令行完全没有任何输出,看起来像卡住了。用 io.TeeReader 边
	// 写文件边打点进度,\r 覆盖同一行、不刷屏——只有在真的走终端(不是面板
	// shell 出的、被 CombinedOutput 整体缓冲吞掉的场景)时用户才看得到,但打印
	// 本身没有额外成本,不需要区分场景特殊处理。
	progress := &downloadProgress{total: resp.ContentLength}
	if _, err := io.Copy(tmp, io.TeeReader(resp.Body, progress)); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write %s: %w", tmpPath, err)
	}
	progress.finish()
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

// downloadProgress 实现 io.Writer,配合 io.TeeReader 在下载过程中打印进度。
// total<=0(服务器没给 Content-Length,比如用了 chunked 编码)时退化成只显示
// 已下载字节数,不算百分比。
type downloadProgress struct {
	total     int64
	written   int64
	lastPrint time.Time
}

func (p *downloadProgress) Write(b []byte) (int, error) {
	n := len(b)
	p.written += int64(n)
	// 每 200ms 刷新一次,不是每写一个 chunk 就打印——那样量太大,终端会被刷屏
	// 而不是看到一行在动。
	if time.Since(p.lastPrint) >= 200*time.Millisecond {
		p.print()
		p.lastPrint = time.Now()
	}
	return n, nil
}

func (p *downloadProgress) print() {
	mb := float64(p.written) / 1e6
	if p.total > 0 {
		fmt.Printf("\r下载 netclient 安装包: %.1f / %.1f MB (%.0f%%)", mb, float64(p.total)/1e6, float64(p.written)/float64(p.total)*100)
	} else {
		fmt.Printf("\r下载 netclient 安装包: %.1f MB", mb)
	}
}

// finish 打印最终进度并换行,让下载之后的日志不会接着写在同一行末尾。
func (p *downloadProgress) finish() {
	p.print()
	fmt.Println()
}
