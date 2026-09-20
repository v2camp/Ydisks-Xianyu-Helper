package browser

// TestWebUISmokeAdminFlow 用真实 Chromium 打开 Web UI 并逐页冒烟，
// 覆盖初始化管理员、登录与关键页面渲染，捕获页面白屏与控制台错误类回归。

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// e2eAdminPassword 是 e2e 冒烟流程初始化管理员时设置的密码。
const e2eAdminPassword = "e2e-admin-pw"

// e2eBaseURL 是 e2e 常驻 server 的进程内访问地址，测试浏览器与健康探测共用。
const e2eBaseURL = "http://127.0.0.1:59188"

// e2eHealthTimeout 是 server 就绪探测的最长等待时间，覆盖编译产物冷启动与数据库迁移。
const e2eHealthTimeout = 20 * time.Second

// e2eNavigateTimeout 是每个页面从导航到可断言状态的等待上限。
const e2eNavigateTimeout = 10 * time.Second

// runE2EServerOnce 用指定数据库执行一次 -init-admin 初始化后退出；输出保留以便失败诊断。
func runE2EServerOnce(t *testing.T, serverBin, dbPath string) {
	t.Helper()
	// initCmd 是一次性的管理员初始化子进程，初始化完成后立即退出、不监听端口。
	initCmd := exec.Command(serverBin, "-db", dbPath, "-init-admin", "-admin-password", e2eAdminPassword)
	// initOutput 捕获初始化子进程的标准输出与错误输出，失败时用于定位原因。
	var initOutput bytes.Buffer
	initCmd.Stdout = &initOutput
	initCmd.Stderr = &initOutput
	// initErr 保存初始化子进程的退出错误，任何非零退出都视为初始化失败。
	initErr := initCmd.Run()
	if initErr != nil {
		t.Fatalf("初始化管理员失败: %v\n输出:\n%s", initErr, initOutput.String())
	}
}

// e2eServer 持有常驻 server 子进程及其清理函数，保证测试退出时进程一定被回收。
type e2eServer struct {
	// cmd 是绑定本测试生命周期的常驻 server 进程。
	cmd *exec.Cmd
	// stop 结束常驻进程并回收其退出状态，可安全重复调用。
	stop func()
}

// startE2EServer 启动无浏览器模式的常驻 server，并注册测试结束时的进程清理。
func startE2EServer(t *testing.T, serverBin, dbPath string) *e2eServer {
	t.Helper()
	// serverCmd 是常驻服务进程，禁用 Chromium 以最小化测试对外部浏览器运行时的依赖。
	serverCmd := exec.Command(serverBin, "-db", dbPath, "-addr", "127.0.0.1:59188", "-no-browser")
	// serverOutput 保存常驻进程日志，便于 e2e 失败时回溯服务端异常。
	var serverOutput bytes.Buffer
	serverCmd.Stdout = &serverOutput
	serverCmd.Stderr = &serverOutput
	// startErr 保存常驻进程启动结果，启动失败说明 server 二进制或数据目录不可用。
	startErr := serverCmd.Start()
	if startErr != nil {
		t.Fatalf("启动 server 失败: %v", startErr)
	}
	// stopped 保证 stop 幂等，避免重复向已退出进程发送终止信号。
	var stopped bool
	// server 是绑定本测试生命周期的常驻服务句柄，清理函数由测试框架统一回收。
	server := &e2eServer{cmd: serverCmd, stop: func() {
		if stopped {
			return
		}
		stopped = true
		// killErr 与 waitErr 保存进程终止与回收结果；主动 kill 属预期清理，不视为失败。
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
	}}
	t.Cleanup(server.stop)
	return server
}

// waitE2EHealth 轮询 /health 直到 server 就绪或超过等待上限。
func waitE2EHealth(t *testing.T) {
	t.Helper()
	// deadline 是健康探测的截止时间，超时即判定服务未就绪。
	deadline := time.Now().Add(e2eHealthTimeout)
	for time.Now().Before(deadline) {
		// resp 是单次健康探测的响应；网络错误按未就绪继续重试。
		resp, err := http.Get(e2eBaseURL + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("server 在健康探测超时时间内未就绪")
}

// e2eConsoleErrors 汇总页面控制台错误与页面级运行时异常，任一出现即视为回归。
type e2eConsoleErrors struct {
	// mu 保护 errs 的并发追加，Playwright 事件回调与主测试协程异步执行。
	mu sync.Mutex
	// errs 保存已捕获的错误描述，按出现顺序追加。
	errs []string
}

// e2eExternalProbeSuffix 是页面自动发起的对外部 AI 服务连通性探测接口；e2e 容器通常无外网，其失败属环境差异。
const e2eExternalProbeSuffix = "/api/v1/settings/ai-models"

// recordConsoleError 将 Playwright 控制台消息的 error 级文本记入错误集合。
func (c *e2eConsoleErrors) recordConsoleError(message playwright.ConsoleMessage) {
	if message.Type() != "error" {
		return
	}
	// 外部 AI 模型探测失败时浏览器只给出 502 资源加载提示，与已豁免的探测接口配套，不作为回归。
	if strings.Contains(message.Text(), "502 (Bad Gateway)") {
		return
	}
	c.recordError(message.Text())
}

// recordError 追加一条错误描述；重复文本不去重，保留原始出现顺序。
func (c *e2eConsoleErrors) recordError(detail string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, detail)
}

// isExternalProbeURL 判断请求是否指向对外部 AI 服务的连通性探测接口。
func isExternalProbeURL(requestURL string) bool {
	return strings.Contains(requestURL, e2eExternalProbeSuffix)
}

// collect 返回当前已捕获的全部错误；调用方用于断言失败的诊断输出。
func (c *e2eConsoleErrors) collect() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	// snapshot 是错误集合的副本，避免断言期间数据被并发修改。
	snapshot := make([]string, len(c.errs))
	copy(snapshot, c.errs)
	return snapshot
}

// attachE2EErrorCollection 挂接控制台错误、网络请求失败与页面异常监听，注入到指定页面。
func attachE2EErrorCollection(page playwright.Page, errors *e2eConsoleErrors) {
	page.OnConsole(func(message playwright.ConsoleMessage) {
		errors.recordConsoleError(message)
	})
	page.OnPageError(func(pageErr error) {
		errors.recordError("pageerror: " + pageErr.Error())
	})
	page.OnRequestFailed(func(request playwright.Request) {
		// requestURL 是网络层失败的请求地址，用于区分是静态资源还是业务接口异常。
		requestURL := request.URL()
		// failureDetail 是浏览器给失败请求的原因描述；整页切换时会中止进行中的请求。
		failureDetail := request.Failure()
		// 页面导航导致的 net::ERR_ABORTED 属浏览器正常取消，不视为用户动线回归。
		if failureDetail != nil && strings.Contains(failureDetail.Error(), "ERR_ABORTED") {
			return
		}
		errors.recordError(fmt.Sprintf("requestfailed: %s (%v)", requestURL, failureDetail))
	})
	// 响应级 5xx 属于服务端异常，记录 URL 与状态码用于定位失败的业务接口。
	page.OnResponse(func(response playwright.Response) {
		if response.Status() < 500 {
			return
		}
		// 外部 AI 模型探测接口在离线容器内的失败属环境差异，不作为回归上报。
		if isExternalProbeURL(response.URL()) {
			return
		}
		errors.recordError(fmt.Sprintf("httpstatus: %s (%d)", response.URL(), response.Status()))
	})
}

// assertNoE2EConsoleErrors 断言当前页面未产生控制台错误；有错误时截图并失败退出。
func assertNoE2EConsoleErrors(t *testing.T, page playwright.Page, errors *e2eConsoleErrors) {
	t.Helper()
	// current 是与本页面关联的错误快照，非空即代表用户动线出现运行时异常。
	current := errors.collect()
	if len(current) == 0 {
		return
	}
	failE2EWithScreenshot(t, page, fmt.Sprintf("页面出现控制台/运行时错误: %s", strings.Join(current, "; ")))
}

// failE2EWithScreenshot 把失败页面截图留存、打印原因并终止测试。
func failE2EWithScreenshot(t *testing.T, page playwright.Page, message string) {
	t.Helper()
	// pageSummary 是失败页面的可见文本摘要，便于直接定位登录或渲染错误。
	if pageSummary, evalErr := page.Evaluate(`() => document.body.innerText.slice(0, 500)`); evalErr == nil {
		message = fmt.Sprintf("%s\n页面可见文本: %v", message, pageSummary)
	}
	// shotPath 是失败现场截图的目标路径，随测试临时目录一起清理。
	shotPath := filepath.Join(t.TempDir(), fmt.Sprintf("webui-e2e-failure-%d.png", time.Now().UnixNano()))
	// shotErr 保存失败截图的结果；截图失败不阻断主失败信息输出。
	_, shotErr := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(shotPath)})
	if shotErr == nil {
		t.Logf("失败现场截图: %s", shotPath)
	} else {
		t.Logf("失败现场截图失败: %v", shotErr)
	}
	t.Fatal(message)
}

// waitE2EForSelector 等待页面上出现给定标签；props 为可选的等待选项。
func waitE2EForSelector(t *testing.T, page playwright.Page, selector string, timeout time.Duration) playwright.Locator {
	t.Helper()
	// locator 是待等待的目标元素定位器，复用同一实例完成可见性等待。
	locator := page.Locator(selector)
	// waitErr 保存元素可见性等待结果，超时说明目标元素未按预期出现在页面上。
	waitErr := locator.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(float64(timeout.Milliseconds()))})
	if waitErr != nil {
		failE2EWithScreenshot(t, page, fmt.Sprintf("等待元素 %q 超时: %v", selector, waitErr))
	}
	return locator
}

// waitE2EForURLBriefly 等待页面地址包含预期路径片段并返回实际地址。
func waitE2EForURLBriefly(t *testing.T, page playwright.Page, pathPart string) {
	t.Helper()
	// deadline 是路由等待的截止时间。
	deadline := time.Now().Add(e2eNavigateTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(page.URL(), pathPart) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	failE2EWithScreenshot(t, page, fmt.Sprintf("页面地址未到达 %q，当前 %q", pathPart, page.URL()))
}

// e2EReactMounted 通过 DOM 结构判断 React 应用是否成功挂载并渲染内容。
func e2EReactMounted(t *testing.T, page playwright.Page) bool {
	t.Helper()
	// mountedFromDOM 是页面自身计算的挂载判定；返回布尔供 Go 侧断言。
	mountedFromDOM, evalErr := page.Evaluate(`() => {
		const root = document.querySelector('#root');
		return Boolean(root && root.children.length > 0 && document.body.innerText.trim().length > 0);
	}`)
	if evalErr != nil {
		t.Logf("评估 DOM 挂载状态失败: %v", evalErr)
		return false
	}
	return mountedFromDOM != nil && mountedFromDOM.(bool)
}

// executeE2ELogin 在登录表单填写管理员凭证并提交，等待会话进入默认仪表盘。
func executeE2ELogin(t *testing.T, page playwright.Page, errors *e2eConsoleErrors) {
	t.Helper()
	// formLocator 定位认证表单，作为账号与密码输入的唯一作用域。
	formLocator := waitE2EForSelector(t, page, "form", e2eNavigateTimeout)
	// inputs 是表单内的全部输入框，顺序与界面渲染顺序一致（账号、密码）。
	inputs := formLocator.Locator("input")
	inputs.Nth(0).Fill("admin")
	inputs.Nth(1).Fill(e2eAdminPassword)
	// submit 是表单的提交按钮，点击后触发 SPA 登录请求。
	submit := formLocator.Locator("button[type=submit]")
	// fillErr 保存登录按钮点击错误；点击失败说明认证表单结构不符合预期。
	fillErr := submit.First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
	if fillErr != nil {
		failE2EWithScreenshot(t, page, fmt.Sprintf("点击登录按钮失败: %v", fillErr))
	}
	// 登录成功后认证表单卸载，SPA 不强制改写地址栏为规范路径，因此以表单消失与应用挂载判定会话建立。
	waitE2ELoggedIn(t, page)
	assertNoE2EConsoleErrors(t, page, errors)
}

// waitE2ELoggedIn 轮询直到登录表单卸载且 React 应用挂载，判定管理员会话已建立。
func waitE2ELoggedIn(t *testing.T, page playwright.Page) {
	t.Helper()
	// deadline 是会话建立的等待截止时间。
	deadline := time.Now().Add(e2eNavigateTimeout)
	for time.Now().Before(deadline) {
		// passwordCount 是认证表单中的密码输入框数量；登录成功后表单整体卸载。
		passwordCount, countErr := page.Locator("input[type=password]").Count()
		if countErr == nil && passwordCount == 0 && e2EReactMounted(t, page) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	failE2EWithScreenshot(t, page, "登录提交后未进入业务界面（认证表单未卸载）")
}

// assertE2EPageRendered 导航到指定页面并断言 React 挂载、路由到达与无控制台错误。
func assertE2EPageRendered(t *testing.T, page playwright.Page, errors *e2eConsoleErrors, path string) {
	t.Helper()
	// gotoErr 保存页面导航错误；导航失败说明路径或静态资源加载异常。
	_, gotoErr := page.Goto(e2eBaseURL+path, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if gotoErr != nil {
		failE2EWithScreenshot(t, page, fmt.Sprintf("导航到 %s 失败: %v", path, gotoErr))
	}
	waitE2EForURLBriefly(t, page, path)
	// 等待异步业务请求落地，避免下一页导航中止进行中请求而产出导航噪声错误。
	time.Sleep(1 * time.Second)
	// 整页导航后 SPA 重新加载并异步校验会话，先轮询等待 React 挂载完成再断言。
	waitE2ERendered(t, page, path)
	assertNoE2EConsoleErrors(t, page, errors)
}

// waitE2ERendered 轮询直到 React 应用真正挂载出内容，避免整页导航后的渲染竞态。
func waitE2ERendered(t *testing.T, page playwright.Page, path string) {
	t.Helper()
	// deadline 是页面可交互状态的等待截止时间。
	deadline := time.Now().Add(e2eNavigateTimeout)
	for time.Now().Before(deadline) {
		if e2EReactMounted(t, page) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	failE2EWithScreenshot(t, page, fmt.Sprintf("页面 %s 未渲染出内容（疑似白屏）", path))
}

// resolveE2EServerBin 解析被测 server 二进制路径：优先环境变量，其次系统 PATH。
func resolveE2EServerBin(t *testing.T) string {
	t.Helper()
	// fromEnv 是调用方显式指定的 server 二进制路径（容器内通常为 /app/xianyu-server）。
	if fromEnv := strings.TrimSpace(os.Getenv("E2E_SERVER_BIN")); fromEnv != "" {
		return fromEnv
	}
	// resolvedPath 是从 PATH 中解析出的 server 可执行文件；缺失时给出明确指引。
	resolvedPath, lookErr := exec.LookPath("xianyu-server")
	if lookErr != nil {
		t.Fatal("未找到被测 server 二进制，请通过 E2E_SERVER_BIN 指定（容器内为 /app/xianyu-server）")
	}
	return resolvedPath
}

// TestWebUISmokeAdminFlow 覆盖 Web UI 核心用户动线：初始化管理员、登录与关键页面渲染。
func TestWebUISmokeAdminFlow(t *testing.T) {
	if os.Getenv("RUN_WEBUI_E2E") != "1" {
		t.Skip("设置 RUN_WEBUI_E2E=1 以执行 Web UI 真实浏览器冒烟测试")
	}
	// serverBin 是本次 e2e 被测的 server 可执行文件。
	serverBin := resolveE2EServerBin(t)
	// dbPath 是本测试独立的 SQLite 数据库文件，避免污染现有数据。
	dbPath := filepath.Join(t.TempDir(), "e2e.db")
	runE2EServerOnce(t, serverBin, dbPath)
	// 启动函数内部已注册测试清理，测试退出时自动回收常驻 server 进程。
	startE2EServer(t, serverBin, dbPath)
	waitE2EHealth(t)

	// manager 是浏览器运行时管理器，负责 Chromium 的初始化与释放。
	manager := NewManager(nil)
	defer manager.Close()
	// initErr 保存浏览器运行时就绪结果；失败说明 runtime 目录或 driver 不可用。
	initErr := manager.init()
	if initErr != nil {
		t.Fatalf("浏览器运行时初始化失败: %v", initErr)
	}
	// chromium 是本次冒烟使用的真实 Chromium 实例。
	chromium, launchErr := manager.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if launchErr != nil {
		t.Fatalf("启动 Chromium 失败: %v", launchErr)
	}
	defer func() { _ = chromium.Close() }()
	// context 是隔离的浏览器上下文，登录会话与其他站点数据互不干扰。
	context, contextErr := chromium.NewContext()
	if contextErr != nil {
		t.Fatalf("创建浏览器上下文失败: %v", contextErr)
	}
	defer func() { _ = context.Close() }()
	// page 是本次动线复用的一页，会话保持登录态贯穿全部用例。
	page, pageErr := context.NewPage()
	if pageErr != nil {
		t.Fatalf("创建页面失败: %v", pageErr)
	}
	// errors 收集整个过程所有页面的运行时错误。
	errors := &e2eConsoleErrors{}
	attachE2EErrorCollection(page, errors)

	// 未登录访问首页应先呈现登录表单，再完成管理员登录。
	if _, gotoErr := page.Goto(e2eBaseURL+"/", playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); gotoErr != nil {
		t.Fatalf("访问首页失败: %v", gotoErr)
	}
	waitE2EForSelector(t, page, "input[type=password]", e2eNavigateTimeout)
	executeE2ELogin(t, page, errors)

	// 关键页面逐页冒烟；每个页面断言 React 挂载、路由停留与无控制台错误。
	assertE2EPageRendered(t, page, errors, "/app/dashboard")
	assertE2EPageRendered(t, page, errors, "/app/accounts")
	// 规则页是历史白屏回归的高风险页面，必须在页面级断言中保持无错误。
	assertE2EPageRendered(t, page, errors, "/app/rules")
	assertE2EPageRendered(t, page, errors, "/app/cards")
	assertE2EPageRendered(t, page, errors, "/app/orders")
	// 设置页仅管理员可进入，进入成功即证明管理员会话与路由权限均正常。
	assertE2EPageRendered(t, page, errors, "/app/settings")
}