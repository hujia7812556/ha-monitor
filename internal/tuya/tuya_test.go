package tuya

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// mockHTTPClient 创建一个模拟的HTTP客户端，返回预定义的响应
func mockHTTPClient(t *testing.T, expectedPath string, response *tokenResponse) *http.Client {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求路径，需要包含查询参数
		fullPath := expectedPath
		if expectedPath == "/v1.0/token" {
			fullPath = "/v1.0/token?grant_type=1"
		}
		if r.URL.String() != fullPath {
			t.Errorf("Expected path %s, got %s", fullPath, r.URL.String())
		}

		// 验证必要的请求头
		requiredHeaders := []string{
			"client_id",
			"sign",
			"t",
			"sign_method",
			"Signature-Headers",
		}

		for _, header := range requiredHeaders {
			if r.Header.Get(header) == "" {
				t.Errorf("Missing required header: %s", header)
				// 设置一个默认值以便测试可以继续
				r.Header.Set(header, "test_value")
			}
		}

		// 返回模拟响应
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))

	t.Cleanup(func() {
		server.Close()
	})

	// 修改Transport的实现
	return &http.Client{
		Transport: &http.Transport{
			Proxy: func(req *http.Request) (*url.URL, error) {
				// 保持原始请求头
				serverURL, err := url.Parse(server.URL)
				if err != nil {
					return nil, err
				}
				// 将请求重定向到测试服务器，但保持原始路径
				serverURL.Path = req.URL.Path
				serverURL.RawQuery = req.URL.RawQuery
				return serverURL, nil
			},
		},
	}
}

func TestGetNewToken(t *testing.T) {
	// 修改测试数据
	mockResponse := &tokenResponse{
		Success: true,
		Result: struct {
			AccessToken  string `json:"access_token"`
			ExpireTime   int64  `json:"expire_time"`
			RefreshToken string `json:"refresh_token"`
			Uid          string `json:"uid"` // 添加 uid 字段
		}{
			AccessToken:  "test_access_token",
			ExpireTime:   3600,
			RefreshToken: "test_refresh_token",
			Uid:          "test_uid", // 添加测试 uid
		},
	}

	// 创建测试配置
	config := Config{
		Enabled:      true,
		AccessID:     "test_access_id",
		AccessSecret: "test_access_secret",
		DeviceID:     "test_device_id",
		Region:       "cn",
		WaitSeconds:  10,
	}

	// 创建带有mock HTTP客户端的Tuya客户端
	client := NewClient(config, mockHTTPClient(t, "/v1.0/token?grant_type=1", mockResponse))

	// 测试获取新token
	token, err := client.getNewToken()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// 验证返回的token
	if token.AccessToken != mockResponse.Result.AccessToken {
		t.Errorf("Expected access token %s, got %s", mockResponse.Result.AccessToken, token.AccessToken)
	}

	if token.RefreshToken != mockResponse.Result.RefreshToken {
		t.Errorf("Expected refresh token %s, got %s", mockResponse.Result.RefreshToken, token.RefreshToken)
	}

	// 验证过期时间是否正确设置（考虑5分钟的缓冲）
	expectedExpireTime := time.Now().Add(time.Duration(mockResponse.Result.ExpireTime)*time.Second - 5*time.Minute)
	timeDiff := token.ExpireTime.Sub(expectedExpireTime)
	if timeDiff > time.Second || timeDiff < -time.Second {
		t.Errorf("Expire time not set correctly. Expected around %v, got %v", expectedExpireTime, token.ExpireTime)
	}

	// 添加 uid 验证
	if token.Uid != mockResponse.Result.Uid {
		t.Errorf("Expected uid %s, got %s", mockResponse.Result.Uid, token.Uid)
	}
}

func TestGetNewTokenFailure(t *testing.T) {
	// 准备失败响应
	mockResponse := &tokenResponse{
		Success: false,
	}

	// 创建测试配置
	config := Config{
		Enabled:      true,
		AccessID:     "test_access_id",
		AccessSecret: "test_access_secret",
		DeviceID:     "test_device_id",
		Region:       "cn",
		WaitSeconds:  10,
	}

	// 创建带有mock HTTP客户端的Tuya客户端
	client := NewClient(config, mockHTTPClient(t, "/v1.0/token", mockResponse))

	// 测试获取新token失败的情况
	_, err := client.getNewToken()
	if err == nil {
		t.Error("Expected error for failed token request, got nil")
	}
}
