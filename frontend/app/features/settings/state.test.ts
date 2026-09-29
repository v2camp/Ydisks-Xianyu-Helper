import { expect,test } from 'vitest';
import type { SystemSettings } from './api';
import {
  buildPersistableSettings,
  createCredentials,
  createCredentialsMessage,
  DEFAULT_DELIVERY_GUARD_CONFIG,
  isCurrentSettingsRequest,
  parseDeliveryGuardConfig,
  serializeDeliveryGuardConfig,
  validateCredentials,
} from './state';

// settingsFixture 是覆盖敏感字段过滤、系统字段与 AI 字段的最小草稿。
const settingsFixture: SystemSettings = {
  log_level: 'info',
  smtp_password: 'secret',
  ai_api_key: 'api-key',
  ai_model: 'model-a',
  'mcp.servers': '[{"name":"find_stuff","url":"http://127.0.0.1:59190/mcp"}]',
  renewal_log_retention_days: 15,
};

test('system 保存草稿只提交系统白名单字段并剔除 AI 字段',
  // 配置裁剪测试验证 SMTP 兼容字段与 AI 字段都不会被系统范围批量保存覆盖。
  () => {
    expect(buildPersistableSettings(settingsFixture, 'system')).toEqual({ log_level: 'info', renewal_log_retention_days: 15 });
  });

test('ai 保存草稿只提交 AI 白名单字段并剔除系统字段',
  // 配置裁剪测试验证 AI 范围不会覆盖系统设置页负责的字段。
  () => {
    expect(buildPersistableSettings(settingsFixture, 'ai')).toEqual({ ai_api_key: 'api-key', ai_model: 'model-a', 'mcp.servers': settingsFixture['mcp.servers'] });
  });

test('登录凭据校验覆盖用户名、密码和确认密码边界',
  // 凭据校验测试验证所有前端可直接阻断的错误都在请求前返回。
  () => {
    expect(validateCredentials(createCredentials('ab'))).toContain('用户名');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: '' })).toContain('当前密码');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: 'old', new_password: 'short', confirm_password: 'short' })).toContain('8 个字符');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: 'old', new_password: 'long-password', confirm_password: 'different' })).toContain('不一致');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: 'old' })).toBe('');
    expect(createCredentialsMessage('success', '已保存')).toEqual({ type: 'success', text: '已保存' });
  });

test('Settings 请求代次拒绝过期响应和取消响应',
  // 请求边界测试验证刷新或组件卸载后旧响应不会覆盖当前状态。
  () => {
    // controller 是模拟组件卸载取消的控制器。
    const controller = new AbortController();
    expect(isCurrentSettingsRequest(2, 2, controller.signal)).toBe(true);
    expect(isCurrentSettingsRequest(1, 2, controller.signal)).toBe(false);
    controller.abort();
    expect(isCurrentSettingsRequest(2, 2, controller.signal)).toBe(false);
  });

test('system 保存草稿保留 delivery_content_guard 门禁 JSON',
  // 白名单测试验证门禁配置随系统配置一起提交，不会被裁剪掉。
  () => {
    // guardJSON 是开关关闭且带额外违禁词的门禁配置原文。
    const guardJSON = JSON.stringify({ enabled: false, link_check: true, extra_block_words: '加微信|扫码' });
    expect(buildPersistableSettings({ ...settingsFixture, delivery_content_guard: guardJSON }, 'system')).toEqual({
      log_level: 'info',
      renewal_log_retention_days: 15,
      delivery_content_guard: guardJSON,
    });
  });

test('门禁配置序列化与解析往返保持开关和额外词不变',
  // 往返测试验证序列化结果可被解析回等价配置，覆盖开关与额外词。
  () => {
    // config 是两项检查均关闭且含额外违禁词的配置。
    const config = { enabled: false, link_check: false, extra_block_words: '加微信, QQ群|站外' };
    // raw 是序列化后的门禁 JSON 原文。
    const raw = serializeDeliveryGuardConfig(config);
    expect(JSON.parse(raw)).toEqual(config);
    // roundTrip 是解析回的门禁配置。
    const roundTrip = parseDeliveryGuardConfig(raw);
    expect(roundTrip.invalid).toBe(false);
    expect(roundTrip.config).toEqual(config);
  });

test('门禁配置缺失或空白回退默认值且不提示非法',
  // 默认值测试验证未配置时展示默认启用状态，不触发非法 JSON 提示。
  () => {
    expect(parseDeliveryGuardConfig(undefined)).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: false });
    expect(parseDeliveryGuardConfig('')).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: false });
    expect(parseDeliveryGuardConfig('   ')).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: false });
    // defaults 是解析默认配置得到的结果，双向默认一致。
    const defaults = parseDeliveryGuardConfig(serializeDeliveryGuardConfig(DEFAULT_DELIVERY_GUARD_CONFIG));
    expect(defaults.invalid).toBe(false);
    expect(defaults.config).toEqual({ enabled: true, link_check: true, extra_block_words: '' });
  });

test('门禁配置非法 JSON 回退默认值并提示',
  // 回退测试验证非对象 JSON、数组、非字符串值与语法错误都回退默认且要求提示。
  () => {
    expect(parseDeliveryGuardConfig('{not json')).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: true });
    expect(parseDeliveryGuardConfig('[]')).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: true });
    expect(parseDeliveryGuardConfig('123')).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: true });
    expect(parseDeliveryGuardConfig(42)).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: true });
    expect(parseDeliveryGuardConfig('null')).toEqual({ config: DEFAULT_DELIVERY_GUARD_CONFIG, invalid: true });
  });

test('门禁配置部分字段缺失或类型不符时用默认补齐',
  // 归一测试验证缺失键与错误类型字段不会破坏其余字段读取。
  () => {
    // partial 是只关闭违禁词门禁、其余缺失的配置。
    const partial = parseDeliveryGuardConfig('{"enabled":false}');
    expect(partial.invalid).toBe(false);
    expect(partial.config).toEqual({ enabled: false, link_check: true, extra_block_words: '' });
    // typed 是开关为字符串布尔、额外词类型的容错解析结果。
    const typed = parseDeliveryGuardConfig('{"enabled":"false","link_check":"true","extra_block_words":123}');
    expect(typed.invalid).toBe(false);
    expect(typed.config).toEqual({ enabled: false, link_check: true, extra_block_words: '' });
  });

test('门禁配置额外违禁词保存逗号竖线原文',
  // 额外词测试验证用户输入的分隔符原文原样序列化保存。
  () => {
    // config 是保存额外违禁词原文的配置。
    const config = { enabled: true, link_check: false, extra_block_words: '加微信,\n扫码进群|QQ' };
    // raw 是序列化后的门禁 JSON 原文。
    const raw = serializeDeliveryGuardConfig(config);
    expect(JSON.parse(raw).extra_block_words).toBe('加微信,\n扫码进群|QQ');
    expect(parseDeliveryGuardConfig(raw).config.extra_block_words).toBe('加微信,\n扫码进群|QQ');
  });
