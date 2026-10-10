// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { MCPAuditPage,MCPServiceStatus,OperationResponse } from './api';
import { generateMCPToken,getMCPAudit,getMCPServiceStatus,revokeMCPToken,updateMCPServiceSettings } from './api';
import { useMCPService } from './mcpHooks';

vi.mock('./api', /* settingsApiMockFactory 提供设置 Hook 与 MCP 卡片共用的确定性 API 替身。 */ () => ({
  fetchAIModels: vi.fn(),
  generateMCPToken: vi.fn(),
  getMCPAudit: vi.fn(),
  getMCPServiceStatus: vi.fn(),
  getSystemSettings: vi.fn(),
  revokeMCPToken: vi.fn(),
  testAIConnection: vi.fn(),
  updateLoginCredentials: vi.fn(),
  updateMCPServiceSettings: vi.fn(),
  updateSystemSettings: vi.fn(),
  verifySession: vi.fn(),
}));

// statusMock 是 MCP 状态读取请求的可控替身。
const statusMock = vi.mocked(getMCPServiceStatus);
// auditMock 是 MCP 审计分页读取请求的可控替身。
const auditMock = vi.mocked(getMCPAudit);
// settingsMock 是 MCP 启停与网络策略保存请求的可控替身。
const settingsMock = vi.mocked(updateMCPServiceSettings);
// tokenMock 是 MCP 令牌生成请求的可控替身。
const tokenMock = vi.mocked(generateMCPToken);
// revokeMock 是 MCP 令牌吊销请求的可控替身。
const revokeMock = vi.mocked(revokeMCPToken);

// statusFixture 是已启用、尚未生成令牌的初始状态。
const statusFixture: MCPServiceStatus = {
  enabled: true, allowNonLoopback: false, hasToken: false,
  tokenCreatedAt: 0, tokenLastUsedAt: 0, hasPreviousToken: false,
  previousTokenExpiresAt: 0, endpoint: 'http://127.0.0.1:59188/mcp',
};
// disabledStatusFixture 是未启用、未放行的初始状态，用于复现启用开关的乐观置值。
const disabledStatusFixture: MCPServiceStatus = { ...statusFixture, enabled: false, allowNonLoopback: false };
// auditFixture 是包含一条成功调用的审计分页。
const auditFixture: MCPAuditPage = {
  records: [{
    id: 1, createdAt: 1_700_000_000, userId: 1, tokenSource: 'persisted', category: 'tool',
    name: 'system_ping', cookieId: '', arguments: '{"confirm":true}', success: true, errorClass: '', durationMs: 3,
  }],
  total: 1, page: 1, pageSize: 10, totalPages: 1,
};

describe('useMCPService', /* 当前回调验证 MCP 卡片的状态、启停、令牌与审计流程。 */ () => {
  beforeEach(/* 当前回调重置 MCP API 替身的默认成功结果。 */ () => {
    vi.clearAllMocks();
    statusMock.mockResolvedValue(statusFixture);
    auditMock.mockResolvedValue(auditFixture);
    settingsMock.mockResolvedValue({ success: true });
    tokenMock.mockResolvedValue('plain-token-abc');
    revokeMock.mockResolvedValue({ success: true });
  });

  test('挂载后加载状态与首页审计', /* 当前回调验证首次进入页面的状态与审计加载。 */ async () => {
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    expect(hook.result.current.status?.endpoint).toBe(statusFixture.endpoint);
    await waitFor(/* 等待审计加载完成。 */ () => expect(hook.result.current.audit).not.toBeNull());
    expect(hook.result.current.audit?.records[0].name).toBe('system_ping');
    expect(hook.result.current.statusLoading).toBe(false);
    expect(hook.result.current.auditLoading).toBe(false);
  });

  test('保存启用开关后刷新状态并提示成功', /* 当前回调验证启停保存成功后的提示与状态刷新。 */ async () => {
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 保存动作切换启用开关。 */ async () => hook.result.current.submitSettings({ enabled: false, allowNonLoopback: false }));
    expect(settingsMock).toHaveBeenCalledWith({ enabled: false, allowNonLoopback: false });
    expect(hook.result.current.message).toEqual({ type: 'success', text: 'MCP 服务设置已保存' });
    expect(statusMock.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  test('保存请求未返回前启用开关已呈现目标值', /* 当前回调验证启停开关在保存请求落地之前就反映管理员点击的目标状态。 */ async () => {
    // resolveSave 是保存请求的延迟放行控制器。
    let resolveSave: (value: OperationResponse) => void = () => undefined;
    statusMock.mockResolvedValue(disabledStatusFixture);
    // pendingSave 是保持挂起直到显式放行的保存请求，用于观察保存未落地时的开关展示值。
    const pendingSave = new Promise<OperationResponse>(/* resolveSave 保存延迟保存请求的完成函数。 */ resolve => { resolveSave = resolve; });
    settingsMock.mockReturnValue(pendingSave);
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    act(/* 点击启用开关并触发保存。 */ () => { void hook.result.current.submitSettings({ enabled: true, allowNonLoopback: false }); });
    expect(hook.result.current.status?.enabled).toBe(true);
    expect(hook.result.current.saving).toBe(true);
    await act(/* 放行保存请求并等待收尾。 */ async () => { resolveSave({ success: true }); });
  });

  test('保存成功后由回读结果接管且后续回读可覆盖', /* 当前回调验证乐观值在回读落地后被清除，不再遮蔽服务端值。 */ async () => {
    statusMock.mockResolvedValueOnce(disabledStatusFixture);
    statusMock.mockResolvedValueOnce({ ...disabledStatusFixture, enabled: true });
    statusMock.mockResolvedValue(disabledStatusFixture);
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 保存动作切换启用开关。 */ async () => hook.result.current.submitSettings({ enabled: true, allowNonLoopback: false }));
    expect(hook.result.current.status?.enabled).toBe(true);
    await act(/* 再次回读服务端状态。 */ async () => hook.result.current.reloadStatus());
    expect(hook.result.current.status?.enabled).toBe(false);
  });

  test('保存失败时回滚到服务端值', /* 当前回调验证保存失败后开关回到服务端状态而不是停在乐观值。 */ async () => {
    statusMock.mockResolvedValue(disabledStatusFixture);
    settingsMock.mockRejectedValue(new Error('网络异常'));
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 保存动作触发失败分支。 */ async () => hook.result.current.submitSettings({ enabled: true, allowNonLoopback: true }));
    expect(hook.result.current.status?.enabled).toBe(false);
    expect(hook.result.current.status?.allowNonLoopback).toBe(false);
    expect(hook.result.current.message?.type).toBe('error');
    expect(hook.result.current.saving).toBe(false);
  });

  test('非本机访问开关在保存请求未返回前已呈现目标值', /* 当前回调验证网络策略开关与启停开关同样乐观置值。 */ async () => {
    // resolveAllow 是保存请求的延迟放行控制器。
    let resolveAllow: (value: OperationResponse) => void = () => undefined;
    statusMock.mockResolvedValue(disabledStatusFixture);
    // pendingAllow 是保持挂起直到显式放行的保存请求，用于观察网络策略开关的乐观置值。
    const pendingAllow = new Promise<OperationResponse>(/* resolveAllow 保存延迟保存请求的完成函数。 */ resolve => { resolveAllow = resolve; });
    settingsMock.mockReturnValue(pendingAllow);
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    act(/* 点击非本机访问开关并触发保存。 */ () => { void hook.result.current.submitSettings({ enabled: false, allowNonLoopback: true }); });
    expect(hook.result.current.status?.allowNonLoopback).toBe(true);
    await act(/* 放行保存请求并等待收尾。 */ async () => { resolveAllow({ success: true }); });
  });

  test('连续切换以最后一次目标值为准', /* 当前回调验证在途乐观值不会累积，最终展示最近一次点击的目标。 */ async () => {
    // resolveFirst 是第一次保存请求的延迟放行控制器。
    let resolveFirst: (value: OperationResponse) => void = () => undefined;
    // resolveSecond 是第二次保存请求的延迟放行控制器。
    let resolveSecond: (value: OperationResponse) => void = () => undefined;
    statusMock.mockResolvedValue(disabledStatusFixture);
    // pendingFirst 是第一次保持挂起的保存请求，用于观察在途乐观值。
    const pendingFirst = new Promise<OperationResponse>(/* resolveFirst 保存第一次延迟保存请求的完成函数。 */ resolve => { resolveFirst = resolve; });
    // pendingSecond 是第二次保持挂起的保存请求，用于验证在途乐观值以最后一次点击为准。
    const pendingSecond = new Promise<OperationResponse>(/* resolveSecond 保存第二次延迟保存请求的完成函数。 */ resolve => { resolveSecond = resolve; });
    settingsMock.mockReturnValueOnce(pendingFirst).mockReturnValueOnce(pendingSecond);
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    act(/* 第一次点击请求启用。 */ () => { void hook.result.current.submitSettings({ enabled: true, allowNonLoopback: false }); });
    act(/* 第二次点击改回关闭并放开非本机访问。 */ () => { void hook.result.current.submitSettings({ enabled: false, allowNonLoopback: true }); });
    expect(hook.result.current.status?.enabled).toBe(false);
    expect(hook.result.current.status?.allowNonLoopback).toBe(true);
    await act(/* 放行两次保存请求并等待收尾。 */ async () => { resolveFirst({ success: true }); resolveSecond({ success: true }); });
  });

  test('保存失败时展示错误提示', /* 当前回调验证启停保存失败的错误提示。 */ async () => {
    settingsMock.mockRejectedValue(new Error('网络异常'));
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 保存动作触发失败分支。 */ async () => hook.result.current.submitSettings({ enabled: true, allowNonLoopback: true }));
    expect(hook.result.current.message?.type).toBe('error');
    expect(hook.result.current.saving).toBe(false);
  });

  test('生成令牌后保留一次性明文并刷新状态', /* 当前回调验证令牌生成后的明文保留与状态刷新。 */ async () => {
    statusMock.mockResolvedValueOnce(statusFixture);
    statusMock.mockResolvedValue({ ...statusFixture, hasToken: true, tokenCreatedAt: 1_700_000_100 });
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 生成动作请求一次性令牌明文。 */ async () => hook.result.current.generateToken());
    expect(hook.result.current.token).toBe('plain-token-abc');
    expect(hook.result.current.message?.type).toBe('success');
    await waitFor(/* 等待状态刷新反映令牌已存在。 */ () => expect(hook.result.current.status?.hasToken).toBe(true));
  });

  test('吊销令牌后清除页面明文', /* 当前回调验证吊销令牌后的一次性明文清理。 */ async () => {
    statusMock.mockResolvedValueOnce({ ...statusFixture, hasToken: true, tokenCreatedAt: 1_700_000_100 });
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 生成动作先制造页面明文。 */ async () => hook.result.current.generateToken());
    expect(hook.result.current.token).toBe('plain-token-abc');
    await act(/* 吊销动作清除全部持久化令牌。 */ async () => hook.result.current.revokeToken());
    expect(revokeMock).toHaveBeenCalledTimes(1);
    expect(hook.result.current.token).toBe('');
    expect(hook.result.current.message).toEqual({ type: 'success', text: '已吊销全部持久化令牌' });
  });

  test('审计加载失败时展示错误信息', /* 当前回调验证审计读取失败的降级提示。 */ async () => {
    auditMock.mockRejectedValue(new Error('审计服务不可用'));
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待审计失败信息落地。 */ () => expect(hook.result.current.auditError).not.toBe(''));
    expect(hook.result.current.auditError).toBe('审计服务不可用');
    expect(hook.result.current.auditLoading).toBe(false);
  });

  test('dismissToken 清除一次性明文', /* 当前回调验证管理员确认保存后清除页面明文。 */ async () => {
    // hook 是 MCP 服务钩子的渲染结果。
    const hook = renderHook(useMCPService);
    await waitFor(/* 等待初始状态加载完成。 */ () => expect(hook.result.current.status).not.toBeNull());
    await act(/* 生成动作制造页面明文。 */ async () => hook.result.current.generateToken());
    expect(hook.result.current.token).toBe('plain-token-abc');
    act(/* 确认动作清除页面明文。 */ () => hook.result.current.dismissToken());
    expect(hook.result.current.token).toBe('');
  });
});
