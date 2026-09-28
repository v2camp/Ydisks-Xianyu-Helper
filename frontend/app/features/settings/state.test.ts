import { expect,test } from 'vitest';
import type { SystemSettings } from './api';
import { buildPersistableSettings,createCredentials,createCredentialsMessage,isCurrentSettingsRequest,validateCredentials } from './state';

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
