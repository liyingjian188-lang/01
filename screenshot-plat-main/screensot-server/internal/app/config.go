package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config supports JSON file loading with optional env overrides.
type Config struct {
	Models               []string `json:"models"`
	SiliconflowBaseURL   string   `json:"siliconflow_base_url"`
	SiliconflowAPIKey    string   `json:"siliconflow_api_key"`
	VisionTimeoutSeconds int      `json:"vision_timeout_seconds"`
	VisionMaxTokens      int      `json:"vision_max_tokens"`
	VisionMaxRetries     int      `json:"vision_max_retries"`
	VisionRetryDelayMS   int      `json:"vision_retry_delay_ms"`
	MobileAccessToken    string   `json:"mobile_access_token"`
	TemplatePath         string   `json:"template_path"`
}

func defaultConfig() Config {
	return Config{
		Models:               []string{"Qwen/Qwen3-VL-32B-Instruct"},
		SiliconflowBaseURL:   "https://api.siliconflow.cn",
		VisionTimeoutSeconds: 120,
		VisionMaxTokens:      4096,
		VisionMaxRetries:     2,
		VisionRetryDelayMS:   2000,
		TemplatePath:         "web/result.html",
	}
}

func loadConfigFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	var c Config
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func mergeEnv(c Config) Config {
	if env := strings.TrimSpace(os.Getenv("VISION_MODELS")); env != "" {
		c.Models = splitCSV(env)
	}
	if env := strings.TrimSpace(os.Getenv("SILICONFLOW_BASEURL")); env != "" {
		c.SiliconflowBaseURL = env
	}
	if env := strings.TrimSpace(os.Getenv("VISION_TIMEOUT_SECONDS")); env != "" {
		seconds, err := strconv.Atoi(env)
		if err != nil || seconds <= 0 {
			fmt.Fprintf(os.Stderr, "warn: invalid VISION_TIMEOUT_SECONDS=%q, keep %s\n", env, c.visionTimeout())
		} else {
			c.VisionTimeoutSeconds = seconds
		}
	}
	if env := strings.TrimSpace(os.Getenv("VISION_MAX_TOKENS")); env != "" {
		maxTokens, err := strconv.Atoi(env)
		if err != nil || maxTokens <= 0 {
			fmt.Fprintf(os.Stderr, "warn: invalid VISION_MAX_TOKENS=%q, keep %d\n", env, c.visionMaxTokens())
		} else {
			c.VisionMaxTokens = maxTokens
		}
	}
	if env := strings.TrimSpace(os.Getenv("TEMPLATE_PATH")); env != "" {
		c.TemplatePath = env
	}
	return c
}

func loadConfig() Config {
	path := resolveConfigPath()
	c := defaultConfig()
	if b, err := os.Stat(path); err == nil && !b.IsDir() {
		if fileCfg, err2 := loadConfigFile(path); err2 == nil {
			if len(fileCfg.Models) > 0 {
				c.Models = fileCfg.Models
			}
			if fileCfg.SiliconflowBaseURL != "" {
				c.SiliconflowBaseURL = fileCfg.SiliconflowBaseURL
			}
			if fileCfg.SiliconflowAPIKey != "" {
				c.SiliconflowAPIKey = fileCfg.SiliconflowAPIKey
			}
			if fileCfg.VisionTimeoutSeconds > 0 {
				c.VisionTimeoutSeconds = fileCfg.VisionTimeoutSeconds
			}
			if fileCfg.VisionMaxTokens > 0 {
				c.VisionMaxTokens = fileCfg.VisionMaxTokens
			}
			if fileCfg.VisionMaxRetries > 0 {
				c.VisionMaxRetries = fileCfg.VisionMaxRetries
			}
			if fileCfg.VisionRetryDelayMS > 0 {
				c.VisionRetryDelayMS = fileCfg.VisionRetryDelayMS
			}
			if fileCfg.MobileAccessToken != "" {
				c.MobileAccessToken = fileCfg.MobileAccessToken
			}
			if fileCfg.TemplatePath != "" {
				c.TemplatePath = fileCfg.TemplatePath
			}
		} else {
			fmt.Fprintf(os.Stderr, "warn: read config file failed: %v\n", err2)
		}
	}

	c = mergeEnv(c)
	if !filepath.IsAbs(c.TemplatePath) {
		c.TemplatePath = filepath.Join(filepath.Dir(path), c.TemplatePath)
	}

	masked := c.SiliconflowAPIKey
	if len(masked) > 8 {
		masked = masked[:4] + "***" + masked[len(masked)-3:]
	}
	fmt.Fprintf(
		os.Stderr,
		"using config: %s\nmodels=%v baseURL=%s timeout=%s maxTokens=%d key=%s template=%s\n",
		path,
		c.Models,
		c.SiliconflowBaseURL,
		c.visionTimeout(),
		c.visionMaxTokens(),
		masked,
		c.TemplatePath,
	)
	return c
}

func (c Config) visionMaxTokens() int {
	if c.VisionMaxTokens <= 0 {
		return 4096
	}
	return c.VisionMaxTokens
}

func (c Config) visionTimeout() time.Duration {
	if c.VisionTimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(c.VisionTimeoutSeconds) * time.Second
}

func (c Config) visionRetryDelay() time.Duration {
	if c.VisionRetryDelayMS <= 0 {
		return 2 * time.Second
	}
	return time.Duration(c.VisionRetryDelayMS) * time.Millisecond
}

// resolveConfigPath resolves config path in this order:
// 1. SERVER_CONFIG
// 2. ./config.json
// 3. <exe-dir>/config.json
// 4. ./screensot-server/config.json
func resolveConfigPath() string {
	if p := strings.TrimSpace(os.Getenv("SERVER_CONFIG")); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}

	candidates := []string{"config.json"}
	if exe, err := os.Executable(); err == nil && exe != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.json"))
	}
	candidates = append(candidates, filepath.Join("screensot-server", "config.json"))

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return "config.json"
}
