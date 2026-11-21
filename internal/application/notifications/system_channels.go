package notifications

import (
	"context"
	"fmt"
	"time"
)

// SystemAlertChannelRepository 定义系统告警渠道用例所需的最小持久化端口。
// 系统渠道由管理员共享，不使用租户渠道的 userID 归属语义。
type SystemAlertChannelRepository interface {
	// ListSystemChannels 查询全部系统告警渠道摘要。
	ListSystemChannels(ctx context.Context) ([]ChannelSummary, error)
	// GetSystemChannel 查询单条系统告警渠道完整记录。
	GetSystemChannel(ctx context.Context, channelID int64) (*ChannelRecord, error)
	// CreateSystemChannel 创建系统告警渠道；adminID 记录创建者用于审计。
	CreateSystemChannel(ctx context.Context, adminID int64, input ChannelInput) (int64, error)
	// UpdateSystemChannel 更新系统告警渠道。
	UpdateSystemChannel(ctx context.Context, adminID int64, record ChannelRecord) error
	// DeleteSystemChannel 删除系统告警渠道。
	DeleteSystemChannel(ctx context.Context, channelID int64) error
}

// SystemAlertChannelService 编排管理员系统告警渠道的 CRUD 与测试发送。
type SystemAlertChannelService struct {
	// repository 保存系统渠道的最小持久化端口。
	repository SystemAlertChannelRepository
	// sender 保存测试通知发送端口；普通 CRUD 不依赖该端口。
	sender ChannelSender
}

// NewSystemAlertChannelService 构造系统告警渠道应用服务。
func NewSystemAlertChannelService(repository SystemAlertChannelRepository, sender ChannelSender) *SystemAlertChannelService {
	return &SystemAlertChannelService{repository: repository, sender: sender}
}

// List 查询全部系统告警渠道摘要，不读取渠道配置。
func (s *SystemAlertChannelService) List(ctx context.Context) ([]ChannelSummary, error) {
	if s == nil || s.repository == nil {
		return nil, ErrChannelInvalidInput
	}
	return s.repository.ListSystemChannels(ctx)
}

// GetEditor 返回系统告警渠道的非敏感编辑器视图。
func (s *SystemAlertChannelService) GetEditor(ctx context.Context, channelID int64) (ChannelEditor, error) {
	if s == nil || s.repository == nil || channelID <= 0 {
		return ChannelEditor{}, ErrChannelInvalidInput
	}
	// record、lookupErr 保存系统渠道完整记录与查询错误。
	record, lookupErr := s.repository.GetSystemChannel(ctx, channelID)
	if lookupErr != nil {
		return ChannelEditor{}, lookupErr
	}
	if record == nil {
		return ChannelEditor{}, ErrChannelNotFound
	}
	return channelEditorFromRecord(record), nil
}

// Create 校验并创建系统告警渠道；adminID 是当前管理员，用于创建者审计。
func (s *SystemAlertChannelService) Create(ctx context.Context, adminID int64, input ChannelInput) (int64, error) {
	if s == nil || s.repository == nil || adminID <= 0 {
		return 0, ErrChannelInvalidInput
	}
	// validationErr 表示渠道名称与协议类型校验结果。
	if validationErr := validateChannelFields(input.Name, input.Type); validationErr != nil {
		return 0, validationErr
	}
	return s.repository.CreateSystemChannel(ctx, adminID, input)
}

// Update 合并部分更新、校验最终字段并保存系统告警渠道。
func (s *SystemAlertChannelService) Update(ctx context.Context, adminID, channelID int64, patch ChannelPatch) error {
	if s == nil || s.repository == nil || adminID <= 0 || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	// record、lookupErr 保存系统渠道完整记录与查询错误。
	record, lookupErr := s.repository.GetSystemChannel(ctx, channelID)
	if lookupErr != nil {
		return lookupErr
	}
	if record == nil {
		return ErrChannelNotFound
	}
	// patchErr 表示合并后的渠道字段校验结果。
	if patchErr := applyChannelPatch(record, patch); patchErr != nil {
		return patchErr
	}
	return s.repository.UpdateSystemChannel(ctx, adminID, *record)
}

// Delete 删除系统告警渠道；不存在时返回统一未找到错误。
func (s *SystemAlertChannelService) Delete(ctx context.Context, channelID int64) error {
	if s == nil || s.repository == nil || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	return s.repository.DeleteSystemChannel(ctx, channelID)
}

// Test 向指定系统告警渠道发送固定格式测试通知；正文不包含渠道配置或凭证。
func (s *SystemAlertChannelService) Test(ctx context.Context, channelID int64, now time.Time) error {
	if s == nil || s.repository == nil || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	if s.sender == nil {
		return ErrNotifierUnavailable
	}
	// record、lookupErr 保存系统渠道记录与查询错误；不存在时返回未找到而不是发送失败。
	record, lookupErr := s.repository.GetSystemChannel(ctx, channelID)
	if lookupErr != nil {
		return lookupErr
	}
	if record == nil {
		return ErrChannelNotFound
	}
	if now.IsZero() {
		now = time.Now()
	}
	// body 是固定测试通知正文，不包含渠道敏感配置。
	body := fmt.Sprintf("🧪 系统告警渠道测试\n\n这是一条来自闲鱼助手的系统告警测试通知，收到说明渠道配置正常。\n时间: %s", now.Format("2006-01-02 15:04:05"))
	return s.sender.SendToChannel(channelID, body)
}
