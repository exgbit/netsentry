//go:build windows

package netclientinstall

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"netsentry/internal/winexec"
)

// 下面几个超时给的是"正常情况绰绰有余、异常情况不至于无限等"的量级——真机踩过
// 坑:一次错误的下载物(GUI 安装包)在隐藏窗口里等人点击,CombinedOutput 挂死
// 9 分钟毫无报错,人工排查半天才定位到。正常的 netclient install(注册服务+拷贝
// 文件)和 join(一次 HTTPS 注册请求+重启 daemon)真机实测都在几十秒内完成。
const (
	installTimeout   = 4 * time.Minute
	joinTimeout      = 4 * time.Minute
	uninstallTimeout = 3 * time.Minute
)

// runWithTimeout 以隐藏窗口+超时的方式跑一个子进程,超时时把进程杀掉并返回
// 明确的超时错误(带上已经捕获的输出,方便定位卡在哪)。
func runWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := winexec.HiddenContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("timed out after %s (进程已被强制结束,可能在等待一个不可见的交互界面或无响应的网络): %s", timeout, winexec.DecodeConsoleOutput(out))
	}
	return out, err
}

// installedNetclientExePath 是 netclient 自身 install 步骤把自己拷贝到的固定安装路径
// (netclient 的 functions.Install() 逻辑决定的,不是本包能选择的)。
const installedNetclientExePath = `C:\Program Files (x86)\Netclient\netclient.exe`

// installedNetclientDir 是 installedNetclientExePath 所在目录,Uninstall 收尾清理时用。
var installedNetclientDir = filepath.Dir(installedNetclientExePath)

// Run 执行"下载 → 安装 → 加入网络"三步(不含联动安装 guard 那部分——那部分需要
// main 包里的 doInstall/backup,放在 main.go 的 setup-netclient 处理逻辑里做,
// 避免 netclientinstall 反过来依赖 main 包导致循环依赖)。
//
// port/name 对应 netclient join 的 -p/--name 参数,跟内部同事手动 join 时用的参数
// 保持一致(公司内网固定用 -p 51821;--name 用来在 Netmaker 管理端区分是谁的哪台
// 设备,不给的话 netclient 自己生成的默认名字认不出是谁的机器)。两个都是空字符串
// 时不传对应的 flag,交给 netclient 自己用默认值。
func Run(token, port, name string) error {
	tmpPath, err := download(DownloadURL(PinnedVersion))
	if err != nil {
		return fmt.Errorf("download netclient: %w", err)
	}
	defer os.Remove(tmpPath)

	// 预授权防火墙规则,再跑安装——真机桌面闭环测试抓到的问题:下载下来的临时
	// 安装文件(netclient-download-*.exe)在 install 过程中会监听端口,Windows
	// 防火墙对没有规则的程序会在桌面弹"是否允许 netclient-download-12345678 访问
	// 网络"的确认框,临时文件名随机、发行者"未知",同事看到只会困惑(而且每次
	// 安装文件名都不同,每次都弹)。这里在运行前给这个具体路径加一条放行规则、
	// 装完删除,弹窗从根上不出现。setup-netclient 走到这里时已经过 ensureElevated
	// 提权,netsh 有权限执行。加删规则都是尽力而为:失败只是退回"会弹窗"的原状,
	// 不值得让整个安装因此中断。
	const installerFwRule = "NetSentry netclient installer"
	_ = winexec.Hidden("netsh.exe", "advfirewall", "firewall", "add", "rule",
		"name="+installerFwRule, "dir=in", "action=allow", "program="+tmpPath, "enable=yes").Run()
	defer func() {
		_ = winexec.Hidden("netsh.exe", "advfirewall", "firewall", "delete", "rule", "name="+installerFwRule).Run()
	}()

	if out, err := runWithTimeout(installTimeout, tmpPath, "install"); err != nil {
		return fmt.Errorf("netclient install: %w: %s", err, winexec.DecodeConsoleOutput(out))
	}

	args := []string{"join", "-t", token}
	if port != "" {
		args = append(args, "-p", port)
	}
	if name != "" {
		args = append(args, "--name", name)
	}
	if out, err := runWithTimeout(joinTimeout, installedNetclientExePath, args...); err != nil {
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
//
// 真机验证过:netclient 自己的 uninstall 会删掉服务/配置/日志,但删不掉自己
// 这个正在被执行的 netclient.exe 文件本身(Windows 不允许进程删除自己的镜像
// 文件,NetSentry 当初解决自身同样的问题时也踩过这个坑,见 internal/selfcleanup)。
// 上面 CombinedOutput() 已经等 netclient.exe uninstall 这个子进程完全退出才会
// 返回,它对自身文件的句柄这时候已经释放,轮不到 NetSentry(不是同一个进程)
// 自己再去检测——直接删掉剩下的整个目录即可,不需要额外的进程存活检查。
// Installed 报告本机是否已经存在安装好的 netclient.exe(固定安装路径)。
func Installed() bool {
	_, err := os.Stat(installedNetclientExePath)
	return err == nil
}

func Uninstall() error {
	if _, err := os.Stat(installedNetclientExePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", installedNetclientExePath, err)
	}

	out, err := runWithTimeout(uninstallTimeout, installedNetclientExePath, "uninstall")
	if err != nil {
		return fmt.Errorf("netclient uninstall: %w: %s", err, winexec.DecodeConsoleOutput(out))
	}

	// 带重试的收尾删除——真机踩过的竞态:`netclient uninstall` 命令返回时,WinSW
	// 包装进程的退出和服务注销还在异步收尾(winsw.err.log 等文件句柄要过几秒才
	// 释放),立刻 RemoveAll 会撞上 "being used by another process"。实测几秒后
	// 就能删掉,这里最多等 30 秒,足够覆盖正常收尾时间,又不会在真出问题时挂太久。
	var rmErr error
	for i := 0; i < 15; i++ {
		if rmErr = os.RemoveAll(installedNetclientDir); rmErr == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("remove leftover %s: %w", installedNetclientDir, rmErr)
}

// download 把 url 下载到一个带 .exe 后缀的临时文件,返回临时文件路径。失败自动
// 重试最多 3 次——真机踩过的坑:下载中途连接被 CDN 掐断(HTTP/2 stream
// PROTOCOL_ERROR,一次触发场景是控制台被鼠标点击进入"选择模式"、进程输出被
// 挂起几分钟导致流超时被服务端重置;不冻结时这类瞬时网络错误也可能偶发),
// 之前一次失败就让整个 setup-netclient 前功尽弃,重试一次几乎总能成功。
func download(url string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			fmt.Printf("下载失败,%d 秒后重试(第 %d/3 次): %v\n", 3, attempt, lastErr)
			time.Sleep(3 * time.Second)
		}
		tmpPath, err := downloadOnce(url)
		if err == nil {
			return tmpPath, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// downloadOnce 执行单次下载尝试。
func downloadOnce(url string) (string, error) {
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
