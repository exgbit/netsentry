// Package backup 负责在 netclient 配置一致时保存一份"已知良好"快照,供 watch 包在配置损坏时恢复。
package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"netsentry/internal/guardconfig"
)

// Outcome 描述一次 backup 执行的结果。
// 仅当 Run 返回的 error 为 nil 时,Outcome 才有意义;error 非 nil 时调用方应先处理 error,
// 此时返回的 Outcome 是零值,不代表任何具体状态。
type Outcome int

const (
	OutcomeSkippedMissing Outcome = iota
	OutcomeSkippedInconsistent
	OutcomeSaved
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSkippedMissing:
		return "skipped: netclient.json or servers.json missing"
	case OutcomeSkippedInconsistent:
		return "skipped: currently inconsistent, not overwriting known-good backup"
	case OutcomeSaved:
		return "ok"
	default:
		return "unknown"
	}
}

// Run 检查 netclientDir 下的配置是否一致,一致则把两个文件复制到 backupDir 作为"已知良好"快照。
// 不一致或文件缺失时跳过,绝不用坏状态覆盖已有的良好备份。
//
// 两个文件的复制不是完全原子的(两次 rename 无法合并成一次操作),但会先把两个文件都复制到
// 临时文件,确认都成功后才依次 rename 到位——这样第二个文件复制失败(磁盘满、权限变化等)
// 不会导致已有的良好备份对被破坏成"一半新一半旧"的不一致状态。
func Run(netclientDir, backupDir string) (Outcome, error) {
	var zero Outcome

	load, err := guardconfig.Load(netclientDir)
	if err != nil {
		return zero, err
	}
	if !load.NetclientExists || !load.ServersExists {
		return OutcomeSkippedMissing, nil
	}
	if !load.Consistent {
		return OutcomeSkippedInconsistent, nil
	}

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return zero, fmt.Errorf("create backup dir: %w", err)
	}

	ncGood := filepath.Join(backupDir, "netclient.json.good")
	srvGood := filepath.Join(backupDir, "servers.json.good")
	ncTmp := ncGood + ".tmp"
	srvTmp := srvGood + ".tmp"

	if err := copyToTmp(filepath.Join(netclientDir, "netclient.json"), ncTmp); err != nil {
		return zero, fmt.Errorf("backup netclient.json: %w", err)
	}
	if err := copyToTmp(filepath.Join(netclientDir, "servers.json"), srvTmp); err != nil {
		return zero, fmt.Errorf("backup servers.json: %w", err)
	}
	if err := os.Rename(ncTmp, ncGood); err != nil {
		return zero, fmt.Errorf("finalize netclient.json.good: %w", err)
	}
	if err := os.Rename(srvTmp, srvGood); err != nil {
		return zero, fmt.Errorf("finalize servers.json.good: %w", err)
	}

	stamp := time.Now().Format("2006-01-02 15:04:05")
	if err := os.WriteFile(filepath.Join(backupDir, "last-good.txt"), []byte(stamp), 0o644); err != nil {
		return zero, fmt.Errorf("write last-good.txt: %w", err)
	}
	return OutcomeSaved, nil
}

// copyToTmp 把 src 的内容复制到 tmpDst,不做 rename。
func copyToTmp(src, tmpDst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(tmpDst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
