// v2.2.1 增加断点续扫功能 支持进度条显示
// 自动获取 eth0 网卡的 MAC 地址
// 在以下情况下尝试自动获取 MAC 地址：
// 配置文件不存在时
// 配置文件中的 MAC 地址为空时
// 命令行参数未指定 MAC 地址时
// 获取失败时给出相应的错误提示
// 合并组件zmap和masscan，根据操作系统自动选择扫描器
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ollama_scanner/logger"
	"github.com/ollama_scanner/config"
)

// 在 const 声明之前添加配置结构体
type Config struct {
	Port         int    `json:"port"`
	GatewayMAC   string `json:"gateway_mac"`
	InputFile    string `json:"input_file"`
	OutputFile   string `json:"output_file"`
	ZmapThreads  int    `json:"zmap_threads"`
	MasscanRate  int    `json:"masscan_rate"`
	DisableBench bool   `json:"disable_bench"`
	BenchPrompt  string `json:"bench_prompt"`
	LogPath      string `json:"log_path"`
	EnableLog    bool   `json:"enable_log"`
	TimeZone     string `json:"timezone"`
}

const (
	defaultPort        = 11434 // 修改为 defaultPort
	timeout            = 3 * time.Second
	maxWorkers         = 200
	maxIdleConns       = 100
	idleConnTimeout    = 90 * time.Second
	benchTimeout       = 30 * time.Second
	defaultCSVFile     = "results.csv"
	defaultZmapThreads = 10   // zmap 默认线程数
	defaultMasscanRate = 1000 // masscan 默认扫描速率
	defaultBenchPrompt = "为什么太阳会发光？用一句话回答"
)

var (
	port        = defaultPort // 将 port 改为变量
	httpClient  *http.Client
	csvWriter   *csv.Writer
	csvFile     *os.File
	resultsChan chan ScanResult
	allResults  []ScanResult
	mu          sync.Mutex
	scannerType string // 扫描器类型 (zmap/masscan)
	config      Config
	// 移除这里的初始化，只声明变量
	gatewayMAC   *string
	inputFile    *string
	outputFile   *string
	disableBench *bool
	benchPrompt  *string
	zmapThreads  *int
	masscanRate  *int
	enableLog    = flag.Bool("enable-log", true, "启用日志记录")
	logPath      = flag.String("log-path", "logs/scan.log", "日志文件路径")
)

// 选择合适的扫描器并初始化
func initScanner() error {
	osName := runtime.GOOS
	if osName == "windows" {
		scannerType = "masscan"
		log.Printf("Windows 系统，使用 masscan 扫描器")
	} else {
		scannerType = "zmap"
		log.Printf("Unix/Linux 系统，使用 zmap 扫描器")
	}

	return checkAndInstallScanner()
}

// 检查并安装扫描器
func checkAndInstallScanner() error {
	if scannerType == "masscan" {
		return checkAndInstallMasscan()
	}
	return checkAndInstallZmap()
}

// 添加 masscan 安装函数
func checkAndInstallMasscan() error {
	_, err := exec.LookPath("masscan")
	if err == nil {
		log.Println("masscan 已安装")
		return nil
	}

	log.Println("masscan 未安装, 尝试自动安装...")
	osName := runtime.GOOS

	switch osName {
	case "linux":
		// 尝试使用 apt
		if err := exec.Command("apt", "-v").Run(); err == nil {
			cmd := exec.Command("sudo", "apt-get", "update")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("apt-get update 失败: %w", err)
			}
			cmd = exec.Command("sudo", "apt-get", "install", "-y", "masscan")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("安装 masscan 失败: %w", err)
			}
		} else {
			// 尝试使用 yum
			if err := exec.Command("yum", "-v").Run(); err == nil {
				cmd := exec.Command("sudo", "yum", "install", "-y", "masscan")
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("安装 masscan 失败: %w", err)
				}
			} else {
				return fmt.Errorf("未找到包管理器")
			}
		}
	default:
		return fmt.Errorf("不支持在 %s 系统上自动安装 masscan", osName)
	}

	log.Println("masscan 安装完成")
	return nil
}

// 修改 loadConfig 函数
func loadConfig() error {
	scriptDir, err := getScriptDir()
	if err != nil {
		log.Printf("获取脚本目录失败: %v, 使用当前目录", err)
		scriptDir = "."
	}

	// 尝试读取配置文件
	configPath := filepath.Join(scriptDir, ".env")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 如果配置文件不存在,尝试获取 eth0 的 MAC 地址
			mac, err := getEth0MAC()
			if err != nil {
				log.Printf("自动获取MAC地址失败: %v", err)
			}

			// 创建默认配置，使用相对于脚本目录的路径
			config = Config{
				Port:         port,
				GatewayMAC:   mac,
				InputFile:    filepath.Join(scriptDir, "ip.txt"),
				OutputFile:   filepath.Join(scriptDir, defaultCSVFile),
				ZmapThreads:  defaultZmapThreads,
				MasscanRate:  defaultMasscanRate,
				DisableBench: false,
				BenchPrompt:  defaultBenchPrompt,
				LogPath:      "logs/scan.log",
				EnableLog:    true,
				TimeZone:     "Local",
			}
			// 保存默认配置
			return saveConfig()
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析配置文件
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 处理配置文件中的路径
	// 如果配置文件中的路径是相对路径，则相对于配置文件所在目录处理
	if (!filepath.IsAbs(config.InputFile)) {
		config.InputFile = filepath.Join(scriptDir, config.InputFile)
	}
	if (!filepath.IsAbs(config.OutputFile)) {
		config.OutputFile = filepath.Join(scriptDir, config.OutputFile)
	}

	// 如果配置中的 GatewayMAC 为空,尝试获取 eth0 的 MAC 地址
	if (config.GatewayMAC == "") {
		mac, err := getEth0MAC()
		if (err != nil) {
			log.Printf("自动获取MAC地址失败: %v", err)
		} else {
			config.GatewayMAC = mac
			if (err := saveConfig(); err != nil) {
				log.Printf("保存更新后的配置失败: %v", err)
			}
		}
	}

	// 使用配置更新相关变量
	port = config.Port
	*gatewayMAC = config.GatewayMAC
	*inputFile = config.InputFile
	*outputFile = config.OutputFile
	*zmapThreads = config.ZmapThreads
	*masscanRate = config.MasscanRate
	*disableBench = config.DisableBench
	*benchPrompt = config.BenchPrompt

	return nil
}

func saveConfig() error {
	// 更新配置对象
	config.Port = port
	config.GatewayMAC = *gatewayMAC
	config.InputFile = *inputFile
	config.OutputFile = *outputFile
	config.ZmapThreads = *zmapThreads
	config.MasscanRate = *masscanRate
	config.DisableBench = *disableBench
	config.BenchPrompt = *benchPrompt

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	scriptDir, err := getScriptDir()
	if err != nil {
		return fmt.Errorf("获取脚本目录失败: %w", err)
	}

	configPath := filepath.Join(scriptDir, ".env")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("保存配置文件失败: %w", err)
	}

	return nil
}

type ScanResult struct {
	IP     string
	Models []ModelInfo
}

type ModelInfo struct {
	Name            string
	FirstTokenDelay time.Duration
	TokensPerSec    float64
	Status          string
}

func init() {
    // 加载并验证环境变量
    if err := config.LoadEnv(); err != nil {
        log.Fatalf("加载环境变量失败: %v", err)
    }
    
    if err := config.ValidateEnv(); err != nil {
        log.Fatalf("环境变量验证失败: %v", err)
    }

    // 使用环境变量更新配置
    port = config.GetEnvAsInt("PORT", defaultPort)
    *gatewayMAC = os.Getenv("GATEWAY_MAC")
    *inputFile = os.Getenv("INPUT_FILE")
    *outputFile = os.Getenv("OUTPUT_FILE")
    *zmapThreads = config.GetEnvAsInt("ZMAP_THREADS", defaultZmapThreads)
    *masscanRate = config.GetEnvAsInt("MASSCAN_RATE", defaultMasscanRate)
    *disableBench = config.GetEnvAsBool("DISABLE_BENCH", false)
    *benchPrompt = os.Getenv("BENCH_PROMPT")
    
    // ...rest of existing init code...

	// 初始化命令行参数的默认值
	gatewayMAC = flag.String("gateway-mac", "", "指定网关MAC地址(格式:aa:bb:cc:dd:ee:ff)")
	inputFile = flag.String("input", "ip.txt", "输入文件路径(CIDR格式列表)")
	outputFile = flag.String("output", defaultCSVFile, "CSV输出文件路径")
	disableBench = flag.Bool("no-bench", false, "禁用性能基准测试")
	benchPrompt = flag.String("prompt", defaultBenchPrompt, "性能测试提示词")
	zmapThreads = flag.Int("T", defaultZmapThreads, "zmap 线程数 (默认为 10)")
	masscanRate = flag.Int("rate", defaultMasscanRate, "masscan 扫描速率 (每秒扫描的包数)")

	flag.Usage = func() {
		helpText := fmt.Sprintf(`Ollama节点扫描工具 v2.2.1 https://t.me/+YfCVhGWyKxoyMDhl
默认功能:
- 自动执行性能测试
- 结果导出到%s
- Windows系统使用masscan，其他系统使用zmap

使用方法:
%s [参数]

参数说明:
`, defaultCSVFile, os.Args[0])

		fmt.Fprintf(os.Stderr, helpText)
		flag.PrintDefaults()

		examples := fmt.Sprintf(`
基础使用示例:
  %[1]s -gateway-mac aa:bb:cc:dd:ee:ff
  %[1]s -gateway-mac aa:bb:cc:dd:ee:ff -no-bench -output custom.csv
  
Zmap参数 (Unix/Linux):
  %[1]s -gateway-mac aa:bb:cc:dd:ee:ff -T 20

Masscan参数 (Windows):
  %[1]s -gateway-mac aa:bb:cc:dd:ee:ff -rate 2000
`, os.Args[0])

		fmt.Fprintf(os.Stderr, examples)
	}

	// 加载配置文件
	if err := loadConfig(); err != nil {
		log.Printf("加载配置失败: %v, 使用默认配置", err)
	}

	// 初始化时区配置
	if err := config.InitTimeZone(config.TimeZone); err != nil {
		log.Printf("时区初始化警告: %v", err)
	}

	// 修改使用时区的地方
	logPath := *logPath
	if logPath == "logs/scan.log" {
		taskName, err := logger.GetNextTaskName()
		if err != nil {
			log.Printf("获取任务名称失败: %v, 使用默认名称", err)
			taskName = "模型扫描任务1"
		}

		// 使用配置的时区格式化时间
		timestamp := config.FormatTime(config.Now(), "20060102_1504")
		logFileName := fmt.Sprintf("%s_%s.log", taskName, timestamp)
		// ...rest of existing logging code...
	}

	// 初始化日志系统
	if *enableLog {
		logPath := *logPath
		if logPath == "logs/scan.log" { // 如果使用默认路径
			// 获取任务名称
			taskName, err := logger.GetNextTaskName()
			if err != nil {
				log.Printf("获取任务名称失败: %v, 使用默认名称", err)
				taskName = "模型扫描任务1"
			}

			// 生成日志文件名
			timestamp := config.FormatTime(config.Now(), "20060102_1504")
			logFileName := fmt.Sprintf("%s_%s.log", taskName, timestamp)

			// 获取执行目录
			execDir, err := getExecutableDir()
			if err != nil {
				log.Printf("获取执行目录失败: %v, 使用当前目录", err)
				execDir = "."
			}

			// 组合完整的日志路径
			logPath = filepath.Join(execDir, "logs", logFileName)
		}

		// 确保日志目录存在
		logDir := filepath.Dir(logPath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			log.Printf("创建日志目录失败: %v", err)
		}

		if err := logger.Init(logPath); err != nil {
			log.Printf("初始化日志系统失败: %v", err)
		}
		log.Printf("日志文件路径: %s", logPath)
		defer logger.Close()
	}

	httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        maxIdleConns,
			MaxIdleConnsPerHost: maxIdleConns,
			IdleConnTimeout:     idleConnTimeout,
		},
		Timeout: timeout,
	}
	resultsChan = make(chan ScanResult, 100)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

type Progress struct {
	mu        sync.Mutex
	total     int
	current   int
	startTime time.Time
}

// 添加获取MAC地址的函数
func getEth0MAC() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("获取网络接口失败: %w", err)
	}

	for _, iface := range ifaces {
		// 查找 eth0 接口
		if iface.Name == "eth0" {
			mac := iface.HardwareAddr.String()
			if mac != "" {
				return mac, nil
			}
		}
	}
	return "", fmt.Errorf("未找到 eth0 网卡或获取MAC地址失败")
}

func (p *Progress) Init(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
	p.current = 0
	p.startTime = config.Now()
}

func (p *Progress) Increment() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current++
	p.printProgress()
}

func (p *Progress) printProgress() {
	percentage := float64(p.current) / float64(p.total) * 100
	elapsed := time.Since(p.startTime)
	remainingTime := time.Duration(0)
	if p.current > 0 {
		remainingTime = time.Duration(float64(elapsed) / float64(p.current) * float64(p.total-p.current))
	}
	fmt.Printf("\r当前进度: %.1f%% (%d/%d) 已用时: %v 预计剩余: %v",
		percentage, p.current, p.total, elapsed.Round(time.Second), remainingTime.Round(time.Second))
}

// 增加断点续扫功能
const (
	// ...existing code...
	stateFile = "scan_state.json" // 状态文件名
)

var (
	// ...existing code...
	resumeScan = flag.Bool("resume", false, "从上次中断处继续扫描")
)

// ScanState 结构体用于保存扫描状态
type ScanState struct {
	ScannedIPs   map[string]bool `json:"scanned_ips"`
	LastScanTime time.Time       `json:"last_scan_time"`
	TotalIPs     int             `json:"total_ips"`
	Config       ScanConfig      `json:"config"`
}

type ScanConfig struct {
	GatewayMAC   string `json:"gateway_mac"`
	InputFile    string `json:"input_file"`
	OutputFile   string `json:"output_file"`
	DisableBench bool   `json:"disable_bench"`
}

// saveState 函数用于保存扫描状态到文件中
func saveState(state *ScanState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		return fmt.Errorf("保存状态文件失败: %w", err)
	}

	return nil
}

func loadState() (*ScanState, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}

	var state ScanState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("解析状态文件失败: %w", err)
	}

	return &state, nil
}

func validateStateConfig(state *ScanState) bool {
	return state.Config.GatewayMAC == *gatewayMAC &&
		state.Config.InputFile == *inputFile &&
		state.Config.OutputFile == *outputFile &&
		state.Config.DisableBench == *disableBench
}

// main 函数是程序的入口点,负责初始化程序、检查并安装 zmap、设置信号处理和启动扫描过程.
func main() {
	// 解析命令行参数
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化扫描器
	if err := initScanner(); err != nil {
		log.Printf("❌ 初始化扫描器失败: %v\n", err)
		fmt.Printf("是否继续执行程序？(y/n): ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" {
			os.Exit(1)
		}
	}

	// 初始化 CSV 写入器,用于将扫描结果保存到文件中
	initCSVWriter()
	// 确保在函数退出时关闭 CSV 文件
	defer csvFile.Close()
	// 设置信号处理,以便在收到终止信号时清理资源并退出程序
	setupSignalHandler(cancel)
	// 启动扫描过程,如果扫描失败则打印错误信息
	if err := runScanProcess(ctx); err != nil {
		fmt.Printf("❌ 扫描失败: %v\n", err)
	}

	if *enableLog {
		defer logger.Close()
	}
}

// checkAndInstallZmap 检查系统中是否安装了 zmap,如果未安装则尝试自动安装.
// 支持的操作系统包括 Linux(Debian/Ubuntu 使用 apt,CentOS/RHEL 使用 yum)和 macOS(使用 brew).
// 如果不支持当前操作系统或安装过程中出现错误,将返回相应的错误信息.
func checkAndInstallZmap() error {
	// 检查 zmap 是否已经安装
	_, err := exec.LookPath("zmap")
	if err == nil {
		// zmap 已安装
		log.Println("zmap 已安装")
		return nil
	}

	// zmap 未安装,尝试自动安装
	log.Println("zmap 未安装, 尝试自动安装...")
	var cmd *exec.Cmd
	var installErr error
	// 获取当前操作系统名称
	osName := runtime.GOOS
	log.Printf("Operating System: %s\n", osName)

	// 打印当前环境变量,方便调试
	log.Println("当前环境变量:")
	for _, env := range os.Environ() {
		log.Println(env)
	}

	// 根据不同的操作系统选择不同的安装方式
	switch osName {
	case "linux":
		// 在 Linux 系统上,尝试使用 apt(Debian/Ubuntu)或 yum(CentOS/RHEL)安装 zmap
		// 首先尝试使用 apt
		err = exec.Command("apt", "-v").Run()
		if err == nil {
			// 使用 sudo -u root 明确指定用户身份执行 apt-get update
			cmd = exec.Command("sudo", "-u", "root", "/usr/bin/apt-get", "update")
			installErr = cmd.Run()
			if installErr != nil {
				log.Printf("apt-get update failed: %v\n", installErr)
				return fmt.Errorf("apt-get update failed: %w", installErr)
			}

			// 使用 sudo -u root 明确指定用户身份执行 apt-get install zmap
			cmd = exec.Command("sudo", "-u", "root", "/usr/bin/apt-get", "install", "-y", "zmap")
			installErr = cmd.Run()
			if installErr != nil {
				log.Printf("apt-get install zmap failed: %v\n", installErr)
				return fmt.Errorf("apt-get install zmap failed: %w", installErr)
			}

		} else {
			// 如果 apt 不可用,尝试使用 yum
			err = exec.Command("yum", "-v").Run()
			if err == nil {
				// 使用 sudo -u root 明确指定用户身份执行 yum install zmap
				cmd = exec.Command("sudo", "-u", "root", "/usr/bin/yum", "install", "-y", "zmap")
				installErr = cmd.Run()
				if installErr != nil {
					log.Printf("yum install zmap failed: %v\n", installErr)
					return fmt.Errorf("yum install zmap failed: %w", installErr)
				}

			} else {
				return fmt.Errorf("apt and yum not found, cannot install zmap automatically. Please install manually")
			}
		}
	case "darwin":
		// 在 macOS 系统上,使用 brew 安装 zmap
		_, brewErr := exec.LookPath("brew")
		if brewErr != nil {
			return fmt.Errorf("未安装 brew，无法自动安装 zmap。请手动安装")
		}

		cmd = exec.Command("brew", "install", "zmap")
		installErr = cmd.Run()
		if installErr != nil {
			return fmt.Errorf("使用 brew 安装 zmap 失败: %w", installErr)
		}
	default:
		return fmt.Errorf("不支持的操作系统: %s，无法自动安装 zmap。请手动安装", osName)
	}

	log.Println("zmap 安装完成")
	return nil
}

// initCSVWriter 函数用于初始化 CSV 写入器,创建 CSV 文件并写入表头.
func initCSVWriter() {
	var err error
	// 创建一个新的 CSV 文件,文件路径由命令行参数 -output 指定
	csvFile, err = os.Create(*outputFile)
	if err != nil {
		// 如果创建文件失败,打印错误信息
		fmt.Printf("⚠️ 创建CSV文件失败: %v\n", err)
		return
	}

	// 创建一个新的 CSV 写入器,用于将数据写入 CSV 文件
	csvWriter = csv.NewWriter(csvFile)
	// 定义 CSV 文件的表头
	headers := []string{"IP地址", "模型名称", "状态"}
	// 如果未禁用性能基准测试,则在表头中添加额外的列
	if !*disableBench {
		// 添加首Token延迟和Tokens/s列
		headers = append(headers, "首Token延迟(ms)", "Tokens/s")
	}
	// 将表头写入 CSV 文件
	csvWriter.Write(headers)
}

func setupSignalHandler(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		fmt.Println("\n⚠️ 收到终止信号，正在保存进度...")
		if csvWriter != nil {
			csvWriter.Flush()
		}
		// 保存配置
		if err := saveConfig(); err != nil {
			log.Printf("保存配置失败: %v", err)
		}
		os.Exit(1)
	}()
}

func runScanProcess(ctx context.Context) error {
	if err := validateInput(); err != nil {
		return err
	}

	fmt.Printf("🔍 开始扫描目标，使用网关MAC: %s\n", *gatewayMAC)
	if err := execScan(); err != nil {
		return err
	}

	return processResults(ctx)
}

// ...existing code...

func validateInput() error {
	// 如果命令行参数中未指定 MAC 地址,尝试获取 eth0 的 MAC 地址
	if *gatewayMAC == "" {
		mac, err := getEth0MAC()
		if err != nil {
			return fmt.Errorf("必须指定网关MAC地址,自动获取失败: %v", err)
		}
		*gatewayMAC = mac
		log.Printf("自动使用 eth0 网卡 MAC 地址: %s", mac)
	}

	// 获取脚本所在目录
	scriptDir, err := getScriptDir()
	if err != nil {
		return fmt.Errorf("获取脚本目录失败: %v", err)
	}

	// 优先使用命令行参数中的路径
	// 如果命令行参数是相对路径且配置文件中有绝对路径，则使用配置文件中的路径
	if (!filepath.IsAbs(*inputFile)) {
		if filepath.IsAbs(config.InputFile) {
			*inputFile = config.InputFile
		} else {
			*inputFile = filepath.Join(scriptDir, *inputFile)
		}
	}
	log.Printf("使用输入文件: %s", *inputFile)

	if (!filepath.IsAbs(*outputFile)) {
		if filepath.IsAbs(config.OutputFile) {
			*outputFile = config.OutputFile
		} else {
			*outputFile = filepath.Join(scriptDir, *outputFile)
		}
	}
	log.Printf("使用输出文件: %s", *outputFile)

	// 检查输入文件是否存在
	if _, err := os.Stat(*inputFile); os.IsNotExist(err) {
		// 如果文件不存在，创建一个空文件
		emptyFile, err := os.Create(*inputFile)
		if err != nil {
			return fmt.Errorf("创建输入文件失败: %v", err)
		}
		emptyFile.Close()
		log.Printf("创建了空的输入文件: %s", *inputFile)
		return fmt.Errorf("请在输入文件中添加要扫描的IP地址: %s", *inputFile)
	}

	return nil
}

// 获取脚本所在目录的新函数
func getScriptDir() (string, error) {
	// 尝试使用 os.Executable() 获取可执行文件路径
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取可执行文件路径失败: %v", err)
	}

	// 获取可执行文件的实际路径（处理符号链接）
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("解析符号链接失败: %v", err)
	}

	// 获取目录路径
	dir := filepath.Dir(realPath)

	// 验证目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("脚本目录不存在: %v", err)
	}

	return dir, nil
}

func execScan() error {
	if scannerType == "masscan" {
		return execMasscan()
	}
	return execZmap()
}

func execMasscan() error {
	cmd := exec.Command("masscan",
		"-p", fmt.Sprintf("%d", port),
		"--rate", fmt.Sprintf("%d", *masscanRate),
		"--interface", "eth0",
		"--source-ip", *gatewayMAC,
		"-iL", *inputFile,
		"-oL", *outputFile)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func execZmap() error {
	threads := *zmapThreads // 获取 zmap 线程数

	cmd := exec.Command("zmap",
		"-p", fmt.Sprintf("%d", port),
		"-G", *gatewayMAC,
		"-w", *inputFile,
		"-o", *outputFile,
		"-T", fmt.Sprintf("%d", threads)) // 设置 zmap 线程数
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// v2.2.1支持断点续扫功能
func processResults(ctx context.Context) error {
	file, err := os.Open(*outputFile)
	if err != nil {
		return fmt.Errorf("打开结果文件失败: %w", err)
	}
	defer file.Close()
	var rhWg sync.WaitGroup // 添加 rhWg 声明
	// 加载之前的扫描状态
	var state *ScanState
	if *resumeScan {
		state, err = loadState()
		if err != nil {
			return fmt.Errorf("加载扫描状态失败: %w", err)
		}
		if state != nil && !validateStateConfig(state) {
			return fmt.Errorf("扫描配置已更改,无法继续之前的扫描")
		}
	}

	if state == nil {
		state = &ScanState{
			ScannedIPs: make(map[string]bool),
			Config: ScanConfig{
				GatewayMAC:   *gatewayMAC,
				InputFile:    *inputFile,
				OutputFile:   *outputFile,
				DisableBench: *disableBench,
			},
		}
	}

	// 计算总IP数并更新进度
	scanner := bufio.NewScanner(file)
	if state.TotalIPs == 0 {
		for scanner.Scan() {
			if net.ParseIP(strings.TrimSpace(scanner.Text())) != nil {
				state.TotalIPs++
			}
		}
		file.Seek(0, 0)
	}

	progress := &Progress{}
	progress.Init(state.TotalIPs)
	progress.current = len(state.ScannedIPs)

	ips := make(chan string, maxWorkers*2)
	var wg sync.WaitGroup

	// 定期保存扫描状态
	stopSaving := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 *时间.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state.LastScanTime = config.Now()
				if err := saveState(state); err != nil {
					log.Printf("保存扫描状态失败: %v", err)
				}
			case <-stopSaving:
				return
			}
		}
	}()

	// 修改 worker 函数以支持断点续扫
	workerWithProgress := func(ctx context.Context, wg *sync.WaitGroup, ips <-chan string, state *ScanState, progress *Progress) {
		defer wg.Done()
		for ip := range ips {
			select {
			case <-ctx.Done():
				return
			default:
				if state.ScannedIPs[ip] {
					progress.Increment()
					continue
				}

				if checkPort(ip) && checkOllama(ip) {
					logger.LogScanResult(ip, []string{"检测中"}, "端口开放")
					result := ScanResult{IP: ip}
					if models := getModels(ip); len(models) > 0 {
						models = sortModels(models)
						logger.LogScanResult(ip, models, "发现模型")
						for _, model := range models {
							info := ModelInfo{Name: model}
							if !*disableBench {
								latency, tps, status := benchmarkModel(ip, model)
								info.FirstTokenDelay = latency
								info.TokensPerSec = tps
								info.Status = status
								logger.LogBenchmarkResult(ip, model, latency, tps)
							} else {
								info.Status = "发现"
							}
							result.Models = append(result.Models, info)
						}
						resultsChan <- result
					}
				} else {
					logger.LogScanResult(ip, nil, "端口关闭或服务未响应")
				}
				state.ScannedIPs[ip] = true
				progress.Increment()
			}
		}
	}

	// 启动工作协程
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go workerWithProgress(ctx, &wg, ips, state, progress)
	}

	// 读取IP并发送到通道
	go func() {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			ip := strings.TrimSpace(scanner.Text())
			if net.ParseIP(ip) != nil {
				ips <- ip
			}
		}
		close(ips)
	}()

	// 启动结果处理协程
	rhWg.Add(1)
	go func() {
		defer rhWg.Done()
		handleScanResults()
	}()

	// 等待所有工作协程完成
	wg.Wait()
	close(resultsChan)

	// 等待结果处理完成
	rhWg.Wait()
	csvWriter.Flush()

	// 保存最终状态
	close(stopSaving)
	state.LastScanTime = config.Now()
	if err := saveState(state); err != nil {
		log.Printf("保存最终扫描状态失败: %v", err)
	}

	fmt.Printf("\n✅ 扫描完成，结果已保存到: %s\n", *outputFile)
	return nil
}

func handleScanResults() {
	for res := range resultsChan {

		printResult(res)
		writeCSV(res)
	}
}

func printResult(res ScanResult) {
	fmt.Printf("\nIP地址: %s\n", res.IP)
	fmt.Println(strings.Repeat("-", 50))
	for _, model := range res.Models {
		fmt.Printf("├─ 模型: %-25s\n", model.Name)
		if !*disableBench {
			fmt.Printf("│ ├─ 状态: %s\n", model.Status)
			fmt.Printf("│ ├─ 首Token延迟: %v\n", model.FirstTokenDelay.Round(time.Millisecond))
			fmt.Printf("│ └─ 生成速度: %.1f tokens/s\n", model.TokensPerSec)
		} else {
			fmt.Printf("│ └─ 状态: %s\n", model.Status)
		}
		fmt.Println(strings.Repeat("-", 50))
	}
}

func writeCSV(res ScanResult) {
	for _, model := range res.Models {
		record := []string{res.IP, model.Name, model.Status}
		if !*disableBench {
			record = append(record,
				fmt.Sprintf("%.0f", model.FirstTokenDelay.Seconds()*1000),
				fmt.Sprintf("%.1f", model.TokensPerSec))
		}
		err := csvWriter.Write(record)
		if err != nil {
			fmt.Printf("⚠️ 写入CSV失败: %v\n", err) // Handle the error appropriately
		}
	}
}

func worker(ctx context.Context, wg *sync.WaitGroup, ips <-chan string) {
	defer wg.Done()
	for ip := range ips {
		select {
		case <-ctx.Done():
			return
		default:
			if checkPort(ip) && checkOllama(ip) {
				result := ScanResult{IP: ip}
				if models := getModels(ip); len(models) > 0 {
					models = sortModels(models)
					for _, model := range models {
						info := ModelInfo{Name: model}
						if !*disableBench {
							latency, tps, status := benchmarkModel(ip, model)
							info.FirstTokenDelay = latency
							info.TokensPerSec = tps
							info.Status = status
						} else {
							info.Status = "发现"
						}
						result.Models = append(result.Models, info)
					}
					resultsChan <- result
				}
			}
		}
	}
}

func checkPort(ip string) bool {
	result := net.Dialer{Timeout: timeout}
	conn, err := result.Dial("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return false
	}
	conn.Close()
	if *enableLog {
		logger.LogScanResult(ip, nil, fmt.Sprintf("端口检查: %v", result))
	}
	return true
}

func checkOllama(ip string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("http://%s:%d", ip, port), nil)
	if err != nil {
		return false
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}

	return strings.Contains(string(buf[:n]), "Ollama is running")
}
func getModels(ip string) []string {
	resp, err := httpClient.Get(fmt.Sprintf("http://%s:%d/api/tags", ip, port))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Models []struct {
			Model string `json:"model"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	var models []string
	for _, m := range data.Models {
		if strings.Contains(m.Model, "deepseek-r1") {
			models = append(models, m.Model)
		}
	}
	return models
}

func parseModelSize(model string) float64 {
	parts := strings.Split(model, ":")
	if len(parts) < 2 {
		return 0
	}

	sizeStr := strings.TrimSuffix(parts[len(parts)-1], "b")
	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		return 0
	}

	return size
}

func sortModels(models []string) []string {
	sort.Slice(models, func(i, j int) bool {
		return parseModelSize(models[i]) < parseModelSize(models[j])
	})
	return models
}

func benchmarkModel(ip, model string) (time.Duration, float64, string) {
	if *disableBench {
		return 0, 0, "未测试"
	}

	start := time.Now()
	payload := map[string]interface{}{
		"model":  model,
		"prompt": *benchPrompt,
		"stream": true,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("http://%s:%d/api/generate", ip, port),
		bytes.NewReader(body))
	client := &http.Client{Timeout: benchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, "连接失败"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Sprintf("HTTP错误: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	var (
		firstToken time.Time
		lastToken  time.Time
		tokenCount int
	)

	for scanner.Scan() {
		if tokenCount == 0 {
			firstToken = time.Now()
		}

		lastToken = time.Now()
		tokenCount++
		var data map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &data); err != nil {
			continue
		}

		if done, _ := data["done"].(bool); done {
			break
		}
	}

	if tokenCount == 0 {
		return 0, 0, "无响应"
	}

	totalTime := lastToken.Sub(start)
	return firstToken.Sub(start), float64(tokenCount) / totalTime.Seconds(), "完成"
}

// 获取可执行文件所在目录
func getExecutableDir() (string, error) {
	// 获取可执行文件路径
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	// 获取符号链接指向的真实路径
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", err
	}
	// 返回目录部分
	return filepath.Dir(realPath), nil
}
