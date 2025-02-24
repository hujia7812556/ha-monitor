package tuya

import (
	"os"
	"testing"
	"time"

	"ha-monitor/internal/testutil"
)

// 全局限流器
var limiter = testutil.NewRateLimiter(1.0) // 每秒1次请求

// 从环境变量获取测试配置
func getTestConfig(t *testing.T) Config {
	t.Helper()

	accessID := os.Getenv("TUYA_ACCESS_ID")
	accessSecret := os.Getenv("TUYA_ACCESS_SECRET")
	deviceID := os.Getenv("TUYA_DEVICE_ID")
	region := os.Getenv("TUYA_REGION")

	if accessID == "" || accessSecret == "" || deviceID == "" {
		t.Skip("Skipping integration test: TUYA_ACCESS_ID, TUYA_ACCESS_SECRET, and TUYA_DEVICE_ID environment variables must be set")
	}

	if region == "" {
		region = "cn" // 默认使用中国区
	}

	return Config{
		Enabled:      true,
		AccessID:     accessID,
		AccessSecret: accessSecret,
		DeviceID:     deviceID,
		Region:       region,
		WaitSeconds:  10,
	}
}

func TestIntegrationGetNewToken(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	limiter.Wait() // 使用新的 Wait 方法
	config := getTestConfig(t)
	client := NewClient(config, nil)

	// 测试获取新token
	token, err := client.getNewToken()
	if err != nil {
		t.Fatalf("Failed to get new token: %v", err)
	}

	// 验证返回的token
	if token.AccessToken == "" {
		t.Error("Expected non-empty access token")
	}

	if token.RefreshToken == "" {
		t.Error("Expected non-empty refresh token")
	}

	// 验证过期时间是否在将来
	if !token.ExpireTime.After(time.Now()) {
		t.Error("Token expiration time should be in the future")
	}

	// 添加 uid 验证
	if token.Uid == "" {
		t.Error("Expected non-empty uid")
	}
}

func TestIntegrationRefreshToken(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	limiter.Wait() // 使用新的 Wait 方法
	config := getTestConfig(t)
	client := NewClient(config, nil)

	// 首先获取一个新token
	initialToken, err := client.getNewToken()
	if err != nil {
		t.Fatalf("Failed to get initial token: %v", err)
	}

	limiter.Wait() // 使用新的 Wait 方法

	// 测试刷新token
	refreshedToken, err := client.refreshToken(initialToken.RefreshToken)
	if err != nil {
		t.Fatalf("Failed to refresh token: %v", err)
	}

	// 验证刷新后的token
	if refreshedToken.AccessToken == "" {
		t.Error("Expected non-empty access token after refresh")
	}

	if refreshedToken.AccessToken == initialToken.AccessToken {
		t.Error("Refreshed token should be different from initial token")
	}

	if refreshedToken.RefreshToken == "" {
		t.Error("Expected non-empty refresh token after refresh")
	}

	// 添加 uid 验证
	if refreshedToken.Uid == "" {
		t.Error("Expected non-empty uid after refresh")
	}

	if refreshedToken.Uid != initialToken.Uid {
		t.Error("Refreshed token uid should be same as initial token")
	}
}

func TestIntegrationControlSwitch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	limiter.Wait() // 使用新的 Wait 方法
	config := getTestConfig(t)
	client := NewClient(config, nil)

	// 测试开关控制
	// 注意：这个测试会实际控制设备，请确保设备可以安全地被开关
	t.Log("Testing switch control - turning off")
	if err := client.controlSwitch(false); err != nil {
		t.Fatalf("Failed to turn off switch: %v", err)
	}

	limiter.Wait() // 使用新的 Wait 方法

	t.Log("Testing switch control - turning on")
	if err := client.controlSwitch(true); err != nil {
		t.Fatalf("Failed to turn on switch: %v", err)
	}
}

func TestIntegrationRestartDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	limiter.Wait() // 使用新的 Wait 方法
	config := getTestConfig(t)
	client := NewClient(config, nil)

	t.Log("Testing device restart")
	if err := client.RestartDevice(); err != nil {
		t.Fatalf("Failed to restart device: %v", err)
	}
}
