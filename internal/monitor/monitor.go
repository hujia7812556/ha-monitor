package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type Monitor struct {
	url         string
	token       string
	notifyConf  NotifyConfig
	retryTimes  int
	timeout     time.Duration
	failCount   int
	hasNotified bool // 添加标记，记录是否已发送故障通知
	client      *http.Client
}

type NotifyConfig struct {
	APIURL   string
	APIToken string
	TopicID  int
}

var successStatusCodes = []int{
	http.StatusOK,        // 200
	http.StatusCreated,   // 201
	http.StatusAccepted,  // 202
	http.StatusNoContent, // 204
}

func isSuccessStatus(code int) bool {
	for _, status := range successStatusCodes {
		if code == status {
			return true
		}
	}
	return false
}

func NewMonitor(url string, token string, notify NotifyConfig, retryTimes int, timeout int) *Monitor {
	if timeout <= 0 {
		timeout = 10
	}

	return &Monitor{
		url:         url,
		token:       token,
		notifyConf:  notify,
		retryTimes:  retryTimes,
		timeout:     time.Duration(timeout) * time.Second,
		client:      &http.Client{Timeout: time.Duration(timeout) * time.Second},
		hasNotified: false,
		failCount:   0,
	}
}

func (m *Monitor) Check() error {
	req, err := http.NewRequest("GET", m.url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// 添加认证头
	req.Header.Add("Authorization", "Bearer "+m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		m.failCount++
		if m.failCount >= m.retryTimes {
			if err := m.notifyDown(); err != nil {
				return fmt.Errorf("notification failed: %w", err)
			}
			m.hasNotified = true
		}
		return err
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		m.failCount++
		if m.failCount >= m.retryTimes {
			if err := m.notifyDown(); err != nil {
				return fmt.Errorf("notification failed: %w", err)
			}
			m.hasNotified = true
		}
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 服务恢复正常时
	if m.hasNotified {
		if err := m.notifyUp(); err != nil {
			log.Printf("Failed to send recovery notification: %v", err)
		}
		m.hasNotified = false
		m.failCount = 0 // 只在服务恢复时重置计数
	}

	log.Printf("HomeAssistant service is healthy, status code: %d", resp.StatusCode)
	return nil
}

func (m *Monitor) notifyDown() error {
	payload := map[string]interface{}{
		"platform": "wechat",
		"summary":  "HomeAssistant服务异常",
		"content":  fmt.Sprintf("HomeAssistant service is down after %d retries", m.retryTimes),
		"extra": map[string]interface{}{
			"topic_id": m.notifyConf.TopicID,
		},
	}
	return m.sendNotification(payload)
}

func (m *Monitor) notifyUp() error {
	payload := map[string]interface{}{
		"platform": "wechat",
		"summary":  "HomeAssistant服务已恢复",
		"content":  "HomeAssistant service has recovered",
		"extra": map[string]interface{}{
			"topic_id": m.notifyConf.TopicID,
		},
	}
	return m.sendNotification(payload)
}

func (m *Monitor) sendNotification(payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", m.notifyConf.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-API-Token", m.notifyConf.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		return fmt.Errorf("notification API returned status code: %d", resp.StatusCode)
	}

	return nil
}

func (m *Monitor) UpdateConfig(url string, token string, notify NotifyConfig, retryTimes int, timeout int) {
	m.url = url
	m.token = token
	m.notifyConf = notify
	m.retryTimes = retryTimes
	if timeout <= 0 {
		timeout = 10
	}
	m.timeout = time.Duration(timeout) * time.Second
	m.client.Timeout = m.timeout
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime) // 只显示日期和时间（到秒）
}
