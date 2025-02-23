package tuya

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled     bool
	AccessID    string
	AccessKey   string
	DeviceID    string
	Region      string
	WaitSeconds int
}

type Client struct {
	config Config
	client *http.Client
	token  *tokenInfo
	mu     sync.RWMutex
}

type tokenInfo struct {
	AccessToken  string    `json:"access_token"`
	ExpireTime   time.Time // 本地计算的过期时间
	RefreshToken string    `json:"refresh_token"`
}

type tokenResponse struct {
	Success bool `json:"success"`
	Result  struct {
		AccessToken  string `json:"access_token"`
		ExpireTime   int64  `json:"expire_time"`
		RefreshToken string `json:"refresh_token"`
	} `json:"result"`
}

func NewClient(config Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		config: config,
		client: httpClient,
	}
}

func (c *Client) RestartDevice() error {
	if !c.config.Enabled {
		return nil
	}

	// 关闭电源
	if err := c.controlSwitch(false); err != nil {
		return fmt.Errorf("failed to turn off switch: %w", err)
	}

	// 等待指定时间
	time.Sleep(time.Duration(c.config.WaitSeconds) * time.Second)

	// 开启电源
	if err := c.controlSwitch(true); err != nil {
		return fmt.Errorf("failed to turn on switch: %w", err)
	}

	return nil
}

func (c *Client) getNewToken() (*tokenInfo, error) {
	timestamp := time.Now().UnixMilli()
	path := "/v1.0/token?grant_type=1"

	// 获取令牌时的签名计算方式不同
	// 直接使用 HMAC-SHA256(accessId + timestamp, accessKey)
	h := hmac.New(sha256.New, []byte(c.config.AccessKey))
	message := fmt.Sprintf("%s%d", c.config.AccessID, timestamp)
	h.Write([]byte(message))
	signStr := strings.ToUpper(hex.EncodeToString(h.Sum(nil)))

	url := fmt.Sprintf("https://openapi.tuya%s.com%s", c.config.Region, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 设置请求头
	req.Header.Set("client_id", c.config.AccessID)
	req.Header.Set("sign", signStr) // 使用 sign 而不是 secret
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("t", fmt.Sprintf("%d", timestamp))

	// 打印请求信息以便调试
	log.Printf("Request URL: %s", url)
	log.Printf("Request Headers: client_id=%s, t=%d", c.config.AccessID, timestamp)
	log.Printf("Sign Message: %s", message)
	log.Printf("Sign Result: %s", signStr)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get new token failed: %w", err)
	}
	defer resp.Body.Close()

	// 读取并打印原始响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}
	log.Printf("Get token response: %s", string(respBody))

	// 解析响应
	var result tokenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode token response failed: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("get new token failed: %s", string(respBody))
	}

	return &tokenInfo{
		AccessToken:  result.Result.AccessToken,
		RefreshToken: result.Result.RefreshToken,
		ExpireTime:   time.Now().Add(time.Duration(result.Result.ExpireTime)*time.Second - 5*time.Minute),
	}, nil
}

func (c *Client) refreshToken(refreshToken string) (*tokenInfo, error) {
	timestamp := time.Now().UnixMilli()
	path := "/v1.0/token/" + refreshToken

	// GET 请求的签名字符串格式
	stringToSign := fmt.Sprintf("GET\n\n\n%s", path)
	signStr := c.calculateSign(stringToSign, timestamp)

	url := fmt.Sprintf("https://openapi.tuya%s.com%s", c.config.Region, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 设置请求头
	req.Header.Set("client_id", c.config.AccessID)
	req.Header.Set("sign", signStr)
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("t", fmt.Sprintf("%d", timestamp))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token failed: %w", err)
	}
	defer resp.Body.Close()

	var result tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode refresh response failed: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("refresh token failed")
	}

	return &tokenInfo{
		AccessToken:  result.Result.AccessToken,
		RefreshToken: result.Result.RefreshToken,
		ExpireTime:   time.Now().Add(time.Duration(result.Result.ExpireTime)*time.Second - 5*time.Minute),
	}, nil
}

func (c *Client) getToken() (string, error) {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	// 如果令牌存在且未过期，直接返回
	if token != nil && time.Now().Before(token.ExpireTime) {
		return token.AccessToken, nil
	}

	// 需要获取新令牌
	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查，避免并发获取令牌
	if c.token != nil && time.Now().Before(c.token.ExpireTime) {
		return c.token.AccessToken, nil
	}

	// 如果有 refresh_token，尝试刷新
	if c.token != nil && c.token.RefreshToken != "" {
		newToken, err := c.refreshToken(c.token.RefreshToken)
		if err == nil {
			c.token = newToken
			return newToken.AccessToken, nil
		}
		// 刷新失败，继续获取新令牌
	}

	// 获取新令牌
	newToken, err := c.getNewToken()
	if err != nil {
		return "", fmt.Errorf("get new token failed: %w", err)
	}

	c.token = newToken
	return newToken.AccessToken, nil
}

func (c *Client) controlSwitch(on bool) error {
	// 获取访问令牌
	token, err := c.getToken()
	if err != nil {
		return fmt.Errorf("get token failed: %w", err)
	}

	timestamp := time.Now().UnixMilli()
	path := fmt.Sprintf("/v1.0/iot-03/devices/%s/commands", c.config.DeviceID)
	body := map[string]interface{}{
		"commands": []map[string]interface{}{
			{
				"code":  "switch_1",
				"value": on,
			},
		},
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://openapi.tuya%s.com%s", c.config.Region, path)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	// POST 请求的签名字符串格式：
	// client_id + access_token + t + stringToSign
	// stringToSign = method + "\n" + body_hash + "\n" + headers_hash + "\n" + url
	contentHash := fmt.Sprintf("%x", sha256.Sum256(jsonData))
	stringToSign := fmt.Sprintf("POST\n%s\n\n%s", contentHash, path)
	signStr := c.calculateSign(stringToSign, timestamp)

	// 设置请求头
	req.Header.Set("client_id", c.config.AccessID)
	req.Header.Set("access_token", token)
	req.Header.Set("sign", signStr)
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("t", fmt.Sprintf("%d", timestamp))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response failed: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("tuya API returned error: code=%d, msg=%s", result.Code, result.Msg)
	}

	return nil
}

func (c *Client) calculateSign(stringToSign string, timestamp int64) string {
	// 生成签名字符串：client_id + t + stringToSign
	str := fmt.Sprintf("%s%d%s", c.config.AccessID, timestamp, stringToSign)

	// 使用 HMAC-SHA256 计算签名
	h := hmac.New(sha256.New, []byte(c.config.AccessKey))
	h.Write([]byte(str))

	// 转换为大写的十六进制字符串
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}

func (c *Client) GetConfig() Config {
	return c.config
}
