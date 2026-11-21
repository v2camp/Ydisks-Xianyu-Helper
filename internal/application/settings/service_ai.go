package settings

import (
	"context"
	"errors"
	"strings"
)

// ListAIModels 读取远端模型目录，API 密钥使用前必须经过受控读取或审计。
func (s *Service) ListAIModels(ctx context.Context, userID int64, baseURL, apiKey string) ([]string, error) {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	if s.modelClient == nil {
		return nil, errors.New("AI 模型客户端未初始化")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		// err 表示当前用户 AI 地址读取失败。
		var err error
		baseURL, err = s.repository.GetUser(ctx, userID, "ai_api_url")
		if err != nil {
			return nil, err
		}
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		// err 表示当前用户敏感 API 密钥读取或审计失败。
		var err error
		apiKey, err = s.repository.ReadSensitiveUser(ctx, userID, "ai_api_key", "settings.use", "ai_models")
		if err != nil {
			return nil, err
		}
	} else if // err 表示外部传入 API 密钥的使用审计失败。
	err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.use", Resource: "ai_models", Keys: []string{"ai_api_key"}}); err != nil {
		return nil, err
	}
	return s.modelClient.Fetch(ctx, baseURL, apiKey)
}

// TestAIConnection 发送一次最小对话请求验证 API 地址、密钥和模型的组合。
// baseURL 和 apiKey 为空时回退到系统设置，与 ListAIModels 的解析逻辑一致。
func (s *Service) TestAIConnection(ctx context.Context, userID int64, baseURL, apiKey, model string) (AIConnectionTestResult, error) {
	// err 表示服务依赖或当前用户身份校验失败。
	if err := s.validateUser(userID); err != nil {
		return AIConnectionTestResult{}, err
	}
	if s.modelClient == nil {
		return AIConnectionTestResult{}, errors.New("AI 模型客户端未初始化")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		// lookupErr 表示读取当前用户已保存 AI 服务地址时产生的存储错误。
		var lookupErr error
		baseURL, lookupErr = s.repository.GetUser(ctx, userID, "ai_api_url")
		if lookupErr != nil {
			return AIConnectionTestResult{}, lookupErr
		}
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		// secretErr 表示受控读取当前用户已保存 API Key 或写入相应审计时产生的错误。
		var secretErr error
		apiKey, secretErr = s.repository.ReadSensitiveUser(ctx, userID, "ai_api_key", "settings.use", "ai_connection_test")
		if secretErr != nil {
			return AIConnectionTestResult{}, secretErr
		}
	} else if // auditErr 表示临时 API Key 使用审计未持久化，必须阻止出站请求。
	auditErr := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.use", Resource: "ai_connection_test", Keys: []string{"ai_api_key"}}); auditErr != nil {
		return AIConnectionTestResult{}, auditErr
	}
	model = strings.TrimSpace(model)
	if model == "" {
		// lookupErr 表示读取当前用户默认模型名称时产生的存储错误。
		var lookupErr error
		model, lookupErr = s.repository.GetUser(ctx, userID, "ai_model")
		if lookupErr != nil {
			return AIConnectionTestResult{}, lookupErr
		}
	}
	return s.modelClient.TestConnection(ctx, baseURL, apiKey, model)
}
