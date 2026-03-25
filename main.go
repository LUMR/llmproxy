package main

import (
	"fmt"
	"log"
	"os"

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

	// 创建代理处理器
	proxyHandler := handler.NewProxyHandler(cfg, logInstance)

	// 设置 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 健康检查
	// r.GET("/health", func(c *gin.Context) {
	// 	c.JSON(200, gin.H{"status": "ok"})
	// })

	// 代理所有 /v1/* 请求
	r.Any("/*path", proxyHandler.HandleAll)

	// 启动服务
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting proxy server on %s", addr)
	log.Printf("Proxying to: %s", cfg.Zhipu.APIBase)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
