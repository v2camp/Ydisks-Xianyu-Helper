package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSource 是测试用的假备份读取源；可控制卡密、规则与间隔的返回值。
type fakeSource struct {
	// cards 是 Cards 调用返回的卡密记录。
	cards []CardRecord
	// rules 是 AutomationRules 调用返回的规则记录。
	rules []RuleRecord
	// hours 是 IntervalHours 调用返回的间隔小时数。
	hours int
	// cardsErr 非 nil 时 Cards 返回该错误，用于验证失败不落盘。
	cardsErr error
	// rulesErr 非 nil 时 AutomationRules 返回该错误。
	rulesErr error
}

// Cards 返回预置卡密记录。
func (f *fakeSource) Cards(context.Context) ([]CardRecord, error) {
	return f.cards, f.cardsErr
}

// AutomationRules 返回预置规则记录。
func (f *fakeSource) AutomationRules(context.Context) ([]RuleRecord, error) {
	return f.rules, f.rulesErr
}

// IntervalHours 返回预置间隔。
func (f *fakeSource) IntervalHours(context.Context) (int, error) {
	return f.hours, nil
}

// TestServiceStartsWritesSnapshotImmediately 验证服务启动即产出一份内容正确的备份文件。
func TestServiceStartsWritesSnapshotImmediately(t *testing.T) {
	// dir 是本用例专属的临时备份目录。
	dir := t.TempDir()
	// source 预置一条卡密与一条规则；APIConfig 用密文形态字符串验证原样落盘。
	source := &fakeSource{
		cards: []CardRecord{{ID: 1, Name: "测试卡密", Type: "text", APIConfig: "enc:v1:ciphertext", TextContent: "发货内容", Enabled: true}},
		rules: []RuleRecord{{ID: 7, Name: "付款发货", TriggerType: "order_paid", Enabled: true,
			Actions: []ActionRecord{{ID: 3, ActionType: "send_card", CardID: 1, Enabled: true}}}},
		hours: 24,
	}
	// svc 是被测服务；checkEvery 注入短周期以便用例内等待后续检查点。
	svc := NewService(source, dir, nil)
	svc.checkEvery = 5 * time.Millisecond
	// ctx、cancel 控制备份循环生命周期；验证完成后取消并等待退出。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// done 供 Run 在独立 goroutine 中运行后由测试同步等待。
	done := make(chan struct{})
	go func() {
		// 后台运行备份循环，退出后关闭 done。
		svc.Run(ctx)
		close(done)
	}()
	// 轮询等待首份备份文件出现；超时判定为服务未按契约「启动即备份」。
	waitForFile(t, dir)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("备份循环未在取消后退出")
	}
	// entries 是备份目录内容；应恰好有一份 JSON 文件。
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("备份目录应恰好有 1 份文件: entries=%v err=%v", entries, readErr)
	}
	// payload 是备份文件内容；解析后校验两份资产与密文原样。
	payload, readErr := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if readErr != nil {
		t.Fatalf("读取备份文件失败: %v", readErr)
	}
	// snap 是解析出的快照对象。
	var snap Snapshot
	// jsonErr 是备份 JSON 的解析错误。
	if jsonErr := json.Unmarshal(payload, &snap); jsonErr != nil {
		t.Fatalf("备份文件不是合法 JSON: %v", jsonErr)
	}
	if snap.Version != 1 || len(snap.Cards) != 1 || len(snap.AutomationRules) != 1 {
		t.Fatalf("快照内容不完整: version=%d cards=%d rules=%d", snap.Version, len(snap.Cards), len(snap.AutomationRules))
	}
	if snap.Cards[0].APIConfig != "enc:v1:ciphertext" {
		t.Fatalf("卡密 APIConfig 应密文原样落盘, got %q", snap.Cards[0].APIConfig)
	}
	if snap.AutomationRules[0].Actions[0].ActionType != "send_card" {
		t.Fatalf("规则动作应随备份导出, got %+v", snap.AutomationRules[0].Actions)
	}
}

// TestServicePrunesOldestSnapshots 验证备份保留数量上限，超出时淘汰最旧的文件。
func TestServicePrunesOldestSnapshots(t *testing.T) {
	// dir 是本用例专属的临时备份目录；预置 keepFiles 份按时间递增的旧备份。
	dir := t.TempDir()
	// stale 是其中最旧的一份，快照超限后应被优先淘汰。
	stale := filePrefix + "20000101-000000.json"
	// index 遍历预置 keepFiles 份旧备份，文件名时间戳逐秒递增。
	for index := 0; index < keepFiles; index++ {
		// name 是按序号生成的旧备份文件名。
		name := filePrefix + time.Date(2000, 1, 1, 0, 0, index, 0, time.UTC).Format("20060102-150405") + ".json"
		// writeErr 是预置旧备份的写入错误。
		if writeErr := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); writeErr != nil {
			t.Fatalf("预置旧备份失败: %v", writeErr)
		}
	}
	// source 的间隔设为 0：服务保持运行但不再产出，用于只验证淘汰逻辑。
	source := &fakeSource{hours: 0}
	// svc 是被测服务。
	svc := NewService(source, dir, nil)
	svc.checkEvery = 5 * time.Millisecond
	// ctx、cancel 控制循环生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 再产生一份新备份：总数达到 keepFiles+1，应淘汰最旧的一份。
	svc.snapshot(ctx)
	// remaining 是淘汰后的备份文件数量；应恰好等于 keepFiles。
	remaining, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("读取备份目录失败: %v", readErr)
	}
	if len(remaining) != keepFiles {
		t.Fatalf("应保留 %d 份备份, got %d", keepFiles, len(remaining))
	}
	// entry 是当前遍历的保留文件条目。
	for _, entry := range remaining {
		if entry.Name() == stale {
			t.Fatal("最旧的备份文件应被优先淘汰")
		}
	}
}

// TestServiceSkipsWhenIntervalZero 验证间隔为 0 时检查点不产出新备份。
func TestServiceSkipsWhenIntervalZero(t *testing.T) {
	// dir 是本用例专属的临时备份目录。
	dir := t.TempDir()
	// source 的间隔为 0，表示用户关闭定时备份。
	source := &fakeSource{hours: 0, cards: []CardRecord{{ID: 1, Name: "卡密"}}, rules: []RuleRecord{}}
	// svc 是被测服务；checkEvery 注入短周期。
	svc := NewService(source, dir, nil)
	svc.checkEvery = 5 * time.Millisecond
	// ctx、cancel 控制循环生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	// done 供同步循环退出。
	done := make(chan struct{})
	go func() {
		// 后台运行备份循环。
		svc.Run(ctx)
		close(done)
	}()
	// 启动即产生的首份备份存在；等待若干检查点后数量仍应为 1。
	waitForFile(t, dir)
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	// entries 是关闭时的目录内容。
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("间隔为 0 时不应产出新备份: entries=%d err=%v", len(entries), readErr)
	}
}

// TestServiceSkipsWhenReadFails 验证读取失败时不落盘，等待下个周期重试。
func TestServiceSkipsWhenReadFails(t *testing.T) {
	// dir 是本用例专属的临时备份目录。
	dir := t.TempDir()
	// source 的规则读取固定失败。
	source := &fakeSource{hours: 24, cards: []CardRecord{{ID: 1}}, rulesErr: context.DeadlineExceeded}
	// svc 是被测服务。
	svc := NewService(source, dir, nil)
	// ok 表示本次快照是否成功；读取失败应为 false 且不产生文件。
	if ok := svc.snapshot(context.Background()); ok {
		t.Fatal("读取失败时 snapshot 不应返回成功")
	}
	// entries 是目录内容；应为空。
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("读取失败时不应产生备份文件: entries=%d err=%v", len(entries), readErr)
	}
}

// TestServiceCreatesMissingDirectory 验证备份目录不存在时首次快照会自动创建并落盘。
func TestServiceCreatesMissingDirectory(t *testing.T) {
	// dir 是尚不存在的多级备份目录；模拟容器首次启动的场景。
	dir := filepath.Join(t.TempDir(), "data", "backups")
	// source 预置一条卡密。
	source := &fakeSource{cards: []CardRecord{{ID: 1, Name: "卡密"}}, rules: []RuleRecord{}, hours: 24}
	// svc 是被测服务。
	svc := NewService(source, dir, nil)
	// ok 表示本次快照是否成功；目录自动创建后应成功。
	if ok := svc.snapshot(context.Background()); !ok {
		t.Fatal("备份目录不存在时首次快照应自动创建目录并成功")
	}
	// entries 是自动创建后的目录内容；应恰好有一份备份。
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("自动创建目录后应有一份备份: entries=%d err=%v", len(entries), readErr)
	}
}

// waitForFile 轮询等待目录中出现首个备份文件，超时判定为服务未按契约产出。
func waitForFile(t *testing.T, dir string) {
	t.Helper()
	// deadline 是轮询截止时刻；超过即判定失败。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// entries 是当前目录内容；出现任意条目即视为备份已产出。
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("等待备份文件超时")
}
