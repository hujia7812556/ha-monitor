package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ha-monitor/internal/config"
	"ha-monitor/internal/monitor"
	"ha-monitor/internal/tuya"

	"github.com/robfig/cron/v3"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	log.Println("Starting HomeAssistant Monitor...")

	loader, err := config.NewLoader(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cfg := loader.Get()
	mon := monitor.NewMonitor(
		cfg.Monitor.HAURL,
		cfg.Monitor.HAToken,
		monitor.NotifyConfig{
			APIURL:   cfg.Monitor.Notify.APIURL,
			APIToken: cfg.Monitor.Notify.APIToken,
			TopicID:  cfg.Monitor.Notify.TopicID,
		},
		tuya.Config{
			Enabled:     cfg.Monitor.Tuya.Enabled,
			AccessID:    cfg.Monitor.Tuya.AccessID,
			AccessKey:   cfg.Monitor.Tuya.AccessKey,
			DeviceID:    cfg.Monitor.Tuya.DeviceID,
			Region:      cfg.Monitor.Tuya.Region,
			WaitSeconds: cfg.Monitor.Tuya.WaitSeconds,
		},
		cfg.Monitor.RetryTimes,
		cfg.Monitor.Timeout,
	)

	// 创建一个支持秒级调度的cron调度器
	// 注意：配置虽然支持热加载，但schedule字段的更改并不会更新cron调度器
	c := cron.New(cron.WithSeconds())

	if _, err := c.AddFunc(cfg.Monitor.Schedule, func() {
		currentCfg := loader.Get()
		mon.UpdateConfig(
			currentCfg.Monitor.HAURL,
			currentCfg.Monitor.HAToken,
			monitor.NotifyConfig{
				APIURL:   currentCfg.Monitor.Notify.APIURL,
				APIToken: currentCfg.Monitor.Notify.APIToken,
				TopicID:  currentCfg.Monitor.Notify.TopicID,
			},
			tuya.Config{
				Enabled:     currentCfg.Monitor.Tuya.Enabled,
				AccessID:    currentCfg.Monitor.Tuya.AccessID,
				AccessKey:   currentCfg.Monitor.Tuya.AccessKey,
				DeviceID:    currentCfg.Monitor.Tuya.DeviceID,
				Region:      currentCfg.Monitor.Tuya.Region,
				WaitSeconds: currentCfg.Monitor.Tuya.WaitSeconds,
			},
			currentCfg.Monitor.RetryTimes,
			currentCfg.Monitor.Timeout,
		)

		if err := mon.Check(); err != nil {
			log.Printf("Monitor check failed: %v", err)
		}
	}); err != nil {
		log.Fatalf("Failed to add cron job: %v", err)
	}

	c.Start()

	// 优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down gracefully...")

	c.Stop()
}
