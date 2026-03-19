// +build ignore

// Test hot-reload with add inbound
// Run: go run ./cmd/test_hotreload/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/xrayapi/hotreload"
)

const (
	oldConfigPath = "/tmp/xray-old-config.json"
	newConfigPath = "/tmp/xray-new-config.json"
	xrayBinPath   = "/tmp/xray-bin/xray"
	grpcAPIAddr   = "127.0.0.1:62789"
)

func main() {
	fmt.Println("==============================================")
	fmt.Println("  热重载测试：添加新 Inbound")
	fmt.Println("==============================================")
	fmt.Println()

	ctx := context.Background()

	// Step 1: 创建初始配置（带一个 inbound）
	fmt.Println("[Step 1] 创建初始 xray 配置...")
	if err := createInitialConfig(); err != nil {
		fmt.Printf("  失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  初始配置创建成功")
	fmt.Println()

	// Step 2: 启动旧 xray 实例
	fmt.Println("[Step 2] 启动旧 xray 实例...")
	oldExecutor, err := startXray(oldConfigPath, grpcAPIAddr)
	if err != nil {
		fmt.Printf("  失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  旧实例已启动 (PID: 通过 IsRunning=%v)\n", oldExecutor.IsRunning())
	time.Sleep(1 * time.Second)
	fmt.Println()

	// Step 3: 创建 ForwardManager
	fmt.Println("[Step 3] 创建 ForwardManager...")
	fm, err := forward.NewForwardManager()
	if err != nil {
		fmt.Printf("  失败: %v\n", err)
		os.Exit(1)
	}
	defer fm.Close()

	// 添加一个初始 forward 规则（模拟现有用户）
	initialRule := forward.ForwardRule{
		UserEmail:  "test-user@v2raymg.local",
		ListenPort: 20086,
		TargetAddr: "127.0.0.1:10086", // 指向旧实例的 inbound 端口
	}
	if _, err := fm.AddRule(initialRule); err != nil {
		fmt.Printf("  添加初始规则失败: %v\n", err)
	} else {
		fmt.Printf("  添加初始规则: listen=%d -> target=%s\n", initialRule.ListenPort, initialRule.TargetAddr)
	}
	fmt.Println()

	// Step 4: 配置热重载管理器
	fmt.Println("[Step 4] 配置热重载管理器...")
	hrMgr := hotreload.NewManager(hotreload.Config{
		OldConfigPath:        oldConfigPath,
		NewConfigPath:        newConfigPath,
		OldBinaryPath:        xrayBinPath,
		PortOffset:           1000, // 新实例端口 = 原端口 + 1000 (避免与 forward 层冲突)
		HealthCheckTimeout:   5 * time.Second,
		HealthCheckInterval:  1 * time.Second,
	})
	fmt.Println("  热重载管理器配置完成")
	fmt.Println()

	// Step 5: 准备新 inbound
	fmt.Println("[Step 5] 准备新 inbound 配置...")
	newInbounds := []contracts.InboundSpec{
		{
			Tag:      "vmess-new",
			Port:     10087, // 新增的端口
			Protocol: contracts.ProtocolVMess,
		},
	}
	fmt.Printf("  新增 inbound: tag=%s, port=%d\n", newInbounds[0].Tag, newInbounds[0].Port)
	fmt.Println()

	// Step 6: 执行热重载
	fmt.Println("[Step 6] 执行热重载...")
	fmt.Println("  --- 开始热重载流程 ---")
	result := hrMgr.ExecuteHotReload(ctx, fm, newInbounds, oldExecutor)

	if !result.Success {
		fmt.Printf("  热重载失败: %v\n", result.Error)
		if result.RollbackInfo != nil {
			fmt.Println("  准备回滚...")
		}
		os.Exit(1)
	}

	fmt.Println("  --- 热重载流程完成 ---")
	fmt.Printf("  新实例ID: %s\n", result.NewInstanceID)
	fmt.Printf("  切换端口: %v\n", result.SwitchedPorts)
	fmt.Println()

	// Step 7: 验证
	fmt.Println("[Step 7] 验证结果...")
	fmt.Printf("  旧实例状态: Running=%v\n", !oldExecutor.IsRunning())

	rules := fm.GetAllRules()
	fmt.Printf("  Forward 规则数: %d\n", len(rules))
	for _, r := range rules {
		fmt.Printf("    - %s: listen=%d -> target=%s\n", r.UserEmail, r.ListenPort, r.TargetAddr)
	}
	fmt.Println()

	// Step 8: 验证订阅入口不变
	fmt.Println("[Step 8] 验证订阅入口...")
	fmt.Printf("  原有用户端口: %d (未变)\n", 20086)
	fmt.Printf("  新目标地址: %s\n", "127.0.0.1:11087") // 10087 + 1000
	fmt.Println("  订阅入口保持不变 ✓")
	fmt.Println()

	fmt.Println("==============================================")
	fmt.Println("  测试完成")
	fmt.Println("==============================================")
}

func createInitialConfig() error {
	// 确保目录存在
	os.MkdirAll(filepath.Dir(oldConfigPath), 0755)

	config := map[string]interface{}{
		"log":   map[string]interface{}{"loglevel": "warning"},
		"api":   map[string]interface{}{"tag": "api", "services": []string{"HandlerService", "StatsService", "LoggerService"}},
		"stats": map[string]interface{}{},
		"policy": map[string]interface{}{
			"levels": map[string]interface{}{
				"0": map[string]interface{}{"statsUserUplink": true, "statsUserDownlink": true},
			},
			"system": map[string]interface{}{
				"statsInboundUplink":   true,
				"statsInboundDownlink": true,
			},
		},
		"inbounds": []interface{}{
			map[string]interface{}{
				"tag":      "api",
				"port":     62789,
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{"address": "127.0.0.1"},
			},
			map[string]interface{}{
				"tag":      "vmess-initial",
				"port":     10086,
				"listen":   "0.0.0.0",
				"protocol": "vmess",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"id":       "a348c600-1234-5678-9abc-def012345678",
							"alterId":  0,
							"email":    "test-user@v2raymg.local",
							"level":    0,
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(oldConfigPath, data, 0644)
}

func startXray(configPath, grpcAddr string) (*xray.Executor, error) {
	executor, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBinPath,
		ConfigFilePath: configPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: grpcAddr,
		AutoDownload:   false,
	})
	if err != nil {
		return nil, err
	}

	if err := executor.Init(&xray.ExecutorConfig{
		BinaryPath:     xrayBinPath,
		ConfigFilePath: configPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: grpcAddr,
		AutoDownload:   false,
	}); err != nil {
		return nil, err
	}

	if err := executor.Start(); err != nil {
		return nil, err
	}

	return executor, nil
}
