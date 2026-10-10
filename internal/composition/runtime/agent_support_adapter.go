// agent_support_adapter.go 把客服 Agent 配置应用服务投影为 HTTP transport 消费的端口。
//
// 适配器只做透传：档位合法性、账号归属校验与读写失败策略都由应用用例决定，HTTP 层不再
// 重复判断一次。

package runtime

import (
	"context"

	agentadminapp "xianyu-go/internal/application/agentadmin"
	"xianyu-go/internal/server"
)

// agentSupportPort 把客服 Agent 配置应用服务投影为 HTTP transport 端口。
type agentSupportPort struct {
	// service 是完成构造的客服 Agent 配置应用服务，运行期不替换。
	service *agentadminapp.Service
}

// 编译期断言适配器满足客服 Agent 配置端口。
var _ server.AgentSupportPort = (*agentSupportPort)(nil)

// TenantSettings 透传租户级默认配置读取用例。
func (p *agentSupportPort) TenantSettings(ctx context.Context, userID int64) (agentadminapp.TenantConfig, error) {
	return p.service.TenantConfig(ctx, userID)
}

// UpdateTenantSettings 透传租户级默认配置保存用例。
func (p *agentSupportPort) UpdateTenantSettings(ctx context.Context, userID int64, config agentadminapp.TenantConfig) error {
	return p.service.UpdateTenantConfig(ctx, userID, config)
}

// AccountSettings 透传账号级生效配置读取用例。
func (p *agentSupportPort) AccountSettings(ctx context.Context, userID int64, cookieID string) (agentadminapp.EffectiveConfig, error) {
	return p.service.AccountConfig(ctx, userID, cookieID)
}

// UpdateAccountOverride 透传账号级覆盖保存用例。
func (p *agentSupportPort) UpdateAccountOverride(ctx context.Context, userID int64, cookieID string, override agentadminapp.AccountOverride) error {
	return p.service.UpdateAccountOverride(ctx, userID, cookieID, override)
}

// newAgentSupportPort 构造客服 Agent 配置端口。
//
// 返回接口类型而非具体指针：应用服务缺失时返回未包装的 nil，使 Server 的启动期完整性
// 校验能识别出缺失依赖，而不是拿到一个「非空接口、空实现」的替身。
func newAgentSupportPort(service *agentadminapp.Service) server.AgentSupportPort {
	if service == nil {
		return nil
	}
	return &agentSupportPort{service: service}
}
