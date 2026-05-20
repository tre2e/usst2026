package main

import (
	"bufio"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	loadDotEnv()

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	log.Println("正在登录一网畅学...")
	session, err := Login(cfg.Username, cfg.Password)
	if err != nil {
		log.Fatalf("登录失败: %v", err)
	}
	log.Printf("登录成功，session已获取 (%d字符)", len(session))

	store := LoadStore()
	notifier := NewNotifier(cfg)
	monitor := NewMonitor(session, store, notifier)

	// Test SMTP on startup
	log.Println("正在测试SMTP连接...")
	if err := notifier.Test(); err != nil {
		log.Printf("SMTP测试失败: %v", err)
	} else {
		log.Println("SMTP测试成功")
	}

	// Cleanup old records (>7 days) on startup
	store.Cleanup(7 * 24 * 3600)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sessionRefresh := time.NewTicker(30 * time.Minute)
	pollTicker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)

	log.Printf("开始监听签到 (轮询间隔=%ds)", cfg.PollInterval)

	for {
		select {
		case <-sigCh:
			log.Println("收到退出信号，正在关闭...")
			return

		case <-sessionRefresh.C:
			log.Println("正在刷新session...")
			newSess, err := Login(cfg.Username, cfg.Password)
			if err != nil {
				log.Printf("刷新session失败: %v — 继续使用旧session", err)
			} else {
				monitor.UpdateSession(newSess)
			}

		case <-pollTicker.C:
			if err := monitor.Poll(); err != nil {
				log.Printf("轮询失败: %v", err)
			}
		}
	}
}

func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Remove surrounding quotes if present
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
