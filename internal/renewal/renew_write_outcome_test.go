package renewal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestRenewStreakJustReachedThree 验证连续三次判定只在每个连续段的起点成立一次。
func TestRenewStreakJustReachedThree(t *testing.T) {
	// cases 覆盖长度不足、段首、段中、段尾与状态交替。
	cases := []struct {
		name     string
		statuses []string
		want     string
		expect   bool
	}{
		{name: "不足三次", statuses: []string{"write_rejected", "write_rejected"}, want: "write_rejected", expect: false},
		{name: "段首成立", statuses: []string{"write_rejected", "write_rejected", "write_rejected"}, want: "write_rejected", expect: true},
		{name: "第四次同为该状态不再通知", statuses: []string{"write_rejected", "write_rejected", "write_rejected", "write_rejected"}, want: "write_rejected", expect: false},
		{name: "混合状态不成立", statuses: []string{"write_rejected", "failed", "write_rejected"}, want: "write_rejected", expect: false},
		{name: "请求失败段首成立", statuses: []string{"failed", "failed", "failed", "cookie_updated"}, want: "failed", expect: true},
	}
	// tc 是本次校验的连续状态用例。
	for _, tc := range cases {
		// got 是当前用例的连续三次判定结果。
		got := renewStreakJustReachedThree(tc.statuses, tc.want)
		if got != tc.expect {
			t.Fatalf("%s: got=%v want=%v", tc.name, got, tc.expect)
		}
	}
}

// TestSaveRenewedCookiesOutcomes 验证写回被拒与持久化失败被区分成不同结果。
func TestSaveRenewedCookiesOutcomes(t *testing.T) {
	// store、cleanup 是本地续期仓储。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// scheduler 是不带外部依赖的续期调度器。
	scheduler := NewScheduler(store, nil, nil, nil)
	// ctx 是本次写回的生命周期。
	ctx := context.Background()
	// account 是库中已带 MTOP 签名令牌的账号。
	account := createSchedulerAccount(t, store, "cid-guard", "_m_h5_tk=tok_1; other=1")

	// 响应不含签名令牌且库中已有令牌时，守卫必须拒绝覆盖并保留原凭证。
	// rejected 是守卫拒绝写回后的结果分类。
	rejected := scheduler.saveRenewedCookies(ctx, account.ID, "other=2", `{}`)
	if rejected != renewWriteRejected {
		t.Fatalf("库中已有令牌时应拒绝降级写回: outcome=%s", rejected)
	}
	// kept、err 保存拒绝写回后库中的凭证。
	kept, err := store.Cookies.GetValue(ctx, account.ID)
	if err != nil || !strings.Contains(kept, "_m_h5_tk=tok_1") {
		t.Fatalf("拒绝写回后凭证不应变化: value=%q err=%v", kept, err)
	}

	// 库中不存在签名令牌时，同样缺少令牌的响应必须照常写回。
	plainAccount := createSchedulerAccount(t, store, "cid-plain", "unb=1")
	// stored 是无既有令牌时的写回结果分类。
	stored := scheduler.saveRenewedCookies(ctx, plainAccount.ID, "unb=2", `{}`)
	if stored != renewWriteStored {
		t.Fatalf("库中无令牌时应照常写回: outcome=%s", stored)
	}
	// missing 是账号不存在时的写回结果分类。
	missing := scheduler.saveRenewedCookies(ctx, "missing-account", "unb=2", `{}`)
	if missing != renewWriteFailed {
		t.Fatalf("账号缺失应判为持久化失败: outcome=%s", missing)
	}
}

// TestAddAPILogNotifiesWriteRejectedSeparately 验证写回被拒与请求失败各自独立计数并分级通知。
func TestAddAPILogNotifiesWriteRejectedSeparately(t *testing.T) {
	// store、cleanup 是本地续期仓储。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// notifier 记录通知次数与级别。
	notifier := &schedulerFakeNotifier{}
	// scheduler 绑定通知替身以观察告警分支。
	scheduler := NewScheduler(store, nil, nil, nil, notifier)
	// ctx 是本次写日志的生命周期。
	ctx := context.Background()
	// account 是本次记录日志的账号。
	account := createSchedulerAccount(t, store, "cid-notify", "unb=1")

	// 连续三次写回被拒只应产生一次提示级通知。
	for i := 0; i < 4; i++ {
		scheduler.addAPILog(ctx, db.RenewalLog{BatchID: "b", CookieID: account.ID, Status: "write_rejected", ErrorMessage: "响应缺少 MTOP 签名令牌，拒绝降级写回，库中凭证保持不变"})
	}
	if notifier.calls.Load() != 1 {
		t.Fatalf("写回被拒连续段应只通知一次: calls=%d", notifier.calls.Load())
	}
	if notifier.level != "info" || !strings.Contains(notifier.title, "写回被拒") {
		t.Fatalf("写回被拒应为提示级通知: level=%s title=%s", notifier.level, notifier.title)
	}

	// 请求失败属于独立连续段，必须另行发出警告级通知。
	scheduler.addAPILog(ctx, db.RenewalLog{BatchID: "b", CookieID: account.ID, Status: "failed", ErrorMessage: "接口超时"})
	scheduler.addAPILog(ctx, db.RenewalLog{BatchID: "b", CookieID: account.ID, Status: "failed", ErrorMessage: "接口超时"})
	scheduler.addAPILog(ctx, db.RenewalLog{BatchID: "b", CookieID: account.ID, Status: "failed", ErrorMessage: "接口超时"})
	if notifier.calls.Load() != 2 {
		t.Fatalf("请求失败连续段应另行通知: calls=%d", notifier.calls.Load())
	}
	if notifier.level != "warn" || !strings.Contains(notifier.title, "连续失败") {
		t.Fatalf("请求失败应为警告级通知: level=%s title=%s", notifier.level, notifier.title)
	}
}

// TestAPICookieRenewOneRecordsWriteRejected 验证守卫拒绝写回时日志状态为 write_rejected 且不重启账号。
func TestAPICookieRenewOneRecordsWriteRejected(t *testing.T) {
	// store、cleanup 是本地续期仓储。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// ctx 是本次续期的生命周期。
	ctx := context.Background()
	// expire 是未过期的长登录时间戳。
	expire := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	// account 是库中已带签名令牌的账号。
	account := createSchedulerAccount(t, store, "cid-rejected", "unb=1; _m_h5_tk=tok_1; havana_lgc_exp="+expire)
	// starter 记录账号重启次数。
	starter := &schedulerFakeStarter{}
	// srv 返回成功结果，但下发的 Cookie 集合不含签名令牌（响应显式清空 _m_h5_tk）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)})
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "", MaxAge: -1})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()

	// s 是绑定本地续期服务的调度器。
	s := NewScheduler(store, starter, &schedulerFakePasswordRefresher{}, nil)
	s.api = schedulerRenewServiceFromServer(srv)
	s.apiCookieRenewOne(ctx, "batch-rejected", account)

	// log 是本次续期写入的日志。
	log := lastAPIRenewLog(t, store, account.ID)
	if log.status != "write_rejected" {
		t.Fatalf("状态应为写回被拒: status=%s message=%s err=%s", log.status, log.message, log.errorMessage)
	}
	if !strings.Contains(log.errorMessage, "拒绝降级写回") {
		t.Fatalf("错误说明应指出拒写回: %s", log.errorMessage)
	}
	if starter.restarts.Load() != 0 {
		t.Fatalf("写回被拒不构成凭证更新，不应重启账号: restarts=%d", starter.restarts.Load())
	}
	// kept、err 保存拒绝写回后库中的凭证。
	kept, err := store.Cookies.GetValue(ctx, account.ID)
	if err != nil || !strings.Contains(kept, "_m_h5_tk=tok_1") {
		t.Fatalf("库中签名令牌应保留: value=%q err=%v", kept, err)
	}
}
