package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// channelRepositoryStub 是通知渠道应用服务测试使用的可控端口替身。
type channelRepositoryStub struct {
	// summaries 保存列表查询返回的非敏感渠道摘要。
	summaries []ChannelSummary
	// record 保存更新查询返回的渠道记录。
	record *ChannelRecord
	// bindings 保存绑定列表查询结果。
	bindings []BindingSummary
	// bindingIDs 保存账号启用的渠道 ID。
	bindingIDs []int64
	// ownedChannel 表示渠道归属校验结果。
	ownedChannel bool
	// channelErr 保存渠道归属查询专用错误。
	channelErr error
	// ownedAccount 表示账号归属校验结果。
	ownedAccount bool
	// err 保存端口操作失败原因。
	err error
	// createdInput 保存最近一次创建输入，便于验证敏感值只进入端口。
	createdInput ChannelInput
	// updatedRecord 保存最近一次更新记录，便于验证部分更新合并。
	updatedRecord *ChannelRecord
	// sentBindings 保存最近一次覆盖绑定请求。
	sentBindings []int64
}

// ListChannels 返回预置渠道摘要。
func (r *channelRepositoryStub) ListChannels(context.Context, int64) ([]ChannelSummary, error) {
	return r.summaries, r.err
}

// GetChannelForUpdate 返回预置渠道完整记录。
func (r *channelRepositoryStub) GetChannelForUpdate(context.Context, int64, int64) (*ChannelRecord, error) {
	return r.record, r.err
}

// CreateChannel 记录创建输入并返回固定渠道 ID。
func (r *channelRepositoryStub) CreateChannel(_ context.Context, _ int64, input ChannelInput) (int64, error) {
	r.createdInput = input
	if r.err != nil {
		return 0, r.err
	}
	return 41, nil
}

// UpdateChannel 记录更新后的完整渠道记录。
func (r *channelRepositoryStub) UpdateChannel(_ context.Context, _ int64, record ChannelRecord) error {
	r.updatedRecord = &record
	return r.err
}

// DeleteChannel 返回预置端口错误。
func (r *channelRepositoryStub) DeleteChannel(context.Context, int64, int64) error { return r.err }

// OwnsChannel 返回预置渠道归属结果。
func (r *channelRepositoryStub) OwnsChannel(context.Context, int64, int64) (bool, error) {
	if r.channelErr != nil {
		return false, r.channelErr
	}
	return r.ownedChannel, r.err
}

// OwnsAccount 返回预置账号归属结果。
func (r *channelRepositoryStub) OwnsAccount(context.Context, int64, string) (bool, error) {
	return r.ownedAccount, r.err
}

// ListBindings 返回预置绑定摘要。
func (r *channelRepositoryStub) ListBindings(context.Context, int64) ([]BindingSummary, error) {
	return r.bindings, r.err
}

// GetBindingIDs 返回预置账号绑定 ID。
func (r *channelRepositoryStub) GetBindingIDs(context.Context, string) ([]int64, error) {
	return r.bindingIDs, r.err
}

// SetBindings 记录覆盖绑定请求。
func (r *channelRepositoryStub) SetBindings(_ context.Context, _ string, channelIDs []int64) error {
	r.sentBindings = append([]int64(nil), channelIDs...)
	return r.err
}

// SetSingleBinding 返回预置端口错误。
func (r *channelRepositoryStub) SetSingleBinding(context.Context, string, int64, bool) error {
	return r.err
}

// DeleteBinding 返回预置端口错误。
func (r *channelRepositoryStub) DeleteBinding(context.Context, int64, int64) error { return r.err }

// DeleteAccountBindings 返回预置端口错误。
func (r *channelRepositoryStub) DeleteAccountBindings(context.Context, int64, string) error {
	return r.err
}

// channelSenderStub 是测试发送端口的替身。
type channelSenderStub struct {
	// body 保存最近一次发送的正文。
	body string
	// err 保存发送失败原因。
	err error
}

// SendToChannel 记录发送正文并返回预置错误。
func (s *channelSenderStub) SendToChannel(_ int64, body string) error {
	s.body = body
	return s.err
}

// TestChannelServiceCreatesAndUpdatesWithoutReturningConfig 验证渠道创建更新成功且配置不进入展示模型。
func TestChannelServiceCreatesAndUpdatesWithoutReturningConfig(t *testing.T) {
	// repository 是保存敏感配置但不把它放入摘要的端口替身。
	repository := &channelRepositoryStub{summaries: []ChannelSummary{{ID: 1, Name: "邮件", Type: "email"}}, record: &ChannelRecord{ID: 1, Name: "旧名", Type: "email", Config: `{"password":"secret"}`, Enabled: true}}
	// service 是待验证的通知渠道应用服务。
	service := NewChannelService(repository, nil)
	// channelID、createErr 保存创建结果。
	channelID, createErr := service.CreateChannel(context.Background(), 7, ChannelInput{Name: "邮件", Type: "email", Config: `{"password":"secret"}`})
	if createErr != nil || channelID != 41 || repository.createdInput.Config == "" {
		t.Fatalf("创建结果异常: id=%d err=%v", channelID, createErr)
	}
	// name 保存部分更新后的渠道名称。
	name := "新名"
	// updateErr 保存更新结果。
	updateErr := service.UpdateChannel(context.Background(), 7, 1, ChannelPatch{Name: &name})
	if updateErr != nil || repository.updatedRecord == nil || repository.updatedRecord.Name != "新名" || repository.updatedRecord.Config != `{"password":"secret"}` {
		t.Fatalf("更新结果异常: record=%+v err=%v", repository.updatedRecord, updateErr)
	}
	// summaries、listErr 保存非敏感渠道列表结果。
	summaries, listErr := service.ListChannels(context.Background(), 7)
	if listErr != nil || len(summaries) != 1 || summaries[0].Name != "邮件" {
		t.Fatalf("列表结果异常: summaries=%+v err=%v", summaries, listErr)
	}
}

// TestChannelServiceEditorViewKeepsRecipientButDropsSecrets 验证编辑视图保留收件地址且不返回 SMTP 密码或其他配置字段。
func TestChannelServiceEditorViewKeepsRecipientButDropsSecrets(t *testing.T) {
	// repository 是返回解密内部记录的渠道端口替身。
	repository := &channelRepositoryStub{record: &ChannelRecord{
		ID: 9, Name: "邮件", Type: "email", EventTypes: "[]", Enabled: true,
		Config: `{"to_email":" receiver@example.com ","use_custom_smtp":false,"smtp_password":"secret","smtp_server":"smtp.example.com"}`,
	}}
	// service 是待验证的通知渠道应用服务。
	service := NewChannelService(repository, nil)
	// editor、editorErr 保存脱敏编辑视图及读取错误。
	editor, editorErr := service.GetChannelEditor(context.Background(), 7, 9)
	if editorErr != nil {
		t.Fatalf("读取编辑视图失败: %v", editorErr)
	}
	if editor.ToEmail != "receiver@example.com" || editor.UseCustomSMTP {
		t.Fatalf("编辑视图字段异常: %+v", editor)
	}
}

// TestChannelServiceRejectsOwnershipAndStorageFailures 验证归属失败和存储错误不会继续写入。
func TestChannelServiceRejectsOwnershipAndStorageFailures(t *testing.T) {
	// forbiddenRepository 是拒绝渠道归属的端口替身。
	forbiddenRepository := &channelRepositoryStub{ownedChannel: false, ownedAccount: false}
	// forbiddenService 是使用拒绝归属端口的应用服务。
	forbiddenService := NewChannelService(forbiddenRepository, &channelSenderStub{})
	// forbiddenErr 保存测试发送的归属错误。
	forbiddenErr := forbiddenService.TestChannel(context.Background(), 7, 1, time.Unix(0, 0))
	if !errors.Is(forbiddenErr, ErrChannelForbidden) {
		t.Fatalf("渠道归属失败错误异常: %v", forbiddenErr)
	}
	// bindingErr 保存账号绑定的归属错误。
	bindingErr := forbiddenService.SetBindings(context.Background(), 7, "acc", []int64{1})
	if !errors.Is(bindingErr, ErrAccountForbidden) {
		t.Fatalf("账号归属失败错误异常: %v", bindingErr)
	}
	// storageError 是持久化端口失败原因。
	storageError := errors.New("storage failed")
	// storageRepository 是返回存储错误的端口替身。
	storageRepository := &channelRepositoryStub{ownedChannel: true, ownedAccount: true, err: storageError, record: &ChannelRecord{Name: "x", Type: "webhook"}}
	// storageService 是使用存储错误端口的应用服务。
	storageService := NewChannelService(storageRepository, &channelSenderStub{})
	// updateErr 保存存储错误传播结果。
	updateErr := storageService.UpdateChannel(context.Background(), 7, 1, ChannelPatch{})
	if !errors.Is(updateErr, storageError) {
		t.Fatalf("存储错误未透传: %v", updateErr)
	}
}

// TestChannelServiceOwnsChannelKeepsRepositoryBoundary 验证渠道归属查询只透传非敏感结论和存储错误。
func TestChannelServiceOwnsChannelKeepsRepositoryBoundary(t *testing.T) {
	// cases 描述成功、越权、存储失败和非法输入四类归属结果。
	cases := []struct {
		name       string
		repository *channelRepositoryStub
		userID     int64
		channelID  int64
		want       bool
		wantErr    error
	}{
		{name: "owned", repository: &channelRepositoryStub{ownedChannel: true}, userID: 7, channelID: 1, want: true},
		{name: "not owned", repository: &channelRepositoryStub{ownedChannel: false}, userID: 7, channelID: 1},
		{name: "storage error", repository: &channelRepositoryStub{err: errors.New("storage failed")}, userID: 7, channelID: 1, wantErr: errors.New("storage failed")},
		{name: "invalid user", repository: &channelRepositoryStub{ownedChannel: true}, userID: 0, channelID: 1, wantErr: ErrChannelInvalidInput},
	}
	// testCase 表示当前验证的渠道归属边界场景。
	for _, testCase := range cases {
		// service 是使用当前场景持久化替身的通知渠道服务。
		service := NewChannelService(testCase.repository, nil)
		// exists、ownershipErr 保存应用层归属结果。
		exists, ownershipErr := service.OwnsChannel(context.Background(), testCase.userID, testCase.channelID)
		if exists != testCase.want {
			t.Fatalf("%s: 归属结果=%v，期望=%v", testCase.name, exists, testCase.want)
		}
		if testCase.wantErr != nil {
			if ownershipErr == nil || ownershipErr.Error() != testCase.wantErr.Error() {
				t.Fatalf("%s: 错误=%v，期望=%v", testCase.name, ownershipErr, testCase.wantErr)
			}
			continue
		}
		if ownershipErr != nil {
			t.Fatalf("%s: 意外错误=%v", testCase.name, ownershipErr)
		}
	}
}

// TestChannelServiceTestSendUsesSafeBody 验证测试发送只生成固定正文，不携带渠道配置。
func TestChannelServiceTestSendUsesSafeBody(t *testing.T) {
	// repository 是允许渠道归属且不返回敏感配置的端口替身。
	repository := &channelRepositoryStub{ownedChannel: true}
	// sender 是记录测试正文的通知发送替身。
	sender := &channelSenderStub{}
	// service 是待验证的测试发送应用服务。
	service := NewChannelService(repository, sender)
	// sendErr 保存测试发送结果。
	sendErr := service.TestChannel(context.Background(), 7, 1, time.Unix(0, 0).UTC())
	if sendErr != nil || !strings.Contains(sender.body, "通知渠道测试") || strings.Contains(sender.body, "password") {
		t.Fatalf("测试正文异常: body=%q err=%v", sender.body, sendErr)
	}
}

// TestChannelServiceCoversCRUDBindingsAndValidation 验证渠道 CRUD、绑定操作和所有权边界。
func TestChannelServiceCoversCRUDBindingsAndValidation(t *testing.T) {
	// repository 是可切换返回值的渠道与绑定端口替身。
	repository := &channelRepositoryStub{
		record:       &ChannelRecord{ID: 1, Name: "旧名", Type: "email", Config: "old", EventTypes: "order", Enabled: false},
		ownedChannel: true,
		ownedAccount: true,
		bindingIDs:   []int64{1, 2},
		bindings:     []BindingSummary{{ID: 3, CookieID: "acc", ChannelID: 1}},
	}
	// service 是待验证的通知渠道服务。
	service := NewChannelService(repository, &channelSenderStub{})
	if // err 是渠道列表成功查询的错误。
	_, err := service.ListChannels(context.Background(), 7); err != nil {
		t.Fatalf("渠道列表失败: %v", err)
	}
	if // err 是绑定列表成功查询的错误。
	_, err := service.ListBindings(context.Background(), 7); err != nil {
		t.Fatalf("绑定列表失败: %v", err)
	}
	if // err 是渠道删除成功调用的错误。
	err := service.DeleteChannel(context.Background(), 7, 1); err != nil {
		t.Fatalf("渠道删除失败: %v", err)
	}
	if // err 是绑定删除成功调用的错误。
	err := service.DeleteBinding(context.Background(), 7, 3); err != nil {
		t.Fatalf("绑定删除失败: %v", err)
	}
	if // err 是账号绑定批量删除成功调用的错误。
	err := service.DeleteAccountBindings(context.Background(), 7, "acc"); err != nil {
		t.Fatalf("账号绑定删除失败: %v", err)
	}
	if // err 是账号绑定 ID 查询成功调用的错误。
	_, err := service.GetBindingIDs(context.Background(), 7, "acc"); err != nil {
		t.Fatalf("绑定 ID 查询失败: %v", err)
	}
	if // err 是覆盖多个绑定成功调用的错误。
	err := service.SetBindings(context.Background(), 7, "acc", []int64{1, 2}); err != nil || len(repository.sentBindings) != 2 {
		t.Fatalf("覆盖绑定失败: err=%v ids=%v", err, repository.sentBindings)
	}
	if // err 是单条绑定成功调用的错误。
	err := service.SetSingleBinding(context.Background(), 7, "acc", 1, true); err != nil {
		t.Fatalf("单条绑定失败: %v", err)
	}
	// name、channelType、config、eventTypes、enabled 保存完整部分更新输入。
	name, channelType, config, eventTypes, enabled := "新名", "webhook", "new", "all", true
	if // err 是完整渠道部分更新的错误。
	err := service.UpdateChannel(context.Background(), 7, 1, ChannelPatch{Name: &name, Type: &channelType, Config: &config, EventTypes: &eventTypes, Enabled: &enabled}); err != nil {
		t.Fatalf("完整渠道更新失败: %v", err)
	}
	if repository.updatedRecord == nil || repository.updatedRecord.Config != "new" || !repository.updatedRecord.Enabled {
		t.Fatalf("完整渠道更新未合并: %+v", repository.updatedRecord)
	}
	// createdID、err 保存渠道创建成功结果。
	createdID, err := service.CreateChannel(context.Background(), 7, ChannelInput{Name: "新渠道", Type: "webhook"})
	if err != nil || createdID != 41 {
		t.Fatalf("渠道创建失败: id=%d err=%v", createdID, err)
	}
	// storageErr 保存端口统一返回的存储错误。
	storageErr := errors.New("storage failure")
	// failingRepository 是返回存储错误的端口替身。
	failingRepository := &channelRepositoryStub{err: storageErr, record: repository.record, ownedChannel: true, ownedAccount: true}
	// failingService 是注入存储错误端口的服务。
	failingService := NewChannelService(failingRepository, &channelSenderStub{err: storageErr})
	if // err 是创建端口错误。
	_, err := failingService.CreateChannel(context.Background(), 7, ChannelInput{Name: "x", Type: "email"}); !errors.Is(err, storageErr) {
		t.Fatalf("创建端口错误未透传: %v", err)
	}
	if // err 是更新端口错误。
	err := failingService.UpdateChannel(context.Background(), 7, 1, ChannelPatch{}); !errors.Is(err, storageErr) {
		t.Fatalf("更新端口错误未透传: %v", err)
	}
	if // err 是删除渠道端口错误。
	err := failingService.DeleteChannel(context.Background(), 7, 1); !errors.Is(err, storageErr) {
		t.Fatalf("删除渠道错误未透传: %v", err)
	}
	if // err 是绑定 ID 查询端口错误。
	_, err := failingService.GetBindingIDs(context.Background(), 7, "acc"); !errors.Is(err, storageErr) {
		t.Fatalf("绑定 ID 错误未透传: %v", err)
	}
	if // err 是覆盖绑定端口错误。
	err := failingService.SetBindings(context.Background(), 7, "acc", nil); !errors.Is(err, storageErr) {
		t.Fatalf("覆盖绑定错误未透传: %v", err)
	}
	if // err 是单条绑定端口错误。
	err := failingService.SetSingleBinding(context.Background(), 7, "acc", 1, false); !errors.Is(err, storageErr) {
		t.Fatalf("单条绑定错误未透传: %v", err)
	}
	if // err 是绑定删除端口错误。
	err := failingService.DeleteBinding(context.Background(), 7, 3); !errors.Is(err, storageErr) {
		t.Fatalf("删除绑定错误未透传: %v", err)
	}
	if // err 是账号绑定删除端口错误。
	err := failingService.DeleteAccountBindings(context.Background(), 7, "acc"); !errors.Is(err, storageErr) {
		t.Fatalf("删除账号绑定错误未透传: %v", err)
	}
	if // err 是测试发送端口错误。
	err := failingService.TestChannel(context.Background(), 7, 1, time.Unix(1, 0)); !errors.Is(err, storageErr) {
		t.Fatalf("测试发送错误未透传: %v", err)
	}

	// invalidCases 是所有公开渠道操作的非法参数场景。
	invalidCases := []struct {
		// name 是当前非法参数场景名称。
		name string
		// run 是触发当前参数校验的操作。
		run func() error
	}{
		{name: "list", run: func() error {
			// err 是非法列表用户返回的参数错误。
			_, err := service.ListChannels(context.Background(), 0)
			return err
		}},
		{name: "create", run: func() error {
			// err 是非法创建用户返回的参数错误。
			_, err := service.CreateChannel(context.Background(), 0, ChannelInput{})
			return err
		}},
		{name: "update", run: func() error { return service.UpdateChannel(context.Background(), 0, 1, ChannelPatch{}) }},
		{name: "delete", run: func() error { return service.DeleteChannel(context.Background(), 0, 1) }},
		{name: "owns", run: func() error {
			// err 是非法归属用户返回的参数错误。
			_, err := service.OwnsChannel(context.Background(), 0, 1)
			return err
		}},
		{name: "test", run: func() error { return service.TestChannel(context.Background(), 0, 1, time.Time{}) }},
		{name: "bindings", run: func() error {
			// err 是非法绑定列表用户返回的参数错误。
			_, err := service.ListBindings(context.Background(), 0)
			return err
		}},
		{name: "binding-id", run: func() error {
			// err 是空账号标识返回的参数错误。
			_, err := service.GetBindingIDs(context.Background(), 7, " ")
			return err
		}},
		{name: "single-binding", run: func() error { return (*ChannelService)(nil).SetSingleBinding(context.Background(), 7, "acc", 1, true) }},
		{name: "delete-binding", run: func() error { return service.DeleteBinding(context.Background(), 0, 1) }},
	}
	// testCase 是当前非法参数测试场景。
	for _, testCase := range invalidCases {
		// err 是当前公开操作返回的参数错误。
		err := testCase.run()
		if !errors.Is(err, ErrChannelInvalidInput) {
			t.Errorf("%s: 参数错误=%v", testCase.name, err)
		}
	}
	if // err 是空服务渠道列表的参数错误。
	_, err := (*ChannelService)(nil).ListChannels(context.Background(), 1); !errors.Is(err, ErrChannelInvalidInput) {
		t.Fatalf("空服务列表错误异常: %v", err)
	}
	if // err 是空服务账号归属的参数错误。
	_, err := (*ChannelService)(nil).GetBindingIDs(context.Background(), 1, "acc"); !errors.Is(err, ErrChannelInvalidInput) {
		t.Fatalf("空服务账号错误异常: %v", err)
	}

	// invalidFieldCases 是创建和更新时的空名称、空类型输入。
	invalidFieldCases := []ChannelInput{{Name: "", Type: "email"}, {Name: "name", Type: ""}}
	// input 是当前字段校验场景。
	for _, input := range invalidFieldCases {
		// err 是字段校验返回的参数错误。
		_, err := service.CreateChannel(context.Background(), 7, input)
		if !errors.Is(err, ErrChannelInvalidInput) {
			t.Errorf("字段校验未拒绝: %+v => %v", input, err)
		}
	}
	// repository.record 保存更新字段校验使用的无效现有记录。
	repository.record = &ChannelRecord{ID: 1, Name: "", Type: "email"}
	if // err 是更新合并后字段校验返回的参数错误。
	err := service.UpdateChannel(context.Background(), 7, 1, ChannelPatch{}); !errors.Is(err, ErrChannelInvalidInput) {
		t.Fatalf("更新字段校验未拒绝: %v", err)
	}
	repository.record = nil
	if // err 是渠道归属查询为空时的禁止错误。
	err := service.UpdateChannel(context.Background(), 7, 1, ChannelPatch{}); !errors.Is(err, ErrChannelForbidden) {
		t.Fatalf("空渠道记录错误异常: %v", err)
	}

	// noSenderService 是未装配测试发送端口的服务。
	noSenderService := NewChannelService(repository, nil)
	if // err 是未装配通知器的测试发送错误。
	err := noSenderService.TestChannel(context.Background(), 7, 1, time.Time{}); !errors.Is(err, ErrNotifierUnavailable) {
		t.Fatalf("未装配通知器错误异常: %v", err)
	}
	// zeroTimeSender 是验证零时间自动填充的发送端口替身。
	zeroTimeSender := &channelSenderStub{}
	// zeroTimeService 是使用零时间测试发送的渠道服务。
	zeroTimeService := NewChannelService(&channelRepositoryStub{ownedChannel: true}, zeroTimeSender)
	if // err 是零时间测试正文生成的发送结果。
	err := zeroTimeService.TestChannel(context.Background(), 7, 1, time.Time{}); err != nil {
		t.Fatalf("零时间测试发送失败: %v", err)
	}
	// channelFailure 是渠道归属查询失败原因。
	channelFailure := errors.New("channel ownership failed")
	// channelFailureService 是注入渠道归属查询错误的服务。
	channelFailureService := NewChannelService(&channelRepositoryStub{ownedAccount: true, channelErr: channelFailure}, nil)
	if // err 是批量绑定渠道归属查询错误。
	err := channelFailureService.SetBindings(context.Background(), 7, "acc", []int64{1}); !errors.Is(err, channelFailure) {
		t.Fatalf("批量绑定归属错误未透传: %v", err)
	}
	if // err 是单条绑定渠道归属查询错误。
	err := channelFailureService.SetSingleBinding(context.Background(), 7, "acc", 1, true); !errors.Is(err, channelFailure) {
		t.Fatalf("单条绑定归属错误未透传: %v", err)
	}
	// forbiddenBindingService 是拒绝渠道归属的绑定服务。
	forbiddenBindingService := NewChannelService(&channelRepositoryStub{ownedAccount: true, ownedChannel: false}, nil)
	if // err 是批量绑定渠道归属禁止错误。
	err := forbiddenBindingService.SetBindings(context.Background(), 7, "acc", []int64{1}); !errors.Is(err, ErrChannelForbidden) {
		t.Fatalf("批量绑定禁止错误异常: %v", err)
	}
	if // err 是单条绑定归属禁止错误。
	err := forbiddenBindingService.SetSingleBinding(context.Background(), 7, "acc", 1, true); !errors.Is(err, ErrChannelForbidden) {
		t.Fatalf("单条绑定禁止错误异常: %v", err)
	}
}

// TestChannelEditorLegacySMTPModes 验证旧版覆盖及显式模式均准确展示，响应不包含任何 SMTP 内容。
func TestChannelEditorLegacySMTPModes(t *testing.T) {
	// scenario 枚举旧版配置和显式模式，want 是编辑器应展示的来源。
	for _, scenario := range []struct {
		// config 仅使用虚构值，模拟存储中的旧 JSON。
		config string
		// want 表示存在独立或逐字段 SMTP 覆盖。
		want bool
	}{
		{`{"smtp_server":"fixture.invalid","to_email":"recipient@example.com"}`, true},
		{`{"smtp_use_tls":false}`, true},
		{`{"smtp_from":"fixture@example.com"}`, true},
		{`{"smtp_server":""}`, false},
		{`{"smtp_server":"fixture.invalid","use_custom_smtp":false}`, false},
		{`{"use_custom_smtp":true}`, true},
		{`{"use_custom_smtp":"1"}`, true},
		{`{"use_custom_smtp":1}`, true},
		{`{"use_custom_smtp":{}}`, false},
		{`broken`, false},
	} {
		// repository 提供当前虚构配置，不读取真实凭据。
		repository := &channelRepositoryStub{record: &ChannelRecord{ID: 1, Name: "email", Type: "email", Config: scenario.config}}
		// editor、err 保存安全编辑视图和读取错误；失败不回显配置文本。
		editor, err := NewChannelService(repository, nil).GetChannelEditor(context.Background(), 7, 1)
		if err != nil || editor.UseCustomSMTP != scenario.want {
			t.Fatalf("模式判定失败: want=%v err=%v", scenario.want, err)
		}
	}
}

// TestEmailRecipientPatchPreservesSMTP 验证收件地址独立更新不会改变旧版覆盖、未知字段或服务端密码。
func TestEmailRecipientPatchPreservesSMTP(t *testing.T) {
	// repository 持有虚构邮件配置，测试不会发送通知。
	repository := &channelRepositoryStub{record: &ChannelRecord{ID: 1, Name: "email", Type: "email", Config: `{"email":"old@example.com","smtp_server":"fixture.invalid","smtp_password":"fixture-secret","extra":{"n":2}}`}}
	// recipient、name 是用户本次明确修改的非秘密字段。
	recipient, name := " new@example.com ", "renamed"
	// service 保存受测渠道更新编排。
	service := NewChannelService(repository, nil)
	// err 记录仅修改收件地址和名称的保存结果。
	if err := service.UpdateChannel(context.Background(), 7, 1, ChannelPatch{Name: &name, EmailRecipient: &recipient}); err != nil {
		t.Fatal(err)
	}
	// config 只用于验证存储边界保留字段，禁止失败时输出整个配置。
	var config map[string]any
	// err 校验保存结果仍可解析，不在失败信息中暴露配置。
	if err := json.Unmarshal([]byte(repository.updatedRecord.Config), &config); err != nil {
		t.Fatal("保存后的配置不是合法 JSON")
	}
	if repository.updatedRecord.Name != name || config["to_email"] != "new@example.com" || config["smtp_server"] != "fixture.invalid" || config["smtp_password"] != "fixture-secret" || config["extra"] == nil {
		t.Fatal("收件地址更新丢失原 SMTP 或未知字段")
	}
	// exists 检查更新是否意外引入原配置未声明的 SMTP 模式。
	if _, exists := config["use_custom_smtp"]; exists {
		t.Fatal("旧版逐字段模式不能被自动改写")
	}
}

// TestEmailRecipientPatchRejectsAmbiguousUpdates 验证类型冲突、同时替换配置、空地址和损坏存储不会覆盖旧值。
func TestEmailRecipientPatchRejectsAmbiguousUpdates(t *testing.T) {
	// recipient、replacement、otherType 提供互斥字段测试输入，均为虚构值。
	recipient, replacement, otherType := "new@example.com", `{}`, "webhook"
	// scenario 枚举必须拒绝的更新组合，oldConfig 不包含真实秘密。
	for _, scenario := range []struct {
		// channelType 是现有渠道协议。
		channelType string
		// oldConfig 是原始存储 JSON。
		oldConfig string
		// patch 是本次请求，EmailRecipient 与完整 Config 不可混用。
		patch ChannelPatch
	}{
		{"webhook", `{}`, ChannelPatch{EmailRecipient: &recipient}},
		{"email", `{}`, ChannelPatch{EmailRecipient: &recipient, Config: &replacement}},
		{"email", `{}`, ChannelPatch{EmailRecipient: &recipient, Type: &otherType}},
		{"email", `broken`, ChannelPatch{EmailRecipient: &recipient}},
		{"email", `null`, ChannelPatch{EmailRecipient: &recipient}},
		{"email", `[]`, ChannelPatch{EmailRecipient: &recipient}},
	} {
		// repository 保存当前场景原值，updatedRecord 应始终为空。
		repository := &channelRepositoryStub{record: &ChannelRecord{Name: "fixture", Type: scenario.channelType, Config: scenario.oldConfig}}
		// err 必须将歧义输入归类为参数错误，并阻止持久化。
		if err := NewChannelService(repository, nil).UpdateChannel(context.Background(), 7, 1, scenario.patch); !errors.Is(err, ErrChannelInvalidInput) || repository.updatedRecord != nil {
			t.Fatalf("歧义请求未拒绝: %v", err)
		}
	}
	// err 验证纯空白收件地址在存储前被拒绝。
	if _, err := patchEmailRecipient(`{}`, " "); !errors.Is(err, ErrChannelInvalidInput) {
		t.Fatal("空收件地址必须拒绝")
	}
}

// TestChannelServiceEditorReturnsPerTypeConfig 验证各渠道类型的编辑器回读：Config 只含可回显的非空键，
// SavedCredentialKeys 只含敏感键名且按字典序排序，响应中绝不出现任何敏感键的值。
func TestChannelServiceEditorReturnsPerTypeConfig(t *testing.T) {
	// markers 是全部虚构敏感值中的可识别片段，响应文本不得包含其中任何一个。
	markers := []string{"fixture-secret", "fixture-token", "fixture-key", "fixture-app", "access_token=fixture", "Bearer fixture"}
	// scenario 描述一种渠道类型的存储配置与期望回读结果。
	for _, scenario := range []struct {
		// name 是失败输出使用的场景名称。
		name string
		// channelType 是存储中的渠道类型，可能包含历史别名。
		channelType string
		// config 是存储中的配置 JSON，值均为虚构占位。
		config string
		// wantConfig 是期望回显的可回显键值；nil 表示无任何可回显键。
		wantConfig map[string]string
		// wantKeys 是期望回显的敏感键名，必须已按字典序排序；nil 表示无敏感键。
		wantKeys []string
	}{
		{
			name: "qq 全部键", channelType: "qq",
			config:     `{"app_id":"fixture-app","app_secret":"fixture-secret","user_openid":" fixture-user ","group_openid":"fixture-group"}`,
			wantConfig: map[string]string{"user_openid": "fixture-user", "group_openid": "fixture-group"},
			wantKeys:   []string{"app_id", "app_secret"},
		},
		{
			name: "qq 仅目标", channelType: "qq",
			config:     `{"user_openid":"fixture-user","group_openid":"  "}`,
			wantConfig: map[string]string{"user_openid": "fixture-user"},
		},
		{
			name: "bark 不含跳转地址", channelType: "bark",
			config:     `{"device_key":"fixture-key","server_url":" https://bark.invalid ","title":"标题","sound":"","group":"g","url":"https://jump.invalid"}`,
			wantConfig: map[string]string{"server_url": "https://bark.invalid", "title": "标题", "group": "g"},
			wantKeys:   []string{"device_key"},
		},
		{
			name: "dingtalk", channelType: "dingtalk",
			config:   `{"webhook_url":"https://oapi.invalid/robot?access_token=fixture","secret":"fixture-secret"}`,
			wantKeys: []string{"secret", "webhook_url"},
		},
		{
			name: "dingtalk 别名", channelType: "ding_talk",
			config:   `{"webhook_url":"https://oapi.invalid/robot?access_token=fixture","secret":"  "}`,
			wantKeys: []string{"webhook_url"},
		},
		{
			name: "feishu 别名", channelType: "lark",
			config:   `{"webhook_url":"https://open.invalid/hook","secret":"fixture-secret"}`,
			wantKeys: []string{"secret", "webhook_url"},
		},
		{
			name: "feishu", channelType: "feishu",
			config:   `{"webhook_url":"https://open.invalid/hook"}`,
			wantKeys: []string{"webhook_url"},
		},
		{
			name: "webhook 请求头归敏感", channelType: "webhook",
			config:     `{"http_method":"POST","webhook_url":"https://hook.invalid/x?key=fixture","headers":{"Authorization":"Bearer fixture"}}`,
			wantConfig: map[string]string{"http_method": "POST"},
			wantKeys:   []string{"headers", "webhook_url"},
		},
		{
			name: "wechat", channelType: "wechat",
			config:   `{"webhook_url":"https://qyapi.invalid/hook?key=fixture"}`,
			wantKeys: []string{"webhook_url"},
		},
		{
			name: "telegram", channelType: "telegram",
			config:     `{"chat_id":" fixture-chat ","bot_token":"fixture-token"}`,
			wantConfig: map[string]string{"chat_id": "fixture-chat"},
			wantKeys:   []string{"bot_token"},
		},
		{
			name: "email 不进入通用配置", channelType: "email",
			config: `{"to_email":"receiver@example.com","smtp_password":"fixture-secret"}`,
		},
		{
			name: "配置损坏", channelType: "qq",
			config: `broken`,
		},
		{
			name: "未知类型", channelType: "captcha",
			config: `{"token":"fixture-token"}`,
		},
	} {
		// repository 提供当前场景的虚构配置，不接触真实凭据。
		repository := &channelRepositoryStub{record: &ChannelRecord{ID: 1, Name: scenario.name, Type: scenario.channelType, Config: scenario.config}}
		// editor、err 保存脱敏编辑视图及读取错误。
		editor, err := NewChannelService(repository, nil).GetChannelEditor(context.Background(), 7, 1)
		if err != nil {
			t.Fatalf("场景 %s 读取编辑视图失败: %v", scenario.name, err)
		}
		if !reflect.DeepEqual(editor.Config, scenario.wantConfig) {
			t.Fatalf("场景 %s 可回显配置异常: got=%v want=%v", scenario.name, editor.Config, scenario.wantConfig)
		}
		if !reflect.DeepEqual(editor.SavedCredentialKeys, scenario.wantKeys) {
			t.Fatalf("场景 %s 敏感键名异常: got=%v want=%v", scenario.name, editor.SavedCredentialKeys, scenario.wantKeys)
		}
		// encoded 是编辑器视图的 JSON 表达，用于确认任何虚构敏感值都没有进入响应。
		encoded, encodeErr := json.Marshal(editor)
		if encodeErr != nil {
			t.Fatalf("场景 %s 序列化编辑视图失败: %v", scenario.name, encodeErr)
		}
		// marker 是当前需要确认未泄露的敏感值片段。
		for _, marker := range markers {
			if strings.Contains(string(encoded), marker) {
				t.Fatalf("场景 %s 编辑视图泄露敏感值 %q", scenario.name, marker)
			}
		}
	}
}

// TestChannelServiceUpdateMergesConfigByType 验证声明了 Merge 的类型按键合并：未提交键保留、空串删键、
// 空对象保留全部键、旧配置损坏按空对象处理、非对象提交视为未提交。
func TestChannelServiceUpdateMergesConfigByType(t *testing.T) {
	// scenario 描述一种合并场景：现有配置、本次提交与期望结果。
	for _, scenario := range []struct {
		// name 是失败输出使用的场景名称。
		name string
		// channelType 是现有渠道协议类型。
		channelType string
		// oldConfig 是原始存储配置，值均为虚构占位。
		oldConfig string
		// submitted 是本次提交的配置 JSON。
		submitted string
		// want 是期望的最终配置键值，只用于断言。
		want map[string]any
	}{
		{
			name: "qq 部分提交保留其余键", channelType: "qq",
			oldConfig: `{"app_id":"fixture-app","app_secret":"fixture-secret","user_openid":"old-user","group_openid":"fixture-group","extra":{"n":2}}`,
			submitted: `{"user_openid":"new-user"}`,
			want:      map[string]any{"app_id": "fixture-app", "app_secret": "fixture-secret", "user_openid": "new-user", "group_openid": "fixture-group", "extra": map[string]any{"n": float64(2)}},
		},
		{
			name: "qq 空串删除凭据键", channelType: "qq",
			oldConfig: `{"app_id":"fixture-app","app_secret":"fixture-secret","user_openid":"old-user"}`,
			submitted: `{"app_id":"","app_secret":" "}`,
			want:      map[string]any{"user_openid": "old-user"},
		},
		{
			name: "qq 空对象全保留", channelType: "qq",
			oldConfig: `{"app_id":"fixture-app","user_openid":"old-user"}`,
			submitted: `{}`,
			want:      map[string]any{"app_id": "fixture-app", "user_openid": "old-user"},
		},
		{
			name: "qq 旧配置损坏按空对象", channelType: "qq",
			oldConfig: `broken`,
			submitted: `{"user_openid":"fixture-user"}`,
			want:      map[string]any{"user_openid": "fixture-user"},
		},
		{
			name: "qq 非对象提交视为未提交", channelType: "qq",
			oldConfig: `{"user_openid":"old-user"}`,
			submitted: `["ignored"]`,
			want:      map[string]any{"user_openid": "old-user"},
		},
		{
			name: "bark 部分提交保留凭据", channelType: "bark",
			oldConfig: `{"device_key":"fixture-key","server_url":"https://old.invalid","title":"旧标题"}`,
			submitted: `{"title":"新标题"}`,
			want:      map[string]any{"device_key": "fixture-key", "server_url": "https://old.invalid", "title": "新标题"},
		},
		{
			name: "bark 空串删除标题", channelType: "bark",
			oldConfig: `{"device_key":"fixture-key","title":"旧标题"}`,
			submitted: `{"title":""}`,
			want:      map[string]any{"device_key": "fixture-key"},
		},
		{
			name: "dingtalk 部分提交", channelType: "dingtalk",
			oldConfig: `{"webhook_url":"https://oapi.invalid/robot?access_token=fixture","secret":"fixture-secret"}`,
			submitted: `{"secret":"new-secret"}`,
			want:      map[string]any{"webhook_url": "https://oapi.invalid/robot?access_token=fixture", "secret": "new-secret"},
		},
		{
			name: "dingtalk 别名合并", channelType: "ding_talk",
			oldConfig: `{"webhook_url":"https://oapi.invalid/robot","secret":"fixture-secret"}`,
			submitted: `{"webhook_url":"https://oapi.invalid/new"}`,
			want:      map[string]any{"webhook_url": "https://oapi.invalid/new", "secret": "fixture-secret"},
		},
		{
			name: "feishu 部分提交", channelType: "feishu",
			oldConfig: `{"webhook_url":"https://open.invalid/hook","secret":"fixture-secret"}`,
			submitted: `{"secret":"new-secret"}`,
			want:      map[string]any{"webhook_url": "https://open.invalid/hook", "secret": "new-secret"},
		},
		{
			name: "lark 别名合并", channelType: "lark",
			oldConfig: `{"webhook_url":"https://open.invalid/hook"}`,
			submitted: `{"secret":"fixture-secret"}`,
			want:      map[string]any{"webhook_url": "https://open.invalid/hook", "secret": "fixture-secret"},
		},
		{
			name: "webhook 请求头保留", channelType: "webhook",
			oldConfig: `{"http_method":"POST","webhook_url":"https://hook.invalid/x","headers":{"Authorization":"Bearer fixture"}}`,
			submitted: `{"http_method":"PUT"}`,
			want:      map[string]any{"http_method": "PUT", "webhook_url": "https://hook.invalid/x", "headers": map[string]any{"Authorization": "Bearer fixture"}},
		},
		{
			name: "wechat 部分提交", channelType: "wechat",
			oldConfig: `{"webhook_url":"https://qyapi.invalid/hook?key=fixture"}`,
			submitted: `{"webhook_url":"https://qyapi.invalid/new"}`,
			want:      map[string]any{"webhook_url": "https://qyapi.invalid/new"},
		},
		{
			name: "telegram 部分提交", channelType: "telegram",
			oldConfig: `{"chat_id":"old-chat","bot_token":"fixture-token"}`,
			submitted: `{"chat_id":"new-chat"}`,
			want:      map[string]any{"chat_id": "new-chat", "bot_token": "fixture-token"},
		},
	} {
		// repository 保存当前场景原记录，updatedRecord 用于断言合并结果。
		repository := &channelRepositoryStub{record: &ChannelRecord{ID: 1, Name: scenario.name, Type: scenario.channelType, Config: scenario.oldConfig}}
		// submitted 是本次提交的配置副本，避免取到循环变量地址。
		submitted := scenario.submitted
		// err 保存按键合并后的更新结果。
		if err := NewChannelService(repository, nil).UpdateChannel(context.Background(), 7, 1, ChannelPatch{Config: &submitted}); err != nil {
			t.Fatalf("场景 %s 更新失败: %v", scenario.name, err)
		}
		if repository.updatedRecord == nil {
			t.Fatalf("场景 %s 未写入更新记录", scenario.name)
		}
		// merged 保存合并后的配置，仅用于断言，禁止输出到日志。
		var merged map[string]any
		// err 保存合并结果的解析错误，失败信息不输出配置内容。
		if err := json.Unmarshal([]byte(repository.updatedRecord.Config), &merged); err != nil {
			t.Fatalf("场景 %s 合并结果不是合法 JSON", scenario.name)
		}
		if !reflect.DeepEqual(merged, scenario.want) {
			t.Fatalf("场景 %s 合并结果异常: got=%v want=%v", scenario.name, merged, scenario.want)
		}
	}
}

// TestChannelServiceUpdateReplacesConfigWhenRequested 验证邮件渠道与显式 ReplaceConfig 调用仍整体替换配置。
func TestChannelServiceUpdateReplacesConfigWhenRequested(t *testing.T) {
	// scenario 描述一种必须整体替换的场景：现有类型、原配置、提交配置与是否显式要求替换。
	for _, scenario := range []struct {
		// name 是失败输出使用的场景名称。
		name string
		// channelType 是现有渠道协议类型。
		channelType string
		// oldConfig 是原始存储配置，值均为虚构占位。
		oldConfig string
		// submitted 是本次提交的配置。
		submitted string
		// replace 表示显式要求整体替换（MCP 的 config_json 语义）。
		replace bool
	}{
		{name: "email 整体替换", channelType: "email", oldConfig: `{"to_email":"old@example.com","smtp_password":"fixture-secret"}`, submitted: `{"to_email":"new@example.com"}`},
		{name: "qq 显式替换", channelType: "qq", oldConfig: `{"app_id":"fixture-app","user_openid":"old-user"}`, submitted: `{"user_openid":"new-user"}`, replace: true},
		{name: "bark 显式替换", channelType: "bark", oldConfig: `{"device_key":"fixture-key","title":"旧标题"}`, submitted: `{"title":"新标题"}`, replace: true},
		{name: "未知类型整体替换", channelType: "captcha", oldConfig: `{"token":"fixture-token"}`, submitted: `{"other":"value"}`},
	} {
		// repository 提供当前场景的原记录，更新结果必须与提交值完全一致。
		repository := &channelRepositoryStub{record: &ChannelRecord{ID: 1, Name: scenario.name, Type: scenario.channelType, Config: scenario.oldConfig}}
		// submitted 是本次提交的配置副本，避免取到循环变量地址。
		submitted := scenario.submitted
		// err 保存更新结果。
		if err := NewChannelService(repository, nil).UpdateChannel(context.Background(), 7, 1, ChannelPatch{Config: &submitted, ReplaceConfig: scenario.replace}); err != nil {
			t.Fatalf("场景 %s 更新失败: %v", scenario.name, err)
		}
		if repository.updatedRecord.Config != scenario.submitted {
			t.Fatalf("场景 %s 必须整体替换配置", scenario.name)
		}
	}
}

// TestChannelConfigSpecForNormalizesAliases 验证历史别名命中同一份配置键分类，且规范类型保持独立。
func TestChannelConfigSpecForNormalizesAliases(t *testing.T) {
	// scenario 枚举类型名与期望命中的规范类型，用于确认别名只影响查表。
	for _, scenario := range []struct {
		// name 是失败输出使用的场景名称。
		name string
		// channelType 是待查表的类型名。
		channelType string
		// want 是期望命中的规范类型。
		want string
	}{
		{name: "钉钉别名", channelType: "ding_talk", want: "dingtalk"},
		{name: "飞书别名", channelType: "lark", want: "feishu"},
		{name: "钉钉规范名", channelType: "dingtalk", want: "dingtalk"},
		{name: "飞书规范名", channelType: "feishu", want: "feishu"},
		{name: "未知类型", channelType: "captcha", want: ""},
	} {
		if !reflect.DeepEqual(channelConfigSpecFor(scenario.channelType), channelConfigSpecs[scenario.want]) {
			t.Fatalf("场景 %s 未命中期望配置键分类", scenario.name)
		}
	}
}
