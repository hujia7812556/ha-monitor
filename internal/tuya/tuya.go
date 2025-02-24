package tuya

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	Enabled      bool
	AccessID     string
	AccessSecret string
	DeviceID     string
	Region       string
	WaitSeconds  int
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

func (c *Client) getTokenSign(clientID string, secret string, timestamp int64, nonce string, stringToSign string) string {
	// 构建完整签名字符串：client_id + t + nonce + stringToSign
	str := fmt.Sprintf("%s%d%s%s", clientID, timestamp, nonce, stringToSign)

	// 使用 HMAC-SHA256 计算签名
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(str))

	// 转换为大写的十六进制字符串
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}

func generateNonce() string {
	// 创建一个 16 字节的随机数
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// 如果生成失败，使用时间戳作为备选
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	// 使用 base64 编码，并移除可能的特殊字符
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func (c *Client) getNewToken() (*tokenInfo, error) {
	timestamp := time.Now().UnixMilli()
	path := "/v1.0/token?grant_type=1"

	// 生成自定义字段值
	areaID := fmt.Sprintf("%x", sha256.Sum256([]byte(c.config.AccessID)))[:16]
	callID := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d", timestamp))))[:32]

	// 构建 Optional_Signature_key
	optionalSignatureKey := fmt.Sprintf("area_id:%s\ncall_id:%s\n", areaID, callID)

	// 生成 nonce
	nonce := generateNonce()
	// 生成 content-SHA256，因为Body为空，这里使用空字符串的hash值
	contentSHA256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s", "GET", contentSHA256, optionalSignatureKey, path)
	log.Printf("stringToSign: \n%s", stringToSign)

	// 获取令牌的签名，传入所有必要参数
	signStr := c.getTokenSign(c.config.AccessID, c.config.AccessSecret, timestamp, nonce, stringToSign)

	url := fmt.Sprintf("https://openapi.tuya%s.com%s", c.config.Region, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 设置请求头
	req.Header.Set("method", "GET")
	req.Header.Set("client_id", c.config.AccessID)
	req.Header.Set("secret", c.config.AccessSecret)
	req.Header.Set("t", fmt.Sprintf("%d", timestamp))
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("Signature-Headers", "area_id:call_id") // 指定参与签名的字段
	req.Header.Set("area_id", areaID)                      // 设置自定义字段
	req.Header.Set("call_id", callID)                      // 设置自定义字段

	// 打印请求信息以便调试
	log.Printf("Request URL: %s", url)
	log.Printf("Request Headers: client_id=%s, t=%d, area_id=%s, call_id=%s",
		c.config.AccessID, timestamp, areaID, callID)
	log.Printf("Sign Message: %s", signStr)

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

	// 刷新令牌的签名
	signStr := c.getTokenSign(c.config.AccessID, c.config.AccessSecret, timestamp, "", path)

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

	// 计算请求体的哈希值
	contentHash := fmt.Sprintf("%x", sha256.Sum256(jsonData))

	// 获取访问令牌
	token, err := c.getToken()
	if err != nil {
		return fmt.Errorf("get token failed: %w", err)
	}

	// 计算签名
	signStr := c.getTokenSign(c.config.AccessID, c.config.AccessSecret, timestamp, "", contentHash)

	url := fmt.Sprintf("https://openapi.tuya%s.com%s", c.config.Region, path)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

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

func (c *Client) GetConfig() Config {
	return c.config
}
