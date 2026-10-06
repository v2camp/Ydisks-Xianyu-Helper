package runtime

// mcp_card_adapter.go 把卡券应用服务投影为 internal/mcp 的 CardPorts。
// API 连通性测试复用组合层已装配的受控公网 HTTP 请求器。

import (
	"context"

	cardsapp "xianyu-go/internal/application/cards"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/mcp"
)

// mcpCardPorts 聚合卡券应用服务与 API 测试端口。
type mcpCardPorts struct {
	// service 是卡券 CRUD 与库存用例服务。
	service *cardsapp.Service
	// tester 是 API 卡券受控连通性测试端口。
	tester cardsapp.APIRequestTester
}

// 编译期断言适配器满足 MCP 卡券端口。
var _ mcp.CardPorts = (*mcpCardPorts)(nil)

// ListCards 透传卡券组列表用例。
func (a *mcpCardPorts) ListCards(ctx context.Context, userID int64) ([]cardsapp.Card, error) {
	return a.service.List(ctx, userID)
}

// GetCard 透传单卡券组读取用例。
func (a *mcpCardPorts) GetCard(ctx context.Context, userID, cardID int64) (cardsapp.Card, error) {
	return a.service.Get(ctx, userID, cardID)
}

// CreateCard 透传卡券组创建用例。
func (a *mcpCardPorts) CreateCard(ctx context.Context, userID int64, draft cardsapp.Draft) (int64, error) {
	return a.service.Create(ctx, userID, draft)
}

// UpdateCard 透传卡券组更新用例。
func (a *mcpCardPorts) UpdateCard(ctx context.Context, userID, cardID int64, draft cardsapp.Draft) error {
	return a.service.Update(ctx, userID, cardID, draft)
}

// DeleteCard 透传卡券组删除用例。
func (a *mcpCardPorts) DeleteCard(ctx context.Context, userID, cardID int64) error {
	return a.service.Delete(ctx, userID, cardID)
}

// AppendCardData 透传卡密追加用例。
func (a *mcpCardPorts) AppendCardData(ctx context.Context, userID, cardID int64, content string) (int, error) {
	return a.service.AppendData(ctx, userID, cardID, content)
}

// TestCardAPI 透传已保存 API 卡券组的连通性测试用例；完整请求模板由应用层读取，不经过 MCP 层。
func (a *mcpCardPorts) TestCardAPI(ctx context.Context, userID, cardID int64) (cardsapp.APIRequestTestResult, error) {
	return a.service.TestSavedAPI(ctx, userID, cardID, a.tester)
}

// newMCPCardPorts 构造 MCP 卡券端口；服务缺失时返回 nil。
func newMCPCardPorts(ports composition.TransportPorts) *mcpCardPorts {
	if ports.Cards == nil {
		return nil
	}
	return &mcpCardPorts{service: ports.Cards, tester: ports.APICardTester}
}
