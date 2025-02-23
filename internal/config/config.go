package config

import (
	"fmt"
	"log"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type Config struct {
	Monitor MonitorConfig `mapstructure:"monitor"`
}

type MonitorConfig struct {
	HAURL      string       `mapstructure:"ha_url"`
	HAToken    string       `mapstructure:"ha_token"`
	RetryTimes int          `mapstructure:"retry_times"`
	Timeout    int          `mapstructure:"timeout"`
	Schedule   string       `mapstructure:"schedule"`
	Notify     NotifyConfig `mapstructure:"notify"`
	Tuya       TuyaConfig   `mapstructure:"tuya"`
}

type NotifyConfig struct {
	APIURL   string `mapstructure:"api_url"`
	APIToken string `mapstructure:"api_token"`
	TopicID  int    `mapstructure:"topic_id"`
}

type TuyaConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	AccessID    string `mapstructure:"access_id"`
	AccessKey   string `mapstructure:"access_key"`
	DeviceID    string `mapstructure:"device_id"`
	Region      string `mapstructure:"region"`
	WaitSeconds int    `mapstructure:"wait_seconds"`
}

type Loader struct {
	mu     sync.RWMutex
	config *Config
	v      *viper.Viper
}

func NewLoader(path string) (*Loader, error) {
	l := &Loader{
		v: viper.New(),
	}

	l.v.SetConfigFile(path)
	l.v.SetConfigType("yaml")

	if err := l.v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := l.load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	l.v.OnConfigChange(func(e fsnotify.Event) {
		log.Printf("Config file changed: %s\n", e.Name)
		if err := l.load(); err != nil {
			fmt.Printf("Reload config failed: %v\n", err)
		}
	})
	l.v.WatchConfig()

	return l, nil
}

func (l *Loader) load() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	l.config = &cfg
	return nil
}

func (l *Loader) Get() *Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config
}
