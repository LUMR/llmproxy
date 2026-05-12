package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"litellm-proxy/config"
)

type RequestLog struct {
	Timestamp    time.Time              `json:"timestamp"`
	RequestID    string                 `json:"request_id"`
	Model        string                 `json:"model"`
	MappedModel  string                 `json:"mapped_model"`
	Request      map[string]interface{} `json:"request"`
	Response     interface{}            `json:"response,omitempty"`
	Duration     int64                  `json:"duration_ms"`
	IsStream     bool                   `json:"is_stream"`
	Success      bool                   `json:"success"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	TokenUsage   *TokenUsage            `json:"token_usage,omitempty"`
	Method       string                 `json:"method,omitempty"`
	Path         string                 `json:"path,omitempty"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ANSI 颜色代码
const (
	colorReset   = "\033[0m"
	colorGray    = "\033[90m"
	colorGreen   = "\033[32m"
	colorRed     = "\033[31m"
	colorYellow  = "\033[33m"
	colorCyan    = "\033[36m"
	colorMagenta = "\033[35m"
	colorBold    = "\033[1m"
)

type Logger struct {
	cfg *config.LoggingConfig
	ch  chan *RequestLog
	done chan struct{}
}

func New(cfg *config.LoggingConfig) (*Logger, error) {
	if cfg.Dir != "" {
		if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	l := &Logger{
		cfg:  cfg,
		ch:   make(chan *RequestLog, 256),
		done: make(chan struct{}),
	}

	// 启动后台写入 goroutine
	go l.writeLoop()

	return l, nil
}

// writeLoop 后台消费日志，串行写入文件
func (l *Logger) writeLoop() {
	for log := range l.ch {
		l.writeToFile(log)
	}
	close(l.done)
}

// Close 等待所有日志写入完成
func (l *Logger) Close() {
	close(l.ch)
	<-l.done
}

func (l *Logger) NewLog(requestID, model, mappedModel string, isStream bool) *RequestLog {
	return &RequestLog{
		Timestamp:   time.Now(),
		RequestID:   requestID,
		Model:       model,
		MappedModel: mappedModel,
		IsStream:    isStream,
	}
}

func (l *RequestLog) SetTokenUsage(prompt, completion, total int) {
	l.TokenUsage = &TokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	}
}

func (l *RequestLog) Finalize() {
	l.Duration = time.Since(l.Timestamp).Milliseconds()
}

// Save 提交日志到异步 channel，不阻塞调用方
func (l *Logger) Save(log *RequestLog) error {
	// 控制台输出（同步，因为 print 是线程安全的）
	l.printConsole(log)

	if !l.cfg.Enabled {
		return nil
	}

	log.Finalize()

	// 非阻塞发送，channel 满时丢弃（避免内存无限增长）
	select {
	case l.ch <- log:
	default:
		fmt.Fprintf(os.Stderr, "[logger] channel full, dropping log for request %s\n", log.RequestID)
	}

	return nil
}

// writeToFile 串行写入文件（只在 writeLoop 中调用）
func (l *Logger) writeToFile(log *RequestLog) {
	dateStr := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s_%s.jsonl", l.cfg.FilePrefix, dateStr)
	path := filepath.Join(l.cfg.Dir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[logger] failed to open log file: %v\n", err)
		return
	}
	defer f.Close()

	data, err := json.Marshal(log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[logger] failed to marshal log: %v\n", err)
		return
	}

	f.Write(data)
	f.Write([]byte{'\n'})
}

// printConsole 输出美化的控制台日志
func (l *Logger) printConsole(log *RequestLog) {
	if !l.cfg.Console {
		return
	}

	log.Finalize()

	// 时间和请求ID
	timeStr := log.Timestamp.Format("15:04:05")
	idStr := log.RequestID
	if len(idStr) > 6 {
		idStr = idStr[:6]
	}

	// 状态颜色
	var statusIcon, statusColor string
	if log.Success {
		statusIcon = "✓"
		statusColor = colorGreen
	} else {
		statusIcon = "✗"
		statusColor = colorRed
	}

	// HTTP 方法颜色
	methodColor := colorCyan
	switch log.Method {
	case "POST":
		methodColor = colorYellow
	case "GET":
		methodColor = colorGreen
	case "DELETE":
		methodColor = colorRed
	}

	// 构建输出
	var sb strings.Builder

	// 第一行：时间、请求ID、HTTP信息
	sb.WriteString(fmt.Sprintf("%s%s%s [%s%s%s] %s%s%s %s\n",
		colorGray, timeStr, colorReset,
		colorMagenta, idStr, colorReset,
		methodColor, log.Method, colorReset,
		log.Path))

	// 第二行：模型映射
	sb.WriteString(fmt.Sprintf("  Model: %s%s%s → %s%s%s\n",
		colorCyan, log.Model, colorReset,
		colorYellow, log.MappedModel, colorReset))

	// 第三行：Token 使用和耗时
	if log.TokenUsage != nil {
		sb.WriteString(fmt.Sprintf("  Tokens: %s%d%s in / %s%d%s out",
			colorGreen, log.TokenUsage.PromptTokens, colorReset,
			colorGreen, log.TokenUsage.CompletionTokens, colorReset))

		// 流式标记
		if log.IsStream {
			sb.WriteString(fmt.Sprintf(" %s[stream]%s", colorCyan, colorReset))
		}

		sb.WriteString(fmt.Sprintf(" | Duration: %s%dms%s\n",
			colorYellow, log.Duration, colorReset))
	} else {
		sb.WriteString(fmt.Sprintf("  Duration: %s%dms%s\n",
			colorYellow, log.Duration, colorReset))
	}

	// 第四行：状态
	if log.Success {
		sb.WriteString(fmt.Sprintf("  Status: %s%s Success%s\n",
			statusColor, statusIcon, colorReset))
	} else {
		sb.WriteString(fmt.Sprintf("  Status: %s%s Error%s",
			statusColor, statusIcon, colorReset))
		if log.ErrorMessage != "" {
			// 截断过长的错误信息
			errMsg := log.ErrorMessage
			if len(errMsg) > 100 {
				errMsg = errMsg[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf(" - %s", errMsg))
		}
		sb.WriteString("\n")
	}

	// 分隔线
	sb.WriteString(fmt.Sprintf("%s└────────────────────────────────────────%s\n",
		colorGray, colorReset))

	fmt.Print(sb.String())
}
