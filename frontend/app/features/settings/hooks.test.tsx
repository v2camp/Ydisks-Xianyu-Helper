// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { OperationResponse,SystemSettings } from './api';
import { getSystemSettings,updateLoginCredentials,updateSystemSettings,verifySession } from './api';
import { useSettings } from './hooks';

vi.mock('./api', /* settingsApiMockFactory 提供设置 Hook 的确定性 API 替身。 */ () => ({
  getSystemSettings: vi.fn(),
  updateLoginCredentials: vi.fn(),
  updateSystemSettings: vi.fn(),
  verifySession: vi.fn(),
}));

// getSettingsMock 是系统设置读取请求的可控替身。
const getSettingsMock = vi.mocked(getSystemSettings);
// updateCredentialsMock 是登录凭据更新请求的可控替身。
const updateCredentialsMock = vi.mocked(updateLoginCredentials);
// updateSettingsMock 是系统设置保存请求的可控替身。
const updateSettingsMock = vi.mocked(updateSystemSettings);
// verifySessionMock 是会话校验请求的可控替身。
const verifySessionMock = vi.mocked(verifySession);

// settingsFixture 是覆盖 AI 行为、验证码和日志字段的实例级系统配置。
const settingsFixture: SystemSettings = { ai_master_enabled: true, ai_scope_config: '{"enabled":true}', 'mcp.servers': '[{"name":"find_stuff","url":"http://127.0.0.1:59190/mcp"}]', 'captcha.remote_service_url': 'https://captcha.example.com', log_level: 'info', global_send_daily_limit: 5 };
// noopReload 是设置凭据成功后定时重载页面的浏览器行为替身。
const noopReload = vi.fn();
// renderSystemSettingsHook 是 system 保存范围的设置 Hook 渲染回调，只保存系统白名单字段。
const renderSystemSettingsHook = () => useSettings('system');
// renderAISettingsHook 是 ai 保存范围的设置 Hook 渲染回调，只保存 AI 行为字段。
const renderAISettingsHook = () => useSettings('ai');

describe('useSettings', /* 当前回调处理设置草稿和凭据请求状态。 */ () => {
  beforeEach(/* 当前回调重置设置 API 替身和浏览器副作用。 */ () => {
    vi.clearAllMocks();
    getSettingsMock.mockResolvedValue(settingsFixture);
    verifySessionMock.mockResolvedValue({ authenticated: true, username: 'admin' });
    updateSettingsMock.mockResolvedValue({ success: true });
    updateCredentialsMock.mockResolvedValue({ success: true, message: '凭据已更新' });
    vi.spyOn(window, 'alert').mockImplementation(
      // alertImplementation 屏蔽设置保存成功时的浏览器弹窗。
      () => undefined,
    );
    Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, reload: noopReload } });
  });

  test('ai 范围加载设置后可以保存 AI 行为字段', /* 当前回调验证 AI 范围加载并保存白名单字段的路径。 */ async () => {
    // hook 是 AI 范围设置 Hook 的渲染结果。
    const hook = renderHook(renderAISettingsHook);
    await waitFor(
      // statusAssertion 等待系统设置加载成功。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    expect(hook.result.current.settings).toMatchObject(settingsFixture);
    expect(hook.result.current.credentials.new_username).toBe('admin');

    await act(
      // saveAction 执行 AI 配置保存动作。
      async () => hook.result.current.handleSave(),
    );
    expect(updateSettingsMock).toHaveBeenCalledWith(expect.objectContaining({ ai_master_enabled: true }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(window.alert).toHaveBeenCalledWith('AI 配置已保存');
  });

  test('system 范围保存不包含 ai_* 与 mcp.servers 字段', /* 当前回调验证系统范围保存不会覆盖 AI 设置。 */ async () => {
    // hook 是系统范围设置 Hook 的渲染结果。
    const hook = renderHook(renderSystemSettingsHook);
    await waitFor(
      // statusAssertion 等待系统设置加载成功。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    await act(
      // saveAction 执行系统配置保存动作。
      async () => hook.result.current.handleSave(),
    );
    // payload 是系统范围实际提交的设置载荷。
    const payload = updateSettingsMock.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.log_level).toBe('info');
    expect(payload.global_send_daily_limit).toBe(5);
    expect(payload['captcha.remote_service_url']).toBe('https://captcha.example.com');
    expect(payload.ai_master_enabled).toBeUndefined();
    expect(payload.ai_scope_config).toBeUndefined();
    expect(payload['mcp.servers']).toBeUndefined();
    expect(window.alert).toHaveBeenCalledWith('系统配置已保存');
  });

  test('ai 范围保存不包含系统字段', /* 当前回调验证 AI 范围保存不会覆盖系统设置。 */ async () => {
    // hook 是 AI 范围设置 Hook 的渲染结果。
    const hook = renderHook(renderAISettingsHook);
    await waitFor(
      // statusAssertion 等待设置加载成功。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    await act(
      // saveAction 执行 AI 配置保存动作。
      async () => hook.result.current.handleSave(),
    );
    // payload 是 AI 范围实际提交的设置载荷。
    const payload = updateSettingsMock.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.ai_master_enabled).toBe(true);
    expect(payload.ai_scope_config).toBe('{"enabled":true}');
    expect(payload['mcp.servers']).toBe(settingsFixture['mcp.servers']);
    expect(payload.log_level).toBeUndefined();
    expect(payload.global_send_daily_limit).toBeUndefined();
    expect(payload['captcha.remote_service_url']).toBeUndefined();
  });

  test('凭据校验失败和服务拒绝都写入错误提示', /* 当前回调验证登录凭据校验和后端拒绝路径。 */ async () => {
    // hook 是凭据表单测试使用的系统范围设置 Hook 渲染结果。
    const hook = renderHook(renderSystemSettingsHook);
    await waitFor(
      // statusAssertion 等待凭据测试使用的系统设置请求完成。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    // invalidEvent 是触发前端凭据校验的最小表单事件。
    const invalidEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // invalidAction 提交非法凭据表单并触发前端校验。
      async () => hook.result.current.handleCredentialsSave(invalidEvent),
    );
    expect(hook.result.current.credentialsMessage?.type).toBe('error');
    expect(updateCredentialsMock).not.toHaveBeenCalled();

    await act(
      // invalidCredentials 设置密码确认不一致的草稿。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'wrong-password' }),
    );
    // validEvent 是触发后端凭据拒绝的表单事件。
    const validEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // mismatchAction 提交确认密码不一致的表单。
      async () => hook.result.current.handleCredentialsSave(validEvent),
    );
    expect(updateCredentialsMock).not.toHaveBeenCalled();

    await act(
      // validCredentials 设置可以提交的凭据草稿。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'new-password' }),
    );
    // rejectedResponse 是后端拒绝凭据更新的统一响应。
    const rejectedResponse: OperationResponse = { success: false, message: '当前密码错误' };
    updateCredentialsMock.mockResolvedValueOnce(rejectedResponse);
    // rejectedEvent 是触发后端拒绝分支的表单事件。
    const rejectedEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // rejectedAction 提交合法表单并验证后端拒绝提示。
      async () => hook.result.current.handleCredentialsSave(rejectedEvent),
    );
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'error', text: '当前密码错误' });
  });

  test('设置读取失败时展示加载错误', /* 当前回调验证系统设置初始请求失败路径。 */ async () => {
    getSettingsMock.mockRejectedValueOnce(new Error('设置服务失败'));
    // hook 是设置读取失败场景下的系统范围设置 Hook 渲染结果。
    const hook = renderHook(renderSystemSettingsHook);
    await waitFor(
      // errorAssertion 等待设置读取错误状态写入。
      () => expect(hook.result.current.requestStatus).toBe('error'),
    );
    expect(hook.result.current.loadError).toBe('设置服务失败');
    expect(hook.result.current.settings).toBeNull();
    await act(
      // saveGuardAction 在没有配置数据时阻止保存请求。
      async () => hook.result.current.handleSave(),
    );
    expect(updateSettingsMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('配置保存和凭据网络异常时保留错误状态', /* 当前回调验证设置保存与凭据网络错误分支。 */ async () => {
    // hook 是设置保存异常场景的系统范围设置 Hook 渲染结果。
    const hook = renderHook(renderSystemSettingsHook);
    await waitFor(
      // statusAssertion 等待设置读取完成。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    updateSettingsMock.mockRejectedValueOnce(new Error('配置保存失败'));
    await act(
      // saveErrorAction 触发系统配置保存错误。
      async () => hook.result.current.handleSave(),
    );
    expect(hook.result.current.saveError).toBe('配置保存失败');

    await act(
      // credentialsAction 写入合法登录凭据。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'new-password' }),
    );
    updateCredentialsMock.mockRejectedValueOnce(new Error('凭据网络失败'));
    // credentialsEvent 是触发凭据保存的表单事件。
    const credentialsEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // credentialsErrorAction 触发凭据网络异常。
      async () => hook.result.current.handleCredentialsSave(credentialsEvent),
    );
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'error', text: '凭据网络失败' });

    updateCredentialsMock.mockResolvedValueOnce({ success: true, message: '更新成功' });
    await act(
      // credentialsSuccessAction 验证凭据成功提示和重载调度。
      async () => hook.result.current.handleCredentialsSave(credentialsEvent),
    );
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'success', text: '更新成功' });
    await waitFor(
      // reloadAssertion 等待凭据保存成功后的页面重载调度。
      () => expect(noopReload).toHaveBeenCalled(),
      { timeout: 2_000 },
    );
    hook.unmount();
  });

  test('重复加载设置时丢弃先发出的旧响应', /* 当前回调验证系统设置请求代次隔离。 */ async () => {
    // resolveFirst 是旧设置请求的完成控制器。
    let resolveFirst: (value: SystemSettings) => void = () => undefined;
    // firstRequest 是保持未完成的旧设置请求 Promise。
    const firstRequest = new Promise<SystemSettings>(/* firstExecutor 保存旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    getSettingsMock.mockReset();
    getSettingsMock.mockReturnValueOnce(firstRequest);
    getSettingsMock.mockResolvedValue(settingsFixture);
    // hook 是系统设置刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(renderAISettingsHook);
    await act(
      // refreshAction 发起第二次设置加载并使首次请求过期。
      () => hook.result.current.loadSettings(),
    );
    await waitFor(
      // successAssertion 等待第二次设置加载成功。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    resolveFirst(settingsFixture);
    await act(
      // staleResolveAction 完成已过期的首次设置响应。
      async () => { await firstRequest; },
    );
    expect(hook.result.current.settings).toMatchObject(settingsFixture);
    hook.unmount();
  });

  test('保存系统配置不会中断正在进行的凭据保存', /* 当前回调验证两类表单请求使用独立取消代次。 */ async () => {
    // resolveCredentials 是延迟凭据请求的完成控制器。
    let resolveCredentials: (value: OperationResponse) => void = () => undefined;
    // pendingCredentialsRequest 模拟尚未返回的凭据保存请求。
    const pendingCredentialsRequest = new Promise<OperationResponse>(/* credentialsExecutor 保存凭据请求完成函数。 */ resolve => { resolveCredentials = resolve; });
    updateCredentialsMock.mockReturnValueOnce(pendingCredentialsRequest);
    // hook 是两个保存动作并发场景下的 Hook 渲染结果。
    const hook = renderHook(renderSystemSettingsHook);
    await waitFor(
      // statusAssertion 等待初始设置加载完成。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    await act(
      // credentialsAction 写入可提交的登录凭据。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'new-password' }),
    );
    // credentialsEvent 是提交凭据表单所需的最小浏览器事件。
    const credentialsEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    // credentialsAction 发起保持未完成的凭据保存。
    let credentialsAction: Promise<void> = Promise.resolve();
    act(/* startCredentialsAction 启动凭据保存但不等待远端响应。 */ () => { credentialsAction = hook.result.current.handleCredentialsSave(credentialsEvent); });
    await waitFor(
      // savingAssertion 等待凭据表单进入保存状态。
      () => expect(hook.result.current.credentialsSaving).toBe(true),
    );
    await act(
      // settingsAction 在凭据保存期间提交系统配置。
      async () => hook.result.current.handleSave(),
    );
    expect(hook.result.current.credentialsSaving).toBe(true);
    resolveCredentials({ success: true, message: '凭据已更新' });
    await act(
      // completeCredentialsAction 完成仍然有效的凭据请求。
      async () => credentialsAction,
    );
    expect(hook.result.current.credentialsSaving).toBe(false);
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'success', text: '凭据已更新' });
    hook.unmount();
  });
});
