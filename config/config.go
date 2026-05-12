package config

import (
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig      `yaml:"server"`
	Auth         AuthConfig        `yaml:"auth"`
	Zhipu        UpstreamConfig    `yaml:"zhipu"`
	OpenAI       UpstreamConfig    `yaml:"openai"`
	ModelMapping map[string]string `yaml:"model_mapping"`
	Logging      LoggingConfig     `yaml:"logging"`
}

type AuthConfig struct {
	Enabled     bool   `yaml:"enabled"`
	BearerToken string `yaml:"bearer_token"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type UpstreamConfig struct {
	APIBase string `yaml:"api_base"`
	APIKey  string `yaml:"api_key"`
}

type LoggingConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Dir        string `yaml:"dir"`
	FilePrefix string `yaml:"file_prefix"`
	Console    bool   `yaml:"console"` // 控制台美化输出
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 展开环境变量
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) MapModel(anthropicModel string) string {
	if mapped, ok := c.ModelMapping[anthropicModel]; ok {
		return mapped
	}
	// 默认映射
	return anthropicModel
}

// expandEnvVars 展开配置中的环境变量 ${VAR} 或 $VAR
func expandEnvVars(s string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

	return re.ReplaceAllStringFunc(s, func(match string) string {
		var varName string
		if strings.HasPrefix(match, "${") {
			varName = match[2 : len(match)-1]
		} else {
			varName = match[1:]
		}
		return os.Getenv(varName)
	})
}
