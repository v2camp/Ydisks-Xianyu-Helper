// modelconfig.go 解析评测所需的模型连接配置。
//
// 配置优先级（前者覆盖后者）：
//  1. .env 或环境变量中的 EVAL_* 显式覆盖项；
//  2. 数据库当前生效配置（.env 的 DATABASE_URL + XIANYU_DATA_KEY 解密读取）。
//
// 敏感约束：接口密钥只用于发起请求，禁止写入报告、日志或任何落盘产物；
// 报告中只出现模型名与收敛后的接口地址。
package evalharness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// 评测采样温度常量。
const (
	// defaultGeneratorTemperature 是被测模型的默认采样温度，与生产
	// internal/engine 的默认温度保持一致，保证评测测的是真实生产行为而非理想化行为。
	defaultGeneratorTemperature float32 = 0.7
	// defaultJudgeTemperature 是判定模型的采样温度，固定为 0 以保证判定端可复现。
	defaultJudgeTemperature float32 = 0.0
	// defaultEvalBaseURL 是只配置了 EVAL_API_KEY 时的兜底接口地址。
	defaultEvalBaseURL = "https://api.deepseek.com/v1"
)

// ModelConfig 是一次模型调用的连接与采样参数。
type ModelConfig struct {
	// BaseURL 是 OpenAI 兼容接口基地址。
	BaseURL string
	// APIKey 是接口密钥；属敏感值，禁止落盘或打印。
	APIKey string
	// Model 是模型标识。
	Model string
	// Temperature 是采样温度。
	Temperature float32
	// Source 是配置来源说明（env 或 database），用于报告与排障，不含密钥。
	Source string
}

// ResolveGeneratorConfig 解析被测模型配置：EVAL_* 优先，缺失时回落数据库当前生效配置。
// values 是 .env 解析结果；store 为 nil 时只能用 EVAL_* 覆盖项。
func ResolveGeneratorConfig(ctx context.Context, store *db.Store, values map[string]string) (ModelConfig, error) {
	// cfg 是逐步填充的被测模型配置。
	cfg := ModelConfig{
		APIKey:      EnvValue(values, "EVAL_API_KEY"),
		BaseURL:     EnvValue(values, "EVAL_BASE_URL"),
		Model:       EnvValue(values, "EVAL_MODEL"),
		Temperature: defaultGeneratorTemperature,
	}
	if cfg.APIKey != "" && cfg.Model != "" {
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultEvalBaseURL
		}
		cfg.Source = "env"
		return cfg, nil
	}
	if strings.TrimSpace(cfg.APIKey) != "" || strings.TrimSpace(cfg.Model) != "" {
		return ModelConfig{}, errors.New("EVAL_API_KEY 与 EVAL_MODEL 必须同时配置，或同时留空以回落数据库配置")
	}
	if store == nil {
		return ModelConfig{}, errors.New("未配置 EVAL_API_KEY/EVAL_MODEL，且没有可用的数据库连接")
	}

	// dbCfg 是从数据库读到的当前生效配置。
	dbCfg, err := readModelConfigFromDB(ctx, store)
	if err != nil {
		return ModelConfig{}, err
	}
	dbCfg.Temperature = defaultGeneratorTemperature
	return dbCfg, nil
}

// ResolveJudgeConfig 解析判定模型配置：EVAL_JUDGE_* 优先，缺失时复用被测配置。
// generator 是已解析的被测配置，作为判定端缺省值；判定端温度强制为 0。
func ResolveJudgeConfig(values map[string]string, generator ModelConfig) ModelConfig {
	// judge 是逐步填充的判定配置，默认复用被测端点。
	judge := ModelConfig{
		BaseURL:     EnvValue(values, "EVAL_JUDGE_BASE_URL"),
		APIKey:      EnvValue(values, "EVAL_JUDGE_API_KEY"),
		Model:       EnvValue(values, "EVAL_JUDGE_MODEL"),
		Temperature: defaultJudgeTemperature,
		Source:      "generator",
	}
	if judge.BaseURL == "" {
		judge.BaseURL = generator.BaseURL
	}
	if judge.APIKey == "" {
		judge.APIKey = generator.APIKey
	}
	if judge.Model == "" {
		judge.Model = generator.Model
	}
	if EnvValue(values, "EVAL_JUDGE_MODEL") != "" {
		judge.Source = "env"
	}
	return judge
}

// readModelConfigFromDB 读取数据库当前生效的模型配置。
// 模型与密钥是全局设置，但敏感读取需要账号归属校验，因此取任意一个已启用 AI 的账号作为入口。
func readModelConfigFromDB(ctx context.Context, store *db.Store) (ModelConfig, error) {
	// cookieID 是用于敏感设置归属校验的账号标识。
	cookieID, err := firstAIEnabledAccount(ctx, store)
	if err != nil {
		return ModelConfig{}, err
	}
	// apiKey、err 是解密后的接口密钥；仅用于发起请求，禁止落盘。
	apiKey, err := store.ReadSensitiveSettingForAccount(ctx, cookieID, "ai_api_key", "settings.use", "ai_reply")
	if err != nil {
		return ModelConfig{}, fmt.Errorf("读取数据库模型密钥失败: %w", err)
	}
	if strings.TrimSpace(apiKey) == "" {
		return ModelConfig{}, errors.New("数据库未配置模型密钥（ai_api_key），请在系统设置中配置或改用 EVAL_API_KEY")
	}
	// baseURL、readErr 是数据库中的接口地址与本次读取的失败原因。
	baseURL, readErr := store.Settings.Get(ctx, "ai_api_url")
	if readErr != nil {
		return ModelConfig{}, fmt.Errorf("读取数据库模型地址失败: %w", readErr)
	}
	// model 是数据库中的模型名；readErr 复用为本次读取的失败原因。
	model, readErr := store.Settings.Get(ctx, "ai_model")
	if readErr != nil {
		return ModelConfig{}, fmt.Errorf("读取数据库模型名失败: %w", readErr)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultEvalBaseURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelConfig{}, errors.New("数据库未配置模型名（ai_model）")
	}
	return ModelConfig{
		BaseURL: baseURL,
		APIKey:  strings.TrimSpace(apiKey),
		Model:   model,
		Source:  "database",
	}, nil
}

// firstAIEnabledAccount 取任意一个已启用 AI 回复的账号标识。
// 该查询只读取账号主键，不涉及任何凭证解密。
func firstAIEnabledAccount(ctx context.Context, store *db.Store) (string, error) {
	// query 取启用 AI 的账号，顺序固定便于复现。
	query := `SELECT cookie_id FROM ai_reply_settings WHERE ai_enabled = 1 ORDER BY cookie_id LIMIT 1`
	// cookieID 是查询结果。
	var cookieID string
	// err 是查询失败原因；sql.ErrNoRows 单独提示，避免与连接错误混淆。
	err := store.DB.QueryRowContext(ctx, query).Scan(&cookieID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("数据库中没有启用 AI 回复的账号，无法读取模型配置")
	}
	if err != nil {
		return "", fmt.Errorf("查询启用 AI 的账号失败: %w", err)
	}
	return cookieID, nil
}
