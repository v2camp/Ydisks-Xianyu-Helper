import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

// lazyPageNames 是产生独立分片的页面名唯一来源，懒加载断言与分片预算共用同一份清单。
import { lazyPageNames } from './app/router/pageEntries';

// lazyPagePattern 由页面清单派生，避免手写正则漏掉页面。
const lazyPagePattern = new RegExp(`const (?:${lazyPageNames.join('|')}) = lazy\\(`, 'g');

const repoRoot = resolve(__dirname); /* repoRoot 表示repoRoot。 */

const readFrontendFile = (relativePath: string) =>
  readFileSync(resolve(repoRoot, relativePath), 'utf8'); /* readFrontendFile 表示readFrontendFile。 */

const extractSingleQuotedValues = (source: string, pattern: RegExp) => {
  const values = new Set<string>(); /* values 表示值集合。 */
  for (const match /* match 表示匹配结果。 */ of source.matchAll(pattern)) {
    values.add(match[1]);
  }
  return values;
}; /* extractSingleQuotedValues 表示extractSingleQuoted值集合。 */

describe('frontend navigation routing', () => {
  test('sidebar entries and App activeTab routes stay in sync', () => {
    const sidebar = readFrontendFile('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */
    const app = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* app 表示认证后页面组合器源码。 */

    const sidebarIDs = extractSingleQuotedValues(sidebar, /id:\s*'([^']+)'/g); /* sidebarIDs 表示sidebarIDs。 */
    const appRouteIDs = extractSingleQuotedValues(app, /case\s+'([^']+)'/g); /* appRouteIDs 表示appRouteIDs。 */

    expect([...sidebarIDs].sort()).toEqual([...appRouteIDs].sort());
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('active navigation uses the primary action blue', () => {
    const sidebar = readFrontendFile('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */

    expect(sidebar).toContain("'bg-brand text-white shadow-brand-active'");
    expect(sidebar).not.toContain("'bg-sky-500 text-white'");
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('route pages are lazy-loaded behind a shared Suspense boundary', () => {
    const app = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* app 表示认证后页面组合器源码。 */
    const lazyPageCount = (app.match(lazyPagePattern) || []).length; /* lazyPageCount 表示按路由懒加载的页面数量，期望值与页面清单长度一致。 */

    expect(lazyPageCount).toBe(14);
    expect(app).toContain('const PageLoading: React.FC');
    expect(app).toContain('<Suspense fallback={<PageLoading />}>');
    expect(app).not.toContain("import Dashboard from '../../components/Dashboard'");
    expect(app).toContain("import('../features/dashboard/pages/Dashboard')");
    expect(app).toContain("import('../features/settings/pages/CustomerAgent')");
    expect(app).toContain("import('../features/settings/pages/OpsAgent')");
    expect(app).toContain("import('../features/connectors/pages/NotificationCenter')");
    expect(app).toContain("import('../features/connectors/pages/SystemAlerts')");
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('authenticated shell owns sidebar and page composition', () => {
    const app = readFrontendFile('app/router/AppRouter.tsx'); /* app 表示认证后的路由组合器源码。 */
    const shell = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* shell 表示认证后应用壳源码。 */

    expect(app).toContain("import AuthenticatedShell, { type DeliveryRuleTarget }");
    expect(app).not.toContain('const renderContent = () =>');
    expect(shell).toContain('const AuthenticatedShell: React.FC<AuthenticatedShellProps>');
    expect(shell).toContain('<Sidebar');
    expect(shell).toContain('<AppContent');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('logout button invalidates the backend session before clearing UI state', () => {
    const app = readFrontendFile('app/router/AppRouter.tsx'); /* app 表示认证后路由组合器源码。 */
    const sessionProvider = readFrontendFile('app/providers/SessionProvider.tsx'); /* sessionProvider 表示认证 Provider 源码。 */

    expect(app).toContain("import { useSession }");
    expect(sessionProvider).toContain('await logout();');
    expect(sessionProvider).toContain("window.addEventListener('auth:logout'");
    expect(app).toContain('onLogout={handleLogout}');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('settings page does not expose backend-inactive system controls', () => {
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示settings。 */
    const settingsConstants = readFrontendFile('app/features/settings/constants.ts'); /* settingsConstants 表示settingsConstants。 */

    expect(settings).not.toContain('允许用户注册');
    expect(settings).not.toContain('显示默认登录信息');
    expect(settings).not.toContain('启用商品自动同步');
    expect(settings).not.toContain('商品同步间隔');
    expect(settings).not.toContain('默认自动回复内容');
    expect(settingsConstants).toContain('SETTINGS_SAVE_OMIT_KEYS');
    expect(settings).toContain('保存后需重启服务生效');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('chat page exposes the browser notification switch', () => {
    const chat = readFrontendFile('app/features/chat/components/BrowserNotificationToggle.tsx'); /* chat 表示聊天页通知入口源码，用于确认普通用户可见。 */
    const chatPage = readFrontendFile('app/features/chat/pages/Chat.tsx'); /* chatPage 表示聊天页组合源码，用于确认开关实际挂载。 */
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示设置页源码，用于确认入口已迁移。 */

    expect(chat).toContain('新消息系统通知');
    expect(chat).toContain('role="switch"');
    expect(chat).toContain('useBrowserNotificationPreference');
    expect(chatPage).toContain("import BrowserNotificationToggle from '../components/BrowserNotificationToggle'");
    expect(chatPage).toContain('<BrowserNotificationToggle />');
    expect(settings).not.toContain('新消息系统通知');
  } /* 测试回调断言聊天页包含浏览器通知开关且管理员设置页不再重复展示。 */);

  test('admin-only settings navigation is gated by session role', () => {
    const app = readFrontendFile('app/router/AppRouter.tsx'); /* app 表示认证后路由组合器源码。 */
    const sessionProvider = readFrontendFile('app/providers/SessionProvider.tsx'); /* sessionProvider 表示认证 Provider 源码。 */
    const shell = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* shell 表示认证后应用壳源码。 */
    const sidebar = readFrontendFile('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */
    const settingsHook = readFrontendFile('app/features/settings/hooks.ts'); /* settingsHook 表示settingsHook。 */

    expect(app).toContain('const { isLoggedIn, isAdmin, signOut } = useSession();');
    expect(sessionProvider).toContain('setIsAdmin(response.is_admin === true)');
    expect(app).toContain("const adminOnlyRoutes: readonly AppRoute[] = ['settings', 'customer-agent', 'system-alerts'];");
    expect(app).toContain('adminOnlyRoutes.includes(activeRoute)');
    expect(app).toContain('adminOnlyRoutes.includes(route)');
    expect(shell).toContain('isAdmin ? <Settings /> : <Dashboard />');
    expect(shell).toContain('isAdmin ? <CustomerAgent onNavigate={onNavigate} /> : <Dashboard />');
    // 运营 Agent 属于所有登录用户，壳层不再按管理员分流。
    expect(shell).toContain('case \'ops-agent\': return <OpsAgent onNavigate={onNavigate} />;');
    expect(sidebar).toContain('isAdmin = false');
    expect(sidebar).toContain("...(isAdmin ? [{ id: 'system-alerts'");
    expect(sidebar).toContain("{ id: 'settings', icon: Settings, label: '系统设置' }");
    expect(sidebar).toContain("{ id: 'customer-agent', icon: Bot, label: '客服 Agent' }");
    // 通知中心在通用菜单中，所有登录用户都可见。
    expect(sidebar).toContain("{ id: 'notifications', icon: Bell, label: '通知中心' },");
    // 运营 Agent 在通用菜单中，不再受管理员菜单条件限制。
    expect(sidebar).toContain("{ id: 'ops-agent', icon: Radio, label: '运营 Agent' },");
    expect(settingsHook).toContain('setLoadError');
    expect(settingsHook).toContain('buildPersistableSettings(settings, scope)');
    // 模型与密钥已迁到连接器页，系统设置 Hook 不再发起模型发现或连接测试。
    expect(settingsHook).not.toContain('loadAIModels');
    expect(settingsHook).not.toContain('testConnection');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('admin configuration pages each own a distinct route mapping', () => {
    const routes = readFrontendFile('app/router/routes.ts'); /* routes 表示路由映射源码。 */

    expect(routes).toContain("'customer-agent'");
    expect(routes).toContain("'/app/customer-agent': 'customer-agent'");
    expect(routes).toContain("'/app/settings': 'settings'");
    expect(routes).toContain("'/app/ops-agent': 'ops-agent'");
  } /* 测试回调断言系统设置、客服 Agent 与运营 Agent 各自拥有独立路由映射。 */);

  test('inbound MCP service card is mounted on the system settings page', () => {
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示系统设置页源码，用于确认入站 MCP 卡片已回归。 */
    const customerAgent = readFrontendFile('app/features/settings/pages/CustomerAgent.tsx'); /* customerAgent 表示客服 Agent 页源码，用于确认入站卡片不再挂载。 */

    expect(settings).toContain("import { MCPServiceCard } from '../components/MCPServiceCard'");
    expect(settings).toContain('<MCPServiceCard />');
    expect(settings).toContain('对外 MCP 服务（入站）');
    expect(customerAgent).toContain('<MCPServerList');
    expect(customerAgent).toContain('mcp.servers');
    expect(customerAgent).not.toContain('<MCPServiceCard');
  } /* 测试回调断言入站 MCP 服务卡回归系统设置页，客服 Agent 页只保留出站工具配置。 */);

  test('captcha remote settings expose the reference privacy and fallback semantics', () => {
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示settings。 */

    expect(settings).toContain('远程过滑块配置');
    expect(settings).toContain("'captcha.remote_service_url'");
    expect(settings).toContain("'captcha.remote_secret_key'");
    expect(settings).toContain("'captcha.remote_pass_cookies'");
    expect(settings).toContain('默认关闭');
    expect(settings).toContain('只有网络不可用或超时才回退本机引擎');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('settings page exposes the QQ connector guide for official bot credentials', () => {
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示系统设置页源码，用于确认连接器已移出。 */
    const connectors = readFrontendFile('app/features/connectors/pages/Connectors.tsx'); /* connectors 表示连接器页源码，用于确认引导挂载。 */
    const registry = readFrontendFile('app/features/connectors/registry.ts'); /* registry 表示连接器注册表源码，用于确认凭据字段来源。 */
    const card = readFrontendFile('app/features/connectors/components/ConnectorCard.tsx'); /* card 表示注册表驱动的连接器卡源码。 */
    const connectorsApi = readFrontendFile('app/features/connectors/api.ts'); /* connectorsApi 表示连接器 feature 的契约适配器源码。 */

    expect(connectors).toContain('连接器');
    expect(connectors).toContain("import { ConnectorCard } from '../components/ConnectorCard'");
    expect(connectors).toContain('<ConnectorCard');
    expect(settings).not.toContain('QQConnectorCard');
    expect(registry).toContain('https://q.qq.com/');
    expect(registry).toContain('机器人 AppID');
    expect(registry).toContain('机器人 AppSecret');
    expect(registry).toContain("'qqbot.app_secret'");
    // QQ 键名仍统一引用共享契约常量，但注册表必须经本 feature 的 api 适配器取用，不得直连 shared/api-contract。
    expect(registry).toContain('QQ_CONNECTOR_READY_KEYS.appId');
    expect(registry).toContain('QQ_CONNECTOR_READY_KEYS.appSecretConfigured');
    expect(registry).toContain("from './api'");
    expect(registry).not.toContain('shared/api-contract/');
    expect(connectorsApi).toContain("export { QQ_CONNECTOR_READY_KEYS } from '../../../shared/api-contract/settings'");
    expect(card).toContain('connector.fields.map');
  } /* 测试回调断言连接器页引导管理员前往 QQ 开放平台配置官方机器人凭据。 */);

  test('QQ inbound commands are configured on the ops agent page instead of the connector', () => {
    const registry = readFrontendFile('app/features/connectors/registry.ts'); /* registry 表示连接器注册表源码。 */
    const opsAgent = readFrontendFile('app/features/settings/pages/OpsAgent.tsx'); /* opsAgent 表示运营 Agent 页源码。 */
    const settingsConstants = readFrontendFile('app/features/settings/constants.ts'); /* settingsConstants 表示设置页字段白名单。 */

    expect(registry).not.toContain('启用入站命令');
    expect(registry).not.toContain("'qqbot.commands_enabled'");
    expect(registry).not.toContain("'qqbot.command_openids'");
    expect(settingsConstants).toContain('OPS_AGENT_SETTING_KEYS');
    expect(settingsConstants).toContain("'qqbot.commands_enabled'");
    expect(settingsConstants).toContain("'qqbot.command_openids'");
    expect(opsAgent).toContain("'qqbot.commands_enabled'");
    expect(opsAgent).toContain("'qqbot.command_openids'");
    expect(opsAgent).toContain('启用运营 Agent（QQ 入站）');
  } /* 测试回调断言 QQ 入站命令开关与白名单已从连接器迁到运营 Agent 页。 */);

  test('email notification config uses the current user integration SMTP', () => {
    const notifications = readFrontendFile('app/features/connectors/pages/Connectors.tsx'); /* notifications 表示连接器页源码。 */
    const notificationState = readFrontendFile('app/features/connectors/state.ts'); /* notificationState 表示连接器页纯函数源码。 */
    const notificationModal = readFrontendFile('app/features/connectors/components/NotificationChannelModal.tsx'); /* notificationModal 表示渠道编辑弹窗源码。 */

    // SMTP 面板与用户集成配置都直接渲染给当前用户，不再有管理员门禁。
    expect(notifications).toContain('<NotificationSmtpSettings');
    expect(notifications).toContain('useUserIntegrations');
    expect(notifications).not.toContain('isAdmin');
    expect(notificationState).toContain("key: 'to_email'");
    expect(notificationModal).toContain('邮件渠道使用');
    // 渠道弹窗不再出现独立 SMTP 模式或 SMTP 凭据字段。
    expect(notificationModal).not.toContain('use_custom_smtp');
    expect(notificationState).not.toContain("key: 'from'");
    expect(notifications).not.toContain('注册验证码');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('keyword reply UI matches contains-based backend behavior', () => {
    const rules = readFrontendFile('app/features/rules/pages/Rules.tsx'); /* rules 表示规则集合。 */

    expect(rules).toContain('包含匹配');
    expect(rules).not.toContain('精确匹配');
    expect(rules).not.toContain('模糊包含');
    expect(rules).not.toContain('匹配类型');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('vite proxy does not advertise removed backend routes', () => {
    const vite = readFrontendFile('vite.config.ts'); /* vite 表示vite。 */

    for (const staleRoute /* staleRoute 表示staleRoute。 */ of [
      '/backup',
      '/logs',
      '/register',
      '/generate-captcha',
      '/verify-captcha',
      '/geetest',
      '/send-verification-code',
    ]) {
      expect(vite).not.toContain(`'${staleRoute}'`);
    }
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('item delivery shortcut opens existing automation rule modal', () => {
    const actions = readFrontendFile('app/features/rules/ruleActions.ts'); /* actions 表示规则动作协调器源码。 */
    const existingRuleBranch = actions.match(/if \(rule\) \{([\s\S]*?)\} else \{/); /* existingRuleBranch 表示existing当前规则Branch。 */

    expect(existingRuleBranch?.[1]).toContain('openAutomationRule(rule)');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('item delivery shortcut is not marked handled before async open completes', () => {
    const actions = readFrontendFile('app/features/rules/ruleActions.ts'); /* actions 表示规则动作协调器源码。 */

    expect(actions).not.toContain('handledDeliveryTarget.current = initialDeliveryTarget.requestId');
    expect(actions).toContain('onDeliveryTargetHandled?.();');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('automation editor keeps multiple delivery contents for normal products', () => {
    const rules = readFrontendFile('app/features/rules/pages/Rules.tsx'); /* rules 表示规则集合。 */
    const actions = readFrontendFile('app/features/rules/ruleActions.ts'); /* actions 表示规则动作协调器源码。 */

    expect(rules).toContain('添加发货内容');
    expect(rules).toContain('{displayVariants.map((variant, index) => (');
    expect(actions).toContain('variants.map(');
    expect(rules).not.toContain(': (isMultiSpecRule ? variants : [variants[0]]).map');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('batch publishing help explains card fields without required-field jargon', () => {
    const itemList = readFrontendFile('app/features/items/pages/ItemList.tsx'); /* itemList 表示当前商品List。 */

    expect(itemList).not.toContain('条件必填');
    expect(itemList).toContain('“付款后发送的卡密”怎么填');
    expect(itemList).toContain('101:1:0;102:2:3');
    expect(itemList).toContain('买家购买 3 件时会发送 6 份');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('item publish image previews revoke object urls', () => {
    const itemList = readFrontendFile('app/features/items/itemActions.ts'); /* itemList 表示商品动作协调器源码。 */

    expect(itemList).toContain('setPublishImagePreviews');
    expect(itemList).toContain('URL.createObjectURL(file)');
    expect(itemList).toContain('URL.revokeObjectURL(preview.url)');
    expect(itemList).not.toContain('src={URL.createObjectURL(file)}');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('QR verification removes the external link and clearly requires in-app risk verification', () => {
    const accounts = readFrontendFile('app/features/accounts/pages/AccountList.tsx'); /* accounts 表示账号集合。 */
    const qrModal = readFrontendFile('app/features/accounts/components/AccountQRCodeModal.tsx'); /* qrModal 表示二维码登录弹窗。 */
	const riskPanel = readFrontendFile('app/features/accounts/components/RiskVerificationPanel.tsx'); /* riskPanel 表示riskPanel。 */
    expect(accounts).not.toContain('href={verificationUrl}');
    expect(accounts).not.toContain('setVerificationUrl');
	expect(qrModal).toContain('RiskVerificationPanel');
	expect(riskPanel).toContain('需要完成安全风控验证');
	expect(riskPanel).toContain('请勿在浏览器中打开验证链接');
		expect(riskPanel).toContain('系统会自动检测并刷新登录状态');
		expect(riskPanel).not.toContain('我已在闲鱼 App 完成验证');
		expect(riskPanel).not.toContain('<button');
	expect(riskPanel).not.toContain('重试');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('account editor exposes password-login refresh and never renders its verification URL', () => {
    const accounts = [
      readFrontendFile('app/features/accounts/pages/AccountList.tsx'),
      readFrontendFile('app/features/accounts/components/AccountEditModal.tsx'),
      readFrontendFile('app/features/accounts/submoduleHooks.ts'),
    ].join('\n'); /* accounts 表示账号集合。 */
    expect(accounts).toContain('passwordLogin({');
	expect(accounts).toContain('checkPasswordLoginStatus(sessionId, controller.signal)');
    expect(accounts).toContain('密码登录刷新授权');
    expect(accounts).toContain('账号已触发平台风控，需要完成人脸识别');
    expect(accounts).not.toContain('result.verification_url');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);
} /* 测试套件回调汇总路由装配与访问边界契约。 */);
