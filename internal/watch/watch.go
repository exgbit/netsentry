// Package watch 负责检测 netclient 配置是否损坏、服务是否在运行,并在需要时自动修复。
package watch

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"netclient-guard/internal/guardconfig"
)

// Action 描述 watch 决策后应该采取的动作。
type Action int

const (
	ActionNone Action = iota
	ActionStartService
	ActionRestoreAndStart
	ActionAlertNoBackup
)

func (a Action) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionStartService:
		return "start-service"
	case ActionRestoreAndStart:
		return "restore-and-start"
	case ActionAlertNoBackup:
		return "alert-no-backup"
	default:
		return "unknown"
	}
}

// Decide 根据配置一致性、服务是否在运行、是否存在可用的已知良好备份,决定要采取的动作。
func Decide(load guardconfig.LoadResult, serviceRunning, backupAvailable bool) Action {
	if !load.Consistent {
		if backupAvailable {
			return ActionRestoreAndStart
		}
		return ActionAlertNoBackup
	}
	if !serviceRunning {
		return ActionStartService
	}
	return ActionNone
}

// ServiceController 抽象对 netclient Windows 服务的控制,方便测试用假实现替代真实的 Service Control Manager 调用。
//
// LogSize/ReadLogFrom 是为了在重启后验证服务是否真的恢复了到 broker 的连接——netclient 在 Windows
// 上有一个已知的、上游未修复的 bug(setupMQTT 手动重试与 paho 库自带的 auto-reconnect 冲突,报错
// "status can only transition to connecting from disconnected"),重启后进程可能长时间卡在反复重连、
// 最终自己退出,而 sc.exe 层面在这整个过程中都会显示 Running,直到它自己退出为止。只看服务状态判定
// "修复成功"是不够的,必须看 netclient 自己打的、连上 broker 才会出现的日志信号。
type ServiceController interface {
	IsRunning() (bool, error)
	Stop() error
	Start() error
	// LogSize 返回 netclient 输出日志(winsw.out.log)当前的字节大小。日志文件尚不存在时返回 (0, nil),
	// 不视为错误——这在 netclient 刚安装、还没运行过一次的场景下是正常状态。
	LogSize() (int64, error)
	// ReadLogFrom 读取日志文件从 offset 到末尾的新增内容。offset 超出当前文件大小(比如日志被截断/
	// 轮转)时返回空内容而不是报错。
	ReadLogFrom(offset int64) ([]byte, error)
}

// Result 描述一次 watch 执行的结果。
//
// 仅当 Run 返回的 error 为 nil 时,Result.Action 才代表"实际发生的动作"。error 非 nil 时,
// Action 要么是零值 ActionNone(表示在做出决策前就失败了,例如读配置或查服务状态出错,并不代表
// "系统健康、无需动作"),要么是本次尝试执行但失败的动作(例如 ActionStartService 但 Start 报错)。
// 两种情况都不代表系统当前处于该动作所暗示的状态,调用方必须先检查 error。
type Result struct {
	Action Action
	Detail string
}

// brokerConnectedSignal 是 netclient 在 setupMQTT 的 OnConnectHandler 里打印的日志(daemon.go 里
// `slog.Info("mqtt connect handler")`),只有真正连上 broker 才会出现,用它作为"这次重启是否真的
// 恢复了连接"的判定依据,而不是仅凭服务状态是 Running。
const brokerConnectedSignal = "mqtt connect handler"

// IsBrokerConnected 判断一段日志内容里是否包含 netclient 成功连上 broker 的信号。
func IsBrokerConnected(logTail []byte) bool {
	return bytes.Contains(logTail, []byte(brokerConnectedSignal))
}

const (
	defaultHealthCheckAttempts = 2
	defaultHealthCheckTimeout  = 45 * time.Second
	defaultHealthCheckInterval = 3 * time.Second
)

// startAndVerifyHealthy 假设服务当前已经是停止状态,启动它并轮询日志确认真的连上了 broker;
// 如果在 checkTimeout 内没等到成功信号,会重新 Stop+Start 再试一次,最多尝试 maxAttempts 次。
// 这是为了绕开 netclient 一个已知的上游 bug:重启后有时会卡在反复重连 broker、最终自己退出,
// 而全新进程(即再 Stop+Start 一次)通常能跳出这个卡死状态——这是从真实故障里观察到的行为,
// 不是理论推测。
func startAndVerifyHealthy(svc ServiceController, maxAttempts int, checkTimeout, pollInterval time.Duration) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := svc.Stop(); err != nil {
				lastErr = fmt.Errorf("attempt %d/%d: stop service before retry: %w", attempt, maxAttempts, err)
				continue
			}
		}
		offset, err := svc.LogSize()
		if err != nil {
			return fmt.Errorf("read log size before start: %w", err)
		}
		if err := svc.Start(); err != nil {
			lastErr = fmt.Errorf("attempt %d/%d: start service: %w", attempt, maxAttempts, err)
			continue
		}
		deadline := time.Now().Add(checkTimeout)
		for {
			tail, err := svc.ReadLogFrom(offset)
			if err == nil && IsBrokerConnected(tail) {
				return nil
			}
			if !time.Now().Before(deadline) {
				break
			}
			time.Sleep(pollInterval)
		}
		lastErr = fmt.Errorf("attempt %d/%d: service started but did not connect to broker within %s (known upstream netclient MQTT reconnect issue)", attempt, maxAttempts, checkTimeout)
	}
	return lastErr
}

// Run 读取 netclientDir 下的配置,结合服务状态和备份可用性做出决策并执行。
// 返回 error 时(ActionAlertNoBackup)调用方应以非零退出码结束,让计划任务运行历史能反映"修复失败"。
func Run(netclientDir, backupDir string, svc ServiceController) (Result, error) {
	load, err := guardconfig.Load(netclientDir)
	if err != nil {
		return Result{}, err
	}

	goodNC := filepath.Join(backupDir, "netclient.json.good")
	goodSrv := filepath.Join(backupDir, "servers.json.good")
	backupAvailable := fileExists(goodNC) && fileExists(goodSrv)

	running, err := svc.IsRunning()
	if err != nil {
		return Result{}, fmt.Errorf("query service status: %w", err)
	}

	action := Decide(load, running, backupAvailable)

	switch action {
	case ActionNone:
		return Result{Action: action, Detail: "config consistent, service running"}, nil

	case ActionStartService:
		if err := startAndVerifyHealthy(svc, defaultHealthCheckAttempts, defaultHealthCheckTimeout, defaultHealthCheckInterval); err != nil {
			return Result{Action: action}, fmt.Errorf("start service: %w", err)
		}
		return Result{Action: action, Detail: "service was not running, started it and verified broker connectivity"}, nil

	case ActionRestoreAndStart:
		// 恢复分两步复制(netclient.json 再 servers.json),中间失败会让 live 配置停在
		// "一半已恢复、一半仍是旧内容"的新状态,而不是原样保留恢复前的状态。这里不做两阶段
		// 提交:恢复前 live 配置本就是不一致的(否则不会走到这个分支),所以复制中途失败得到的
		// 仍是一个不一致状态,并不比恢复前更差;此时函数会直接返回 error、不再调用 Start,
		// 服务保持停止,下一次(5 分钟后)watch 运行会用同一份备份重试,预期可自愈。
		// 与 backup 包不同的是:那里写坏的是最后一份"已知良好"备份本身,一旦写坏就没有回退
		// 手段,所以必须保证两个文件都成功后才 rename;这里写坏的是本就已损坏的 live 配置,
		// 备份文件本身没有被触碰,重试成本很低,因此沿用计划给出的实现,不额外做两阶段提交。
		if err := svc.Stop(); err != nil {
			return Result{Action: action}, fmt.Errorf("stop service: %w", err)
		}
		if err := copyOverwrite(goodNC, filepath.Join(netclientDir, "netclient.json")); err != nil {
			return Result{Action: action}, fmt.Errorf("restore netclient.json: %w", err)
		}
		if err := copyOverwrite(goodSrv, filepath.Join(netclientDir, "servers.json")); err != nil {
			return Result{Action: action}, fmt.Errorf("restore servers.json: %w", err)
		}
		if err := startAndVerifyHealthy(svc, defaultHealthCheckAttempts, defaultHealthCheckTimeout, defaultHealthCheckInterval); err != nil {
			return Result{Action: action}, fmt.Errorf("start service after restore: %w", err)
		}
		return Result{Action: action, Detail: "restored from known-good backup, restarted service, and verified broker connectivity"}, nil

	case ActionAlertNoBackup:
		return Result{Action: action}, fmt.Errorf("config invalid and no known-good backup available, manual intervention required")

	default:
		return Result{}, fmt.Errorf("unknown action %v", action)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyOverwrite(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
