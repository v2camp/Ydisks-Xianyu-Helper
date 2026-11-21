package notifications

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// systemChannelRepositoryStub 为系统告警渠道用例提供可控的 CRUD 结果。
type systemChannelRepositoryStub struct {
	// summaries 是系统渠道列表返回值。
	summaries []ChannelSummary
	// record 是单条系统渠道记录；nil 表示不存在。
	record *ChannelRecord
	// createdID、createdAdminID、createdInput 保存创建结果与参数。
	createdID int64
	// createdAdminID 记录创建者参数。
	createdAdminID int64
	// createdInput 记录创建输入。
	createdInput ChannelInput
	// createErr 是创建失败原因。
	createErr error
	// updatedRecord 保存最近一次更新的系统渠道记录。
	updatedRecord ChannelRecord
	// updateAdminID 记录更新调用传入的管理员。
	updateAdminID int64
	// updateErr 是更新失败原因。
	updateErr error
	// deletedID 记录删除的渠道主键。
	deletedID int64
	// deleteErr 是删除失败原因。
	deleteErr error
}

// ListSystemChannels 返回预置系统渠道摘要。
func (r *systemChannelRepositoryStub) ListSystemChannels(context.Context) ([]ChannelSummary, error) {
	return r.summaries, nil
}

// GetSystemChannel 返回预置系统渠道记录。
func (r *systemChannelRepositoryStub) GetSystemChannel(context.Context, int64) (*ChannelRecord, error) {
	return r.record, nil
}

// CreateSystemChannel 记录创建参数并返回预置结果。
func (r *systemChannelRepositoryStub) CreateSystemChannel(_ context.Context, adminID int64, input ChannelInput) (int64, error) {
	r.createdAdminID, r.createdInput = adminID, input
	return r.createdID, r.createErr
}

// UpdateSystemChannel 记录更新参数并返回预置错误。
func (r *systemChannelRepositoryStub) UpdateSystemChannel(_ context.Context, adminID int64, record ChannelRecord) error {
	r.updateAdminID, r.updatedRecord = adminID, record
	return r.updateErr
}

// DeleteSystemChannel 记录删除参数并返回预置错误。
func (r *systemChannelRepositoryStub) DeleteSystemChannel(_ context.Context, channelID int64) error {
	r.deletedID = channelID
	return r.deleteErr
}

// TestSystemAlertChannelServiceCRUD 验证系统渠道列表、编辑器、创建、更新、删除与测试发送。
func TestSystemAlertChannelServiceCRUD(t *testing.T) {
	// repository 是本次测试使用的系统渠道仓储替身。
	repository := &systemChannelRepositoryStub{
		summaries: []ChannelSummary{{ID: 3, Name: "系统渠道", Type: "webhook", Enabled: true}},
		record:    &ChannelRecord{ID: 3, Name: "系统渠道", Type: "webhook", Config: `{"webhook_url":"https://secret","http_method":"POST"}`, EventTypes: "business_silence", Enabled: true},
		createdID: 9,
	}
	// sender 是记录测试发送正文的替身。
	sender := &channelSenderStub{}
	// service 是待验证的系统告警渠道应用服务。
	service := NewSystemAlertChannelService(repository, sender)
	// summaries、listErr 保存系统渠道列表结果。
	summaries, listErr := service.List(context.Background())
	if listErr != nil || len(summaries) != 1 || summaries[0].ID != 3 {
		t.Fatalf("list summaries=%+v err=%v", summaries, listErr)
	}
	// editor、editorErr 保存编辑器视图；敏感 webhook_url 的值不得回显，只能回显键名。
	editor, editorErr := service.GetEditor(context.Background(), 3)
	if editorErr != nil || editor.Config["http_method"] != "POST" || editor.Config["webhook_url"] != "" {
		t.Fatalf("editor=%+v err=%v", editor, editorErr)
	}
	// savedCredential 表示敏感键名是否进入存在性列表。
	savedCredential := false
	// key 是当前待匹配的已保存凭据键名。
	for _, key := range editor.SavedCredentialKeys {
		if key == "webhook_url" {
			savedCredential = true
		}
	}
	if !savedCredential {
		t.Fatalf("保存凭据键缺失: %+v", editor.SavedCredentialKeys)
	}
	// invalidCreateErr 验证空名称被拒绝。
	if _, invalidCreateErr := service.Create(context.Background(), 7, ChannelInput{Type: "webhook"}); !errors.Is(invalidCreateErr, ErrChannelInvalidInput) {
		t.Fatalf("invalid create err=%v", invalidCreateErr)
	}
	// createdID、createErr 保存有效创建结果。
	createdID, createErr := service.Create(context.Background(), 7, ChannelInput{Name: "新系统渠道", Type: "webhook", Config: `{}`, Enabled: true})
	if createErr != nil || createdID != 9 || repository.createdAdminID != 7 {
		t.Fatalf("created id=%d admin=%d err=%v", createdID, repository.createdAdminID, createErr)
	}
	// nextMethod 是部分更新提交的新配置值。
	nextMethod := `{"http_method":"PUT"}`
	// updateErr 表示系统渠道部分更新结果。
	updateErr := service.Update(context.Background(), 7, 3, ChannelPatch{Config: &nextMethod})
	if updateErr != nil || repository.updateAdminID != 7 {
		t.Fatalf("update admin=%d err=%v", repository.updateAdminID, updateErr)
	}
	// updatedConfig 保存按键合并后的配置；未提交的 webhook_url 必须保留。
	updatedConfig := repository.updatedRecord.Config
	if !strings.Contains(updatedConfig, `"webhook_url":"https://secret"`) || !strings.Contains(updatedConfig, `"http_method":"PUT"`) {
		t.Fatalf("merged config=%s", updatedConfig)
	}
	// deleteErr 表示系统渠道删除结果。
	if deleteErr := service.Delete(context.Background(), 3); deleteErr != nil || repository.deletedID != 3 {
		t.Fatalf("delete id=%d err=%v", repository.deletedID, deleteErr)
	}
	// testErr 表示系统渠道测试发送结果；正文必须标注系统告警。
	if testErr := service.Test(context.Background(), 3, time.Now()); testErr != nil || !strings.Contains(sender.body, "系统告警") {
		t.Fatalf("test body=%q err=%v", sender.body, testErr)
	}
}

// TestSystemAlertChannelServiceMissingAndInvalid 验证不存在渠道、缺失发送器与非法参数。
func TestSystemAlertChannelServiceMissingAndInvalid(t *testing.T) {
	// service 是没有记录、没有发送器的系统渠道服务。
	service := NewSystemAlertChannelService(&systemChannelRepositoryStub{}, nil)
	// editorErr 验证不存在渠道返回统一未找到错误。
	if _, editorErr := service.GetEditor(context.Background(), 3); !errors.Is(editorErr, ErrChannelNotFound) {
		t.Fatalf("missing editor err=%v", editorErr)
	}
	// updateErr 验证不存在渠道的更新返回统一未找到错误。
	if updateErr := service.Update(context.Background(), 7, 3, ChannelPatch{}); !errors.Is(updateErr, ErrChannelNotFound) {
		t.Fatalf("missing update err=%v", updateErr)
	}
	// unavailableErr 验证发送器缺失时测试发送返回未启用错误。
	if unavailableErr := service.Test(context.Background(), 3, time.Now()); !errors.Is(unavailableErr, ErrNotifierUnavailable) {
		t.Fatalf("missing sender err=%v", unavailableErr)
	}
	// invalidCases 保存所有应返回参数错误的调用。
	if _, listErr := NewSystemAlertChannelService(nil, nil).List(context.Background()); !errors.Is(listErr, ErrChannelInvalidInput) {
		t.Fatalf("nil list err=%v", listErr)
	}
	// createErr 是零管理员创建参数错误。
	if _, createErr := service.Create(context.Background(), 0, ChannelInput{Name: "渠道", Type: "webhook"}); !errors.Is(createErr, ErrChannelInvalidInput) {
		t.Fatalf("zero admin create err=%v", createErr)
	}
	// deleteErr 是零渠道删除参数错误。
	if deleteErr := service.Delete(context.Background(), 0); !errors.Is(deleteErr, ErrChannelInvalidInput) {
		t.Fatalf("zero delete err=%v", deleteErr)
	}
}
