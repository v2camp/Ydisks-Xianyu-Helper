// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import Settings from './Settings';
import { getSystemSettings,updateSystemSettings,verifySession } from '../api';
import type { SystemSettings } from '../api';

vi.mock('../api', /* mockSettingsApi 用可断言的替身替换系统设置页依赖的适配层，避免测试触网。 */ () => ({
  fetchAIModels: vi.fn(),
  getSystemSettings: vi.fn(),
  testAIConnection: vi.fn(),
  updateLoginCredentials: vi.fn(),
  updateSystemSettings: vi.fn(),
  verifySession: vi.fn(),
}));

// getSettingsMock 是系统设置读取的替身，负责让页面离开加载态。
const getSettingsMock = vi.mocked(getSystemSettings);
// updateSettingsMock 是系统设置保存的替身，用于断言保存条确实触发了提交。
const updateSettingsMock = vi.mocked(updateSystemSettings);
// verifySessionMock 是会话校验的替身，系统设置页只消费其中的用户名。
const verifySessionMock = vi.mocked(verifySession);

// SYSTEM_SETTINGS_FIXTURE 是系统设置页渲染所需的最小配置快照。
const SYSTEM_SETTINGS_FIXTURE: SystemSettings = {
  log_level: 'info',
  log_format: 'text',
  renewal_log_retention_days: 10,
  backup_interval_hours: '24',
  global_send_daily_limit: 5,
  silence_alert_minutes: 180,
  'captcha.remote_service_url': 'https://captcha.example.com',
};

afterEach(/* 当前回调恢复被替换的弹窗并隔离 DOM。 */ () => {
  vi.restoreAllMocks();
  cleanup();
});

beforeEach(/* 当前回调为每个用例重置适配器替身与弹窗监视。 */ () => {
  vi.clearAllMocks();
  getSettingsMock.mockResolvedValue(SYSTEM_SETTINGS_FIXTURE);
  verifySessionMock.mockResolvedValue({ authenticated: true, username: 'admin' });
  updateSettingsMock.mockResolvedValue({ success: true });
  vi.spyOn(window, 'alert').mockImplementation(/* 当前回调屏蔽并记录可能的浏览器弹窗。 */ () => undefined);
});

describe('Settings 保存范式', /* 当前测试组验证系统配置保存改由页级保存条反馈而不是模态弹窗。 */ () => {
  test('保存成功后由保存条提示且不再弹窗', /* 当前测试验证页级保存条接管成功反馈。 */ async () => {
    render(<Settings />);
    // saveButton 是页级保存条的主按钮，文案说明本次保存的对象。
    const saveButton = await screen.findByRole('button', { name: '保存系统配置' });
    fireEvent.click(saveButton);
    expect(await screen.findByText('系统配置已保存')).toBeTruthy();
    // 保存载荷仍只包含系统白名单字段，保存范式变化不影响范围隔离。
    await waitFor(/* 当前回调等待保存请求发出。 */ () => expect(updateSettingsMock).toHaveBeenCalledTimes(1));
    // payload 是本次提交的设置载荷。
    const payload = updateSettingsMock.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.log_level).toBe('info');
    expect(payload.ai_api_url).toBeUndefined();
    expect(window.alert).not.toHaveBeenCalled();
  });

  test('保存失败时保存条常驻错误与重试入口', /* 当前测试验证失败反馈不再依赖页面顶部的内联横幅。 */ async () => {
    updateSettingsMock.mockRejectedValue(new Error('配置保存失败'));
    render(<Settings />);
    fireEvent.click(await screen.findByRole('button', { name: '保存系统配置' }));
    expect(await screen.findByText('配置保存失败')).toBeTruthy();
    // 失败态按钮复用同一提交回调，点击后重新发起保存。
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await waitFor(/* 当前回调等待重试再次提交。 */ () => expect(updateSettingsMock).toHaveBeenCalledTimes(2));
    expect(window.alert).not.toHaveBeenCalled();
  });
});
