package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tokenFlightFixture 组合 Token 刷新测试使用的本地服务与请求计数器。
type tokenFlightFixture struct {
	// server 是返回固定 accessToken 的本地 token 服务。
	server *httptest.Server
	// requests 统计真实到达平台的 token 请求次数。
	requests *atomic.Int32
}

// client 返回指向本地服务的被测客户端，避免测试访问真实平台。
func (f tokenFlightFixture) client() *ClientImpl {
	return &ClientImpl{HTTPClient: f.server.Client(), TokenURL: f.server.URL + "/"}
}

// withTokenFlightTimings 临时替换合并窗口与最小间隔，返回恢复逻辑交给测试清理。
// window 是结果复用窗口；interval 是两次真实请求的最小间隔。
func withTokenFlightTimings(t *testing.T, window, interval time.Duration) {
	t.Helper()
	// originalWindow、originalInterval 保存原始配置，供测试结束时恢复。
	originalWindow, originalInterval := tokenRefreshShareWindow, tokenRefreshMinInterval
	tokenRefreshShareWindow, tokenRefreshMinInterval = window, interval
	t.Cleanup(func() {
		tokenRefreshShareWindow, tokenRefreshMinInterval = originalWindow, originalInterval
	})
}

// newTokenFlightFixture 构造统计请求次数的本地 token 服务。
func newTokenFlightFixture(t *testing.T) tokenFlightFixture {
	t.Helper()
	// counter 统计真实到达平台的 token 请求次数。
	counter := &atomic.Int32{}
	// service 是返回固定 accessToken 的本地 token 服务。
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		counter.Add(1)
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"flight-access"}}`)
	}))
	t.Cleanup(service.Close)
	return tokenFlightFixture{server: service, requests: counter}
}

// TestTokenRefreshBurstCoalescesIntoSingleRequest 验证同一账号的并发刷新风暴只产生一次真实平台请求。
// 复现场景：一次聊天页刷新会并发触发几十个业务接口，每个接口在令牌过期时各自刷新 token。
func TestTokenRefreshBurstCoalescesIntoSingleRequest(t *testing.T) {
	withTokenFlightTimings(t, 3*time.Second, 0)
	// fixture 是被测客户端与请求计数的组合夹具。
	fixture := newTokenFlightFixture(t)
	// client 是使用本地 token 服务的被测客户端。
	client := fixture.client()
	// callers 是并发刷新调用数，模拟页面刷新时的接口风暴。
	const callers = 20
	// start 是并发起跑信号，保证所有调用尽可能同时进入刷新入口。
	start := make(chan struct{})
	// wg 等待全部调用完成。
	var wg sync.WaitGroup
	// errs 收集每个调用的返回错误。
	errs := make([]error, callers)
	// tokens 收集每个调用得到的 accessToken。
	tokens := make([]string, callers)
	// index 是当前派发的并发调用下标。
	for index := 0; index < callers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			// result、callErr 保存当前并发调用的刷新结果与错误。
			result, callErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-flight")
			errs[slot] = callErr
			if result != nil {
				tokens[slot] = result.AccessToken
			}
		}(index)
	}
	close(start)
	wg.Wait()
	// total 是本次风暴实际产生的平台请求数。
	total := fixture.requests.Load()
	if total != 1 {
		t.Fatalf("并发刷新真实请求数=%d want 1", total)
	}
	// slot 是校验并发结果时的调用下标。
	for slot := 0; slot < callers; slot++ {
		if errs[slot] != nil || tokens[slot] != "flight-access" {
			t.Fatalf("调用 %d 结果异常 token=%q err=%v", slot, tokens[slot], errs[slot])
		}
	}
}

// TestTokenRefreshMinIntervalSpacesRealRequests 验证复用窗口过期后的真实请求仍受最小间隔限制。
func TestTokenRefreshMinIntervalSpacesRealRequests(t *testing.T) {
	withTokenFlightTimings(t, time.Millisecond, 80*time.Millisecond)
	// fixture 是被测客户端与请求计数的组合夹具。
	fixture := newTokenFlightFixture(t)
	// client 是使用本地 token 服务的被测客户端。
	client := fixture.client()
	if // firstErr 是首次刷新的错误；首次请求不受最小间隔限制。
	_, firstErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-gap"); firstErr != nil {
		t.Fatalf("首次刷新失败: %v", firstErr)
	}
	// 让首次结果离开复用窗口，迫使第二次调用发起真实请求。
	time.Sleep(5 * time.Millisecond)
	// startedAt 是第二次调用的发起时间，用于核对最小间隔。
	startedAt := time.Now()
	if // secondErr 是第二次刷新的错误；该请求必须等待最小间隔。
	_, secondErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-gap"); secondErr != nil {
		t.Fatalf("第二次刷新失败: %v", secondErr)
	}
	// elapsed 是第二次调用实际消耗的等待时间。
	elapsed := time.Since(startedAt)
	// total 是两次刷新实际产生的平台请求数。
	total := fixture.requests.Load()
	if total != 2 {
		t.Fatalf("真实请求数=%d want 2", total)
	}
	if elapsed < 60*time.Millisecond {
		t.Fatalf("第二次请求等待 %s 小于最小间隔", elapsed)
	}
}

// TestTokenRefreshWaiterCancellationSkipsRequest 验证等待串行锁的调用被取消时不会产生平台请求。
func TestTokenRefreshWaiterCancellationSkipsRequest(t *testing.T) {
	withTokenFlightTimings(t, time.Millisecond, 0)
	// handlerStarted 通知测试首个请求已经进入服务端处理。
	handlerStarted := make(chan struct{})
	// release 控制服务端何时返回，从而让首个调用保持在途。
	release := make(chan struct{})
	// counter 统计真实到达平台的 token 请求次数。
	counter := &atomic.Int32{}
	// service 是首个请求可控阻塞的 token 服务。
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		counter.Add(1)
		close(handlerStarted)
		<-release
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"held-access"}}`)
	}))
	defer service.Close()
	// client 是使用可控服务的被测客户端。
	client := &ClientImpl{HTTPClient: service.Client(), TokenURL: service.URL + "/"}
	// leaderDone 等待首个调用结束。
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		_, _ = client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-wait")
	}()
	<-handlerStarted
	// waiterCtx、waiterCancel 是立即取消的等待方上下文；等待方必须快速失败且不发起新请求。
	waiterCtx, waiterCancel := context.WithCancel(context.Background())
	waiterCancel()
	// waiterErr 保存被取消等待方的刷新错误。
	_, waiterErr := client.RefreshTokenWithDeviceIDContext(waiterCtx, testCookiesWithUnb, "did-wait")
	if waiterErr == nil {
		t.Fatal("等待方取消后必须返回错误")
	}
	close(release)
	<-leaderDone
	// total 是本次场景实际产生的平台请求数；等待方被取消后仍只能有一次。
	total := counter.Load()
	if total != 1 {
		t.Fatalf("真实请求数=%d want 1", total)
	}
}

// TestTokenRefreshAccountsAreIsolated 验证不同账号之间不共享刷新结果与限频状态。
func TestTokenRefreshAccountsAreIsolated(t *testing.T) {
	withTokenFlightTimings(t, 3*time.Second, 0)
	// fixture 是被测客户端与请求计数的组合夹具。
	fixture := newTokenFlightFixture(t)
	// client 是使用本地 token 服务的被测客户端。
	client := fixture.client()
	if // firstErr 是账号 A 的刷新错误，必须成功。
	_, firstErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-a"); firstErr != nil {
		t.Fatalf("账号 A 刷新失败: %v", firstErr)
	}
	// otherCookies 是另一个账号的 Cookie 串。
	otherCookies := "unb=456; _m_h5_tk=oldtoken_1;"
	if // secondErr 是账号 B 的刷新错误，必须独立请求。
	_, secondErr := client.RefreshTokenWithDeviceIDContext(context.Background(), otherCookies, "did-b"); secondErr != nil {
		t.Fatalf("账号 B 刷新失败: %v", secondErr)
	}
	// total 是跨账号刷新实际产生的平台请求数，两个账号各自请求一次。
	total := fixture.requests.Load()
	if total != 2 {
		t.Fatalf("跨账号真实请求数=%d want 2", total)
	}
}

// TestTokenRefreshCallerCancelDoesNotPoisonFollowers 验证调用方取消造成的失败不会被后续调用复用。
func TestTokenRefreshCallerCancelDoesNotPoisonFollowers(t *testing.T) {
	withTokenFlightTimings(t, 3*time.Second, 0)
	// release 控制首次请求何时结束，保证它被调用方超时打断。
	release := make(chan struct{})
	// counter 统计真实到达平台的 token 请求次数。
	counter := &atomic.Int32{}
	// service 是首个请求延迟返回、用于制造调用方超时的 token 服务。
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// attempt 是当前请求的序号；首个请求被阻塞到测试放行。
		attempt := counter.Add(1)
		if attempt == 1 {
			<-release
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"recover-access"}}`)
	}))
	defer service.Close()
	// client 是使用延迟服务的被测客户端。
	client := &ClientImpl{HTTPClient: service.Client(), TokenURL: service.URL + "/"}
	// shortCtx、shortCancel 给首次调用极短预算，制造调用方超时。
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	// firstErr 保存被超时打断的首次刷新错误。
	_, firstErr := client.RefreshTokenWithDeviceIDContext(shortCtx, testCookiesWithUnb, "did-poison")
	shortCancel()
	if firstErr == nil {
		t.Fatal("首次调用应因超时失败")
	}
	close(release)
	// result、secondErr 保存后续调用的刷新结果与错误。
	result, secondErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-poison")
	if secondErr != nil || result == nil || result.AccessToken != "recover-access" {
		t.Fatalf("后续调用未重新请求 result=%+v err=%v", result, secondErr)
	}
	// total 是本次场景实际产生的平台请求数；取消失败不得被复用，因此必须是两次。
	total := counter.Load()
	if total != 2 {
		t.Fatalf("真实请求数=%d want 2", total)
	}
}

// TestTokenRefreshShareWindowExpiry 验证复用窗口过期后并发调用会重新请求平台。
func TestTokenRefreshShareWindowExpiry(t *testing.T) {
	withTokenFlightTimings(t, 20*time.Millisecond, 0)
	// fixture 是被测客户端与请求计数的组合夹具。
	fixture := newTokenFlightFixture(t)
	// client 是使用本地 token 服务的被测客户端。
	client := fixture.client()
	if // firstErr 是首次刷新的错误，必须成功。
	_, firstErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-window"); firstErr != nil {
		t.Fatalf("首次刷新失败: %v", firstErr)
	}
	// 越过复用窗口，确认结果不再被复用。
	time.Sleep(40 * time.Millisecond)
	if // secondErr 是窗口过期后的刷新错误，必须重新请求平台。
	_, secondErr := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did-window"); secondErr != nil {
		t.Fatalf("窗口过期后的刷新失败: %v", secondErr)
	}
	// total 是窗口过期后实际产生的平台请求数。
	total := fixture.requests.Load()
	if total != 2 {
		t.Fatalf("窗口过期后真实请求数=%d want 2", total)
	}
}

// TestTokenRefreshAccountKeyPrefersUnb 验证账号键优先取 unb，Cookie 轮换后仍命中同一分组。
func TestTokenRefreshAccountKeyPrefersUnb(t *testing.T) {
	// key 是带 unb 的账号键；必须与旧 Cookie 的键一致。
	key := tokenRefreshAccountKey("unb=789; _m_h5_tk=newtoken_2;")
	if !strings.Contains(key, "unb:789") {
		t.Fatalf("账号键=%q 未使用 unb", key)
	}
	// rotated 是同一账号轮换签名令牌后的账号键。
	rotated := tokenRefreshAccountKey("unb=789; _m_h5_tk=other_3;")
	if key != rotated {
		t.Fatalf("令牌轮换后账号键变化 %q != %q", key, rotated)
	}
}
