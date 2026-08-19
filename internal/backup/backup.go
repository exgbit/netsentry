// Package backup 负责在 netclient 配置一致时保存一份"已知良好"快照,供 watch 包在配置损坏时恢复。
package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"netclient-guard/internal/guardconfig"
)

// Outcome 描述一次 backup 执行的结果。
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
func Run(netclientDir, backupDir string) (Outcome, error) {
	load, err := guardconfig.Load(netclientDir)
	if err != nil {
		return OutcomeSkippedMissing, err
	}
	if !load.NetclientExists || !load.ServersExists {
		return OutcomeSkippedMissing, nil
	}
	if !load.Consistent {
		return OutcomeSkippedInconsistent, nil
	}

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return OutcomeSaved, fmt.Errorf("create backup dir: %w", err)
	}
	if err := copyFile(filepath.Join(netclientDir, "netclient.json"), filepath.Join(backupDir, "netclient.json.good")); err != nil {
		return OutcomeSaved, fmt.Errorf("backup netclient.json: %w", err)
	}
	if err := copyFile(filepath.Join(netclientDir, "servers.json"), filepath.Join(backupDir, "servers.json.good")); err != nil {
		return OutcomeSaved, fmt.Errorf("backup servers.json: %w", err)
	}
	stamp := time.Now().Format("2006-01-02 15:04:05")
	if err := os.WriteFile(filepath.Join(backupDir, "last-good.txt"), []byte(stamp), 0o644); err != nil {
		return OutcomeSaved, fmt.Errorf("write last-good.txt: %w", err)
	}
	return OutcomeSaved, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
