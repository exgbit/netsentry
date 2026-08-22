// Package watch 负责检测 netclient 配置是否损坏、服务是否在运行,并在需要时自动修复。
package watch

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"netsentry/internal/guardconfig"
)

// Action 描述 watch 决策后应该采取的动作。
type Action int

const (
	ActionNone Action = iota
	ActionStartService
	ActionRestoreAndStart
	ActionAlertNoBackup
	// ActionRestartStuckService 对应"服务本身没死(sc query 显示 Running),但持续
	// 连不上 broker"这种状态——真机诊断包发现过的一个盲区:之前只在"服务从停止
	// 状态启动"这个时机才会检查 broker 连通性,一个本来连着、后来断线卡死重连
	// 循环的服务会一直被判定为"健康",永远不会触发自愈。
	ActionRestartStuckService
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
	case ActionRestartStuckService:
		return "restart-stuck-service"
	default:
		return "unknown"
	}
}

// Decide 根据配置一致性、服务是否在运行、是否存在可用的已知良好备份、以及服务在跑的情况下
// 是不是卡在跟 broker 断线重连,决定要采取的动作。brokerStuck 只在 serviceRunning 为 true
// 时才有意义(服务都没跑,谈不上"卡在重连"),调用方对此负责,这里不做防御性检查。
func Decide(load guardconfig.LoadResult, serviceRunning, backupAvailable, brokerStuck bool) Action {
	if !load.Consistent {
		if backupAvailable {
			return ActionRestoreAndStart
		}
		return ActionAlertNoBackup
	}
	if !serviceRunning {
		return ActionStartService
	}
	if brokerStuck {
		return ActionRestartStuckService
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

// TunnelChecker 抽象"数据隧道本身是否可用"的检查(实际实现是 ping 一个配置好的内网目标),
// 方便测试用假实现替代真的网络请求。
//
// 真机事故复盘过:netclient 连不上 broker(MQTT 控制通道,用来推送配置/节点更新)不代表
// WireGuard 数据隧道也跟着不能用——broker 卡死重连的时候实测过 ping 内网目标一直是通的。
// 貌似"卡死"其实只影响控制通道,重启服务对它没用(真机上 3 次重试都没能恢复,同时还让
// 本来正常工作的隧道跟着抖动了好几分钟)。所以"卡死"不能单独作为重启的理由,必须先确认
// 隧道本身也确实不可用,再动手修——这时候重启才是净收益为正的操作。
type TunnelChecker interface {
	TunnelReachable() bool
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

// brokerFailSignal 是 netclient 卡在重连 broker 时反复打印的日志,真机诊断包里两种形式都见过:
// 未结构化的 `unable to connect to broker, retrying ...` 和结构化的
// `{"msg":"unable to connect to broker",...}`,这个子串两种都能匹配到。
const brokerFailSignal = "unable to connect to broker"

// IsBrokerConnected 判断一段日志内容里是否包含 netclient 成功连上 broker 的信号。
func IsBrokerConnected(logTail []byte) bool {
	return bytes.Contains(logTail, []byte(brokerConnectedSignal))
}

// IsBrokerStuck 判断日志尾部里,"连接失败"信号是不是比"连接成功"信号更晚出现——也就是
// "这段日志里最后一次跟 broker 相关的事件是失败,还没等到后续的成功恢复"。不要求"从来没
// 连上过":哪怕之前连过、后来断线卡进了重连死循环,只要失败在后,就算卡死。这段日志里压根
// 没出现过失败信号时,不算卡死(可能这段时间根本没发生过重连尝试,不代表有问题)。
func IsBrokerStuck(logTail []byte) bool {
	lastFail := bytes.LastIndex(logTail, []byte(brokerFailSignal))
	if lastFail == -1 {
		return false
	}
	lastOK := bytes.LastIndex(logTail, []byte(brokerConnectedSignal))
	return lastFail > lastOK
}

const (
	defaultHealthCheckAttempts = 3
	defaultHealthCheckTimeout  = 60 * time.Second
	defaultHealthCheckInterval = 3 * time.Second

	// stuckCheckTailBytes 是判断"服务是否卡在重连 broker"时,从 winsw.out.log 尾部读取的字节数。
	// 只看最近这一段、不读全量日志——这个日志是追加写入、会随时间无限增长(netclient 自己的服务
	// 配置显式设了 <log mode="append" />),watch 每 5 分钟跑一次、可能连续跑几个月,每次都读全量
	// 文件代价会越来越大,而我们只关心"最近有没有卡住",不需要历史。16KB 按真机日志里每轮重连
	// 大约 300~400 字节估算,能覆盖大约 20~30 分钟的重连历史,watch 5 分钟一轮绰绰有余。
	stuckCheckTailBytes = 16 * 1024
)

// startAndVerifyHealthy 假设服务当前已经是停止状态,启动它并轮询日志确认 broker 连接没有出问题;
// 如果在 checkTimeout 内确认到连接失败,会重新 Stop+Start 再试一次,最多尝试 maxAttempts 次。
// 这是为了绕开 netclient 一个已知的上游 bug:重启后有时会卡在反复重连 broker、最终自己退出,
// 而全新进程(即再 Stop+Start 一次)通常能跳出这个卡死状态——这是从真实故障里观察到的行为,
// 不是理论推测。
//
// 判定规则(真机踩坑后修订):
//   - 轮询期间看到成功信号(brokerConnectedSignal)→ 立即成功。
//   - 超时且日志尾部没有"失败在成功之后"的证据(!IsBrokerStuck)→ 也算成功。
//     不能把"没等到成功信号"直接当失败:成功信号是 slog.Info 打的,而 netclient
//     v1.6.0 join 出来的主机默认 verbosity 0,Info 级日志整个被过滤——真机上验证过
//     "broker 实际连上了(mosquitto 服务端能看到该客户端),winsw.out.log 里却永远
//     不会出现这行"。失败信号(unable to connect to broker)是 ERROR 级,任何
//     verbosity 下都会打,所以"没有失败证据"是可靠的放行依据。
//   - 超时且日志尾部有失败证据 → 这一轮判失败,Stop+Start 重试。
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
		var tail []byte
		for {
			var readErr error
			tail, readErr = svc.ReadLogFrom(offset)
			if readErr == nil && IsBrokerConnected(tail) {
				return nil
			}
			if !time.Now().Before(deadline) {
				break
			}
			time.Sleep(pollInterval)
		}
		if !IsBrokerStuck(tail) {
			// 超时但没有任何失败证据:默认 verbosity 下成功信号本来就不会出现,
			// 服务在跑、也没在报连接失败,按健康放行。
			return nil
		}
		lastErr = fmt.Errorf("attempt %d/%d: service started but kept failing to connect to broker within %s (known upstream netclient MQTT reconnect issue)", attempt, maxAttempts, checkTimeout)
	}
	return lastErr
}

// Run 读取 netclientDir 下的配置,结合服务状态、备份可用性、以及服务在跑的情况下是否卡在
// 重连 broker(且隧道本身确实也不可用),做出决策并执行。返回 error 时(ActionAlertNoBackup)
// 调用方应以非零退出码结束,让计划任务运行历史能反映"修复失败"。
func Run(netclientDir, backupDir string, svc ServiceController, tunnel TunnelChecker) (Result, error) {
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

	// 只有配置一致、服务又确实在跑的时候,"卡在重连 broker"这个问题才有意义去查——
	// 配置不一致或服务没跑,已经有优先级更高的动作要做,不需要这个信号,也没必要为了
	// 拿这个信号去读一次日志文件。读日志失败(比如文件暂时被占用)不当成 watch 本身的
	// 失败,按"没有卡死信号"处理——好过因为一次偶发的读取失败就贸然重启一个其实可能
	// 正常的服务,下一轮(5 分钟后)会重新判断。
	brokerStuck := false
	if load.Consistent && running {
		size, sizeErr := svc.LogSize()
		if sizeErr == nil {
			offset := size - stuckCheckTailBytes
			if offset < 0 {
				offset = 0
			}
			if tail, readErr := svc.ReadLogFrom(offset); readErr == nil {
				brokerStuck = IsBrokerStuck(tail)
			}
		}
	}

	// 日志显示"卡死"只是个初步嫌疑,真正值不值得为此重启服务,还要看隧道本身是不是
	// 也确实不通——只有 brokerStuck 为 true 时才会调用 TunnelReachable(短路求值,
	// 健康状态下不会多打一次 ping),隧道能通就说明这只是控制通道的问题,不去碰它。
	if brokerStuck && tunnel.TunnelReachable() {
		brokerStuck = false
	}

	action := Decide(load, running, backupAvailable, brokerStuck)

	switch action {
	case ActionNone:
		return Result{Action: action, Detail: "config consistent, service running"}, nil

	case ActionStartService:
		if err := startAndVerifyHealthy(svc, defaultHealthCheckAttempts, defaultHealthCheckTimeout, defaultHealthCheckInterval); err != nil {
			// 注意:走到这里说明 startAndVerifyHealthy 用完所有重试仍未确认连上 broker,但服务本身
			// 处于 Started 状态,这里不会主动再 Stop 它——万一它其实是好的,没必要画蛇添足;万一还是
			// 坏的,下一次(5 分钟后)watch 运行会重新判断。
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
			// 注意:同上——用完所有重试仍未确认连上 broker,但服务处于 Started 状态,这里不会主动
			// 再 Stop 它;是否需要人工介入交给下一次 watch 运行重新判断。
			return Result{Action: action}, fmt.Errorf("start service after restore: %w", err)
		}
		return Result{Action: action, Detail: "restored from known-good backup, restarted service, and verified broker connectivity"}, nil

	case ActionRestartStuckService:
		// 走到这里说明日志显示卡死、而且隧道本身也确认 ping 不通了(见上面
		// TunnelChecker 的门槛检查),不是那种"broker 卡但隧道其实正常"的情况——
		// 这时候才值得为了修 broker 去动服务。服务本身没死(sc query 显示
		// Running),配置也一致,Stop 是安全的,不存在"半恢复"之类的中间状态
		// 需要担心(不涉及配置文件改动),直接复用 startAndVerifyHealthy 的
		// Stop+Start+验证连通性逻辑。
		if err := svc.Stop(); err != nil {
			return Result{Action: action}, fmt.Errorf("stop stuck service: %w", err)
		}
		if err := startAndVerifyHealthy(svc, defaultHealthCheckAttempts, defaultHealthCheckTimeout, defaultHealthCheckInterval); err != nil {
			return Result{Action: action}, fmt.Errorf("restart stuck service: %w", err)
		}
		return Result{Action: action, Detail: "service was running but stuck reconnecting to broker (tunnel also unreachable), restarted and verified connectivity"}, nil

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
