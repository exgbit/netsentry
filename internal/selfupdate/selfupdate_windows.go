//go:build windows

package selfupdate

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// checkInterval 是两次真实检查之间的最小间隔。watch 每 5 分钟触发一次,这里
// 节流到每小时一次——version.json 只有约 200 字节且走内网镜像,这个频率的开销
// 可以忽略,换来"发布后全部客户端最迟 1 小时内完成升级"。
const checkInterval = time.Hour

// httpClient 带整体超时:version.json 很小,exe 约 11MB,内网镜像几秒就能拉完;
// 3 分钟上限防的是"镜像挂了/网络黑洞时 watch 进程被吊死"(下载卡死导致整条
// 调用链无限等待的坑,这个项目在 netclient 安装包下载上踩过一次,不再重犯)。
var httpClient = &http.Client{Timeout: 3 * time.Minute}

// Result 描述一次 Run 的结果。
type Result struct {
	Updated bool
	Detail  string
}

// Run 执行一轮升级检查。baseURL 是镜像根地址(其下应有 version.json 和两个 exe);
// currentVersion 是当前运行版本;dir 是安装目录(guardDir)。节流、无新版本、
// 升级源未配置都不算错误,返回 Updated=false 的 Result。
func Run(baseURL, currentVersion, dir string) (Result, error) {
	if baseURL == "" || baseURL == "disabled" {
		return Result{Detail: "升级源未配置,跳过"}, nil
	}

	stampPath := filepath.Join(dir, "last-update-check.txt")
	stamp, _ := os.ReadFile(stampPath)
	if !ShouldCheck(string(stamp), time.Now(), checkInterval) {
		return Result{Detail: "距上次检查不足间隔,跳过"}, nil
	}
	// 检查一开始就写时间戳,而不是成功后才写——镜像临时不可用时,不希望之后
	// 每 5 分钟的 watch 都去重试一次,失败也等下一个间隔再说。
	_ = os.WriteFile(stampPath, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)

	// 清理历次升级留下的 .old / .failed 文件。可能仍被没重启的旧进程占用而删
	// 不掉,失败忽略、下一轮再试(旧进程退出后总会清掉)。
	for _, pattern := range []string{"*.exe.old*", "*.exe.failed"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, p := range matches {
			_ = os.Remove(p)
		}
	}

	manifestBytes, err := fetch(baseURL + "/version.json")
	if err != nil {
		return Result{}, fmt.Errorf("fetch version.json: %w", err)
	}
	// 先验签再解析使用:清单必须能用编译进本二进制的公钥验证通过,否则直接
	// 拒绝——镜像服务器不在信任链里,它只是个文件搬运工。
	sigBytes, err := fetch(baseURL + "/version.json.sig")
	if err != nil {
		return Result{}, fmt.Errorf("fetch version.json.sig: %w", err)
	}
	if err := VerifyManifestSignature(manifestBytes, string(sigBytes), UpdatePublicKeyHex); err != nil {
		return Result{}, err
	}
	m, err := ParseManifest(manifestBytes)
	if err != nil {
		return Result{}, err
	}
	if m.Version == currentVersion {
		return Result{Detail: "已是最新版本 " + currentVersion}, nil
	}
	// 只升不降:重放旧的已签名清单不能把机器退回旧版本。回滚要用旧代码发一个
	// 更高的版本号(scripts/release.sh 一条命令)。
	if newer, err := IsNewerVersion(m.Version, currentVersion); err != nil {
		return Result{}, err
	} else if !newer {
		return Result{Detail: fmt.Sprintf("镜像版本 %s 不高于当前 %s,防降级跳过", m.Version, currentVersion)}, nil
	}

	// 先把所有文件下载并校验到 .new,全部就绪后再统一换入——避免下到一半失败
	// 时出现两个 exe 版本不一致的中间态。
	//
	// 下载路径带版本号(<baseURL>/<版本>/<文件名>):版本目录里的文件一旦发布
	// 永不变更,可以被任何中间层放心缓存;只有 version.json 是可变的(镜像上
	// 对它禁用了缓存),不存在"版本号更新了但抓到的还是旧 exe"的错配。
	for name, wantSum := range m.Files {
		data, err := fetch(baseURL + "/" + m.Version + "/" + name)
		if err != nil {
			return Result{}, fmt.Errorf("download %s: %w", name, err)
		}
		if got := SHA256Hex(data); got != wantSum {
			return Result{}, fmt.Errorf("%s sha256 mismatch: got %s want %s", name, got, wantSum)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".new"), data, 0o755); err != nil {
			return Result{}, fmt.Errorf("write %s.new: %w", name, err)
		}
	}

	// 逐个换入:当前 exe(可能正在运行)改名成 .old-<时间戳>,再把 .new 放到
	// 原位。第二个失败时尽力把第一个滚回去,保持两个 exe 版本一致。
	//
	// .old 名字必须唯一,不能用固定的 ".old":上一次升级的 .old 可能仍被一直
	// 没重启的旧进程(典型是托盘)占用着,锁定中的文件既删不掉、也不能作为
	// 改名的目标被覆盖——固定名字会让第二次升级永远失败,直到那台机器重启。
	var swapped []swapEntry
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for name := range m.Files {
		cur := filepath.Join(dir, name)
		old := cur + ".old-" + ts
		if err := os.Rename(cur, old); err != nil && !os.IsNotExist(err) {
			rollback(swapped)
			return Result{}, fmt.Errorf("rename %s to .old: %w", name, err)
		}
		if err := os.Rename(cur+".new", cur); err != nil {
			_ = os.Rename(old, cur)
			rollback(swapped)
			return Result{}, fmt.Errorf("move %s.new into place: %w", name, err)
		}
		swapped = append(swapped, swapEntry{cur: cur, old: old})
	}

	return Result{
		Updated: true,
		Detail:  fmt.Sprintf("已从 %s 升级到 %s(正在运行的进程在下次启动时用上新版本)", currentVersion, m.Version),
	}, nil
}

// swapEntry 记录一次换入:cur 是 exe 原位路径,old 是旧版本被改名后的路径。
type swapEntry struct{ cur, old string }

// rollback 把已经换入新版本的文件滚回原来的版本。
func rollback(swapped []swapEntry) {
	for _, s := range swapped {
		_ = os.Rename(s.cur, s.cur+".failed")
		_ = os.Rename(s.old, s.cur)
	}
}

// fetch 拉取一个 URL 的完整内容。
func fetch(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http get %s: unexpected status %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
