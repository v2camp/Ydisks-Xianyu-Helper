// config.go 解析发布前评测所需的运行配置：项目 .env、样本库路径与并发采样参数。
//
// 设计原则：评测配置**只读**项目目录下的 .env，不写回、不修改生产配置。
// 模型密钥优先取 EVAL_* 覆盖项；未配置时由 modelconfig.go 从数据库读当前生效配置。
package evalharness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dotenvFile 是项目根目录下的环境文件默认名。
const dotenvFile = ".env"

// 默认路径与阈值常量。
const (
	// defaultSamplesPath 是缺陷样本库默认位置（由 cmd/evalexport 产出）。
	defaultSamplesPath = "data/evalexport/samples.jsonl"
	// defaultBaselinePath 是基线文件默认位置。
	defaultBaselinePath = "data/evalexport/baseline.json"
	// defaultOutputDir 是评测报告默认输出目录。
	defaultOutputDir = "data/evalexport/replay"
	// defaultSamplesPerCase 是每个样本的默认采样次数。
	defaultSamplesPerCase = 1
	// defaultTolerance 是默认的通过率劣化容差，0.05 表示允许下降 5 个百分点。
	defaultTolerance = 0.05
)

// Config 是一次发布前评测的运行配置。
type Config struct {
	// SamplesPath 是缺陷样本库路径（JSONL）。
	SamplesPath string
	// BaselinePath 是基线文件路径（JSON）。
	BaselinePath string
	// OutputDir 是报告与指标的输出目录。
	OutputDir string
	// SamplesPerCase 是每个样本的采样次数；真实模型非确定性，取多数可抑制噪声。
	SamplesPerCase int
	// Tolerance 是允许的通过率劣化容差。
	Tolerance float64
	// Overrides 是命令行覆盖项。
	Overrides Overrides
}

// Overrides 是命令行传入的覆盖项；零值表示沿用默认或环境配置。
type Overrides struct {
	// SamplesPath 覆盖样本库路径。
	SamplesPath string
	// BaselinePath 覆盖基线路径。
	BaselinePath string
	// OutputDir 覆盖输出目录。
	OutputDir string
	// SamplesPerCase 覆盖采样次数；0 表示不覆盖。
	SamplesPerCase int
	// Tolerance 覆盖容差；负数表示不覆盖。
	Tolerance float64
}

// DefaultConfig 返回除模型配置外的默认运行配置。
func DefaultConfig() *Config {
	return &Config{
		SamplesPath:    defaultSamplesPath,
		BaselinePath:   defaultBaselinePath,
		OutputDir:      defaultOutputDir,
		SamplesPerCase: defaultSamplesPerCase,
		Tolerance:      defaultTolerance,
	}
}

// ApplyOverrides 把命令行覆盖项合并进配置，并校验取值合法性。
func (c *Config) ApplyOverrides(o Overrides) error {
	c.Overrides = o
	if strings.TrimSpace(o.SamplesPath) != "" {
		c.SamplesPath = strings.TrimSpace(o.SamplesPath)
	}
	if strings.TrimSpace(o.BaselinePath) != "" {
		c.BaselinePath = strings.TrimSpace(o.BaselinePath)
	}
	if strings.TrimSpace(o.OutputDir) != "" {
		c.OutputDir = strings.TrimSpace(o.OutputDir)
	}
	if o.SamplesPerCase > 0 {
		c.SamplesPerCase = o.SamplesPerCase
	}
	if o.Tolerance >= 0 {
		c.Tolerance = o.Tolerance
	}
	if c.SamplesPerCase < 1 {
		return errors.New("采样次数必须大于等于 1")
	}
	if c.Tolerance > 1 {
		return errors.New("劣化容差必须位于 0 到 1 之间")
	}
	return nil
}

// LoadDotenv 读取 root 下的 .env 并解析为键值对。
// 解析规则：忽略空行与 # 开头的整行注释；支持可选的 export 前缀；值两侧成对引号会被去掉。
// 不做行内注释剥离，避免把包含 # 的密码截断。文件不存在时返回空映射且不报错。
func LoadDotenv(root string) (map[string]string, error) {
	// path 是环境文件的绝对路径。
	path := filepath.Join(root, dotenvFile)
	// data、err 是文件内容与读取失败原因。
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	// values 是解析出的键值对。
	values := make(map[string]string)
	// line 是当前遍历到的原始行。
	for _, line := range strings.Split(string(data), "\n") {
		// trimmed 是去掉首尾空白后的行内容。
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// body 是去掉可选 export 前缀后的赋值语句。
		body := strings.TrimPrefix(trimmed, "export ")
		// key、value、found 是一次 KEY=VALUE 的拆分结果。
		key, value, found := strings.Cut(body, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = unquote(strings.TrimSpace(value))
	}
	return values, nil
}

// EnvValue 按顺序返回第一个非空的配置值：先环境变量，再 .env 值。
// values 是 .env 解析结果；keys 是候选键名，按优先级排列。
func EnvValue(values map[string]string, keys ...string) string {
	// key 是当前遍历到的候选键名。
	for _, key := range keys {
		// v 是该键在真实环境变量中的取值。
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		// v 是该键在 .env 文件中的取值。
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
	}
	return ""
}

// EnvInt 读取整数配置；缺失或非法时返回 fallback。
func EnvInt(values map[string]string, fallback int, keys ...string) int {
	// raw 是读到的原始配置值。
	raw := EnvValue(values, keys...)
	if raw == "" {
		return fallback
	}
	// parsed、err 是解析结果与失败原因。
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

// EnvFloat 读取浮点配置；缺失或非法时返回 fallback。
func EnvFloat(values map[string]string, fallback float64, keys ...string) float64 {
	// raw 是读到的原始配置值。
	raw := EnvValue(values, keys...)
	if raw == "" {
		return fallback
	}
	// parsed、err 是解析结果与失败原因。
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// unquote 去掉值两侧成对的单引号或双引号。
func unquote(value string) string {
	// quote 是当前尝试匹配的引号字符。
	for _, quote := range []string{`"`, `'`} {
		if len(value) >= 2 && strings.HasPrefix(value, quote) && strings.HasSuffix(value, quote) {
			return value[1 : len(value)-1]
		}
	}
	return value
}
