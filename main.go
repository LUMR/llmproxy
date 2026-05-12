package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"litellm-proxy/config"
	"litellm-proxy/handler"
	"litellm-proxy/logger"

	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 检查 API Key
	if cfg.Zhipu.APIKey == "" {
		log.Fatal("ZHIPU_API_KEY environment variable is required")
	}

	// 初始化日志
	logInstance, err := logger.New(&cfg.Logging)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logInstance.Close()

	// 创建代理处理器
	proxyHandler := handler.NewProxyHandler(cfg, logInstance)

	// 设置 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 健康检查 + 代理所有请求
	r.Any("/*path", handler.BearerAuth(cfg), func(c *gin.Context) {
		if c.Param("path") == "/health" && c.Request.Method == "GET" {
			c.JSON(200, gin.H{"status": "ok"})
			return
		}
		proxyHandler.HandleAll(c)
	})

	// 启动服务
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Printf("Starting proxy server on %s", addr)
		log.Printf("Anthropic proxy: %s", cfg.Zhipu.APIBase)
		if cfg.OpenAI.APIBase != "" {
			log.Printf("OpenAI proxy: %s", cfg.OpenAI.APIBase)
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 等待中断信号，优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
