import { expect,test } from 'vitest';
import type { NotificationChannel } from './api';
import { qqBotConnector } from './registry';
import { buildConnectorSettings,buildNotificationPayload,connectorReady,credentialKeysOf,emptyNotificationForm,isCurrentNotificationRequest,normalizeNotificationForm,notificationChannelTypes,notificationErrorMessage,notificationEventSummary,notificationEvents,resolveCredentialSource,validateNotificationForm } from './state';
import type { NotificationForm } from './types';

// createForm 创建通知渠道校验使用的最小表单对象。
const createForm = (overrides: Partial<NotificationForm> = {}): NotificationForm => ({
  name: '测试渠道', type: 'bark', enabled: true, config: { server_url: 'https://api.day.app', device_key: 'device-key' }, event_types: [], ...overrides,
});

test('通知渠道校验阻止缺少名称和必填配置',
  // 渠道表单测试验证名称、渠道凭据和成功保存请求体。
  () => {
    expect(validateNotificationForm(createForm({ name: '' }))).toBe('请填写渠道名称');
    expect(validateNotificationForm(createForm({ config: {} }))).toContain('Bark 服务器');
    expect(validateNotificationForm(createForm())).toBe('');
    expect(buildNotificationPayload(createForm({ event_types: ['account_offline'] })).event_types).toEqual(['account_offline']);
  });

test('通知事件摘要为空时表示订阅全部事件',
  // 事件摘要测试验证空选择和多事件选择的展示语义。
  () => {
    expect(notificationEventSummary([])).toBe('全部事件');
    expect(notificationEventSummary(['account_offline', 'system_error'])).toBe('掉线通知、系统错误');
  });

test('旧版统一交易开关归一为自动化与人工处理开关',
  // 兼容测试验证旧渠道编辑后会展开四个自动化事件及人工处理事件。
  () => {
  // legacyChannel 是保存过旧版交易事件编码的渠道摘要。
  const legacyChannel = { id: 'legacy-channel', ...createForm({ event_types: ['delivery_result'] }) } as NotificationChannel;
  // normalized 是编辑器展示的细分事件集合。
  const normalized = normalizeNotificationForm(legacyChannel, {});
  expect(normalized.event_types).toEqual(expect.arrayContaining([
    'automation_order_created', 'automation_order_paid', 'automation_buyer_reviewed', 'automation_review_missing_timeout',
    'manual_delivery_result', 'manual_intervention_required',
  ]));
  expect(notificationEvents.filter(
    // event 是当前通知事件定义，用于统计自动化任务类别数量。
    event => event.value.startsWith('automation_'),
  )).toHaveLength(4);
  expect(notificationEventSummary(['manual_intervention_required'])).toBe('需要人工处理');
  });

test('通知请求代次拒绝过期响应',
  // 过期响应测试验证刷新或取消后旧请求不能覆盖当前状态。
  () => {
    expect(isCurrentNotificationRequest(5, 5)).toBe(true);
    expect(isCurrentNotificationRequest(4, 5)).toBe(false);
  });

test('通知表单覆盖各渠道归一化和独立 SMTP 校验',
  // 表单分支测试验证非邮件、继承 SMTP 和独立 SMTP 三条保存路径。
  () => {
    expect(emptyNotificationForm()).toMatchObject({ name: '', type: 'bark', enabled: true, config: {}, event_types: [] });
    // webhook 是非邮件渠道配置复制测试使用的渠道对象。
    const webhook = { id: 'webhook-1', name: 'Webhook', type: 'webhook', config: { webhook_url: 'https://example.com' }, enabled: false } as NotificationChannel;
    // smtp 是继承或补全独立 SMTP 配置时使用的系统设置。
    const smtp = { smtp_server: 'smtp.example.com', smtp_port: 465, smtp_user: 'sender@example.com', smtp_password: 'secret', smtp_from_address: 'sender@example.com' };
    expect(normalizeNotificationForm(webhook, smtp)).toMatchObject({ name: 'Webhook', type: 'webhook', enabled: false, config: { webhook_url: 'https://example.com' }, event_types: [] });
    // inherited 是未开启独立 SMTP 时的邮件渠道表单。
    const inherited = normalizeNotificationForm({ id: 'email-1', name: '邮件', type: 'email', config: { to_email: 'to@example.com' }, enabled: true }, smtp);
    expect(inherited.config).toMatchObject({ to_email: 'to@example.com', use_custom_smtp: false });
    expect(validateNotificationForm({ ...createForm(), type: 'email', config: { to_email: 'to@example.com', use_custom_smtp: true } })).toBe('请填写 独立 SMTP 服务器');
    // custom 是字段完整的独立 SMTP 邮件渠道表单。
    const custom = { ...createForm(), type: 'email' as const, config: { to_email: 'to@example.com', use_custom_smtp: true, smtp_server: 'smtp.example.com', smtp_port: 465, smtp_user: 'sender@example.com', smtp_password: 'secret', smtp_from_address: 'sender@example.com' } };
    expect(validateNotificationForm(custom)).toBe('');
    expect(buildNotificationPayload(custom).config).toMatchObject({ to_email: 'to@example.com', use_custom_smtp: true });
    expect(notificationErrorMessage(new Error('网络失败'), '备用错误')).toBe('网络失败');
    expect(notificationErrorMessage({}, '备用错误')).toBe('备用错误');
  });

// 原有 SMTP 保留模式只发送收件地址，明确重新配置时才发送完整 SMTP 配置。
test('邮件编辑保留现有 SMTP，显式替换仍需完整配置', /* preservedSMTPTest 验证表单状态与请求载荷边界。 */ () => {
  // form 模拟旧版独立 SMTP 渠道，只在浏览器内保存非秘密编辑值。
  const form = createForm({ type: 'email', preserveSMTP: true, config: { to_email: 'new@example.com', use_custom_smtp: true } });
  expect(validateNotificationForm(form)).toBe('');
  expect(buildNotificationPayload(form)).toMatchObject({ email_recipient: 'new@example.com' });
  expect(buildNotificationPayload(form).config).toBeUndefined();
  expect(validateNotificationForm({ ...form, preserveSMTP: false })).toContain('SMTP 服务器');
  expect(validateNotificationForm({ ...form, config: {} })).toContain('收件邮箱');
  expect(buildNotificationPayload({ ...form, preserveSMTP: false, config: { to_email: 'new@example.com', use_custom_smtp: false } })).toMatchObject({ config: { to_email: 'new@example.com', use_custom_smtp: false } });
});

// 连接器就绪判定要求非敏感凭据非空，且敏感凭据已配置或本次新填。
test('连接器就绪判定覆盖缺凭据、已配置与服务端不回显三种状态',
  // 就绪测试验证 AppID 与 AppSecret 的必需组合。
  () => {
    expect(connectorReady(qqBotConnector, {})).toBe(false);
    expect(connectorReady(qqBotConnector, { 'qqbot.app_id': '102012345' })).toBe(false);
    expect(connectorReady(qqBotConnector, { 'qqbot.app_id': '102012345', 'qqbot.app_secret_configured': true })).toBe(true);
    expect(connectorReady(qqBotConnector, { 'qqbot.app_id': '102012345', 'qqbot.app_secret': 'bot-secret-plain' })).toBe(true);
  });

// 连接器保存载荷只提交注册表登记的键，敏感键留空时保持服务端原值。
test('连接器保存载荷收窄到注册表键并保留未改动的敏感值',
  // 载荷测试验证敏感键的三态语义与未登记键的过滤。
  () => {
    expect(buildConnectorSettings([qqBotConnector], { 'qqbot.app_id': '102012345', unrelated: 'drop-me' }))
      .toEqual({ 'qqbot.app_id': '102012345' });
    expect(buildConnectorSettings([qqBotConnector], { 'qqbot.app_id': '102012345', 'qqbot.app_secret': 'bot-secret-plain' }))
      .toEqual({ 'qqbot.app_id': '102012345', 'qqbot.app_secret': 'bot-secret-plain' });
    expect(buildConnectorSettings([qqBotConnector], { 'qqbot.commands_enabled': true, 'qqbot.command_openids': 'openid-a, openid-b' }))
      .toEqual({ 'qqbot.commands_enabled': true, 'qqbot.command_openids': 'openid-a, openid-b' });
    expect(buildConnectorSettings([qqBotConnector], {})).toEqual({});
  });

// 各渠道类型声明的凭据键必须与后端敏感键一致，否则必填豁免与显式清除会作用在错误的键上。
test('各渠道类型声明各自的凭据键与敏感标记',
  // 元数据测试验证凭据键集合、密码控件与敏感字段标记。
  () => {
    expect(credentialKeysOf('qq')).toEqual(['app_id', 'app_secret']);
    expect(credentialKeysOf('bark')).toEqual(['device_key']);
    expect(credentialKeysOf('dingtalk')).toEqual(['webhook_url', 'secret']);
    expect(credentialKeysOf('feishu')).toEqual(['webhook_url', 'secret']);
    expect(credentialKeysOf('wechat')).toEqual(['webhook_url']);
    expect(credentialKeysOf('webhook')).toEqual(['webhook_url']);
    expect(credentialKeysOf('telegram')).toEqual(['bot_token']);
    expect(credentialKeysOf('email')).toEqual([]);
    // fieldOf 按渠道类型和字段键读取字段定义。
    const fieldOf = (type: keyof typeof notificationChannelTypes, key: string) => notificationChannelTypes[type].fields.find(
      // fieldMatcher 匹配当前待断言的字段键。
      field => field.key === key,
    );
    expect(fieldOf('bark', 'device_key')?.sensitive).toBe(true);
    expect(fieldOf('wechat', 'webhook_url')?.sensitive).toBe(true);
    expect(fieldOf('telegram', 'bot_token')?.sensitive).toBe(true);
    expect(fieldOf('dingtalk', 'secret')).toMatchObject({ sensitive: true, type: 'password' });
    expect(fieldOf('feishu', 'secret')).toMatchObject({ sensitive: true, type: 'password' });
    // 非凭据字段不应被标记为敏感，否则会被误当作需要留空沿用。
    expect(fieldOf('bark', 'server_url')?.sensitive).toBeUndefined();
    expect(fieldOf('qq', 'user_openid')?.sensitive).toBeUndefined();
  });

// 凭据来源只在连接器就绪且该连接器声明支撑此渠道类型时成立。
test('凭据来源由连接器就绪状态与支撑渠道声明共同决定',
  // 来源测试验证连接器继承的准入条件。
  () => {
    expect(resolveCredentialSource('qq', {})).toBeNull();
    expect(resolveCredentialSource('qq', { 'qqbot.app_id': '102012345' })).toBeNull();
    expect(resolveCredentialSource('qq', { 'qqbot.app_id': '102012345', 'qqbot.app_secret_configured': true }))
      .toEqual({ hiddenKeys: ['app_id', 'app_secret'], connectorName: 'QQ 机器人' });
    // 没有声明 providerConnectorId 的类型不会因连接器就绪而继承凭据。
    expect(resolveCredentialSource('bark', { 'qqbot.app_id': '102012345', 'qqbot.app_secret_configured': true })).toBeNull();
    // 连接器未声明支撑该渠道类型时不得越权代填凭据。
    expect(resolveCredentialSource(
      'qq',
      { 'qqbot.app_id': '102012345', 'qqbot.app_secret_configured': true },
      [{ ...qqBotConnector, powersChannelTypes: [] }],
    )).toBeNull();
  });

// 凭据必填豁免只作用于本类型声明的凭据键，不得外溢到普通配置字段。
test('凭据必填豁免只对声明键生效且不外溢',
  // 校验测试验证连接器接管、已保存凭据与普通字段三种边界。
  () => {
    // qqForm 是只填写了单聊目标 openid 的 qq 渠道表单。
    const qqForm = createForm({ type: 'qq', config: { user_openid: 'user-openid' } });
    expect(validateNotificationForm(qqForm)).toBe('请填写 机器人 AppID');
    expect(validateNotificationForm(qqForm, { hiddenKeys: ['app_id', 'app_secret'] })).toBe('');
    expect(validateNotificationForm(qqForm, { savedCredentialKeys: ['app_id', 'app_secret'] })).toBe('');
    // 其他类型的凭据键出现在豁免列表里也不能跳过 qq 的凭据必填。
    expect(validateNotificationForm(qqForm, { hiddenKeys: ['webhook_url'], savedCredentialKeys: ['device_key'] })).toBe('请填写 机器人 AppID');
    // server_url 不是 bark 声明的凭据键，即使出现在已保存列表里也仍然必填。
    expect(validateNotificationForm(
      createForm({ config: { device_key: 'device-key' } }),
      { hiddenKeys: ['device_key'], savedCredentialKeys: ['server_url'] },
    )).toContain('Bark 服务器');
    expect(validateNotificationForm(createForm({ config: { server_url: 'https://api.day.app' } }), { savedCredentialKeys: ['device_key'] })).toBe('');
  });

// 凭据清除意图由 credentialAction 表达：clear 写空串触发删除，retain 与缺省都省略该键以保留原值。
test('凭据清除意图在载荷上表现为空串、缺省提交即保留',
  // 载荷测试验证三种凭据键在保留与清除下的提交差异。
  () => {
    // keepForm 表示继续沿用渠道已保存的独立凭据。
    const keepForm = createForm({ type: 'qq', config: { user_openid: 'user-openid', group_openid: '' }, credentialAction: 'retain' });
    expect(buildNotificationPayload(keepForm).config).toEqual({ user_openid: 'user-openid', group_openid: '' });
    expect(Object.keys(buildNotificationPayload(keepForm).config ?? {})).not.toContain('app_id');
    expect(Object.keys(buildNotificationPayload({ ...keepForm, credentialAction: undefined }).config ?? {})).not.toContain('app_secret');
    // clearForm 表示改用连接器凭据，该类型的凭据键一律写成空串交给服务端删除。
    const clearForm = createForm({ type: 'qq', config: { user_openid: 'user-openid', group_openid: '' }, credentialAction: 'clear' });
    expect(buildNotificationPayload(clearForm).config).toEqual({ user_openid: 'user-openid', group_openid: '', app_id: '', app_secret: '' });
    // 清除范围只限当前类型声明的凭据键，不会带上其他渠道的键。
    expect(buildNotificationPayload(createForm({ credentialAction: 'clear' })).config).toEqual({ server_url: 'https://api.day.app', device_key: '' });
    // 清除意图不写回表单状态，取消勾选后重新保存仍提交原值。
    expect(clearForm.config).toEqual({ user_openid: 'user-openid', group_openid: '' });
  });
