// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import OpsAgent from './OpsAgent';
import {
  getOpsConversations,getOpsMessages,getSystemSettings,sendOpsMessage,updateSystemSettings,verifySession,
} from '../api';
import type { OpsConversation,SystemSettings } from '../models';

vi.mock('../api', /* opsAgentApiMockFactory 只替换请求入口，保留纯函数判定避免测试重复实现。 */ async (importOriginal) => {
  // actual 是真实适配层，保留 isQQBotConnectorReady 等纯函数。
  const actual = await importOriginal<typeof import('../api')>();
  return {
    ...actual,
    getOpsConversations: vi.fn(),
    getOpsMessages: vi.fn(),
    getSystemSettings: vi.fn(),
    sendOpsMessage: vi.fn(),
    updateSystemSettings: vi.fn(),
    verifySession: vi.fn(),
  };
});

// getSettingsMock 是系统设置读取的替身，负责让运营配置离开加载态。
const getSettingsMock = vi.mocked(getSystemSettings);
// verifySessionMock 是会话校验的替身，页面只消费其中的用户名。
const verifySessionMock = vi.mocked(verifySession);
// updateSettingsMock 是运营配置保存的替身，用于断言只提交两个 ops 键。
const updateSettingsMock = vi.mocked(updateSystemSettings);
// conversationsMock 是运营会话列表读取的替身。
const conversationsMock = vi.mocked(getOpsConversations);
// messagesMock 是运营消息读取的替身。
const messagesMock = vi.mocked(getOpsMessages);
// sendMock 是人工发送请求的替身。
const sendMock = vi.mocked(sendOpsMessage);

// READY_SETTINGS 是 QQ 凭据齐全且入站开关已关闭的运营配置快照。
const READY_SETTINGS: SystemSettings = {
  'qqbot.app_id': '102012345',
  'qqbot.app_secret_configured': true,
  'qqbot.commands_enabled': false,
  'qqbot.command_openids': '',
};

// MISSING_CREDENTIALS_SETTINGS 是尚未配置 QQ 凭据的运营配置快照。
const MISSING_CREDENTIALS_SETTINGS: SystemSettings = {
  'qqbot.commands_enabled': false,
  'qqbot.command_openids': '',
};

// conversationFixture 是运营对话面板渲染使用的单聊会话。
const conversationFixture: OpsConversation = {
  id: 1, channel: 'qq', targetOpenid: 'USER-OPENID-0001', isGroup: false,
  lastMessageAt: 1_700_000_000_000, lastMessagePreview: '在吗',
};

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

beforeEach(/* 当前回调为每个用例重置适配器替身的默认成功响应。 */ () => {
  vi.clearAllMocks();
  getSettingsMock.mockResolvedValue(READY_SETTINGS);
  verifySessionMock.mockResolvedValue({ authenticated: true, username: 'admin' });
  updateSettingsMock.mockResolvedValue({ success: true });
  conversationsMock.mockResolvedValue({ conversations: [conversationFixture], total: 1, page: 1, pageSize: 20, totalPages: 1 });
  messagesMock.mockResolvedValue({ messages: [], hasMore: false });
  sendMock.mockResolvedValue({
    message: { id: 2, conversationId: 1, direction: 'outbound', source: 'operator', content: '收到', createdAt: 1_700_000_100_000 },
    conversation: { ...conversationFixture, lastMessagePreview: '收到', lastMessageAt: 1_700_000_100_000 },
  });
});

describe('OpsAgent', /* 当前测试组验证运营 Agent 页头、入站配置保存、凭据依赖与对话区挂载。 */ () => {
  test('页头展示运营 Agent 与职责说明，并挂载对话面板', /* 当前测试验证页面结构与对话区落位。 */ async () => {
    render(<OpsAgent />);
    expect(await screen.findByRole('heading', { name: '运营 Agent' })).toBeTruthy();
    expect(screen.getByText('走 QQ 通道；开关与白名单在此，凭据在连接器')).toBeTruthy();
    expect(await screen.findByTestId('ops-conversation-panel')).toBeTruthy();
  });

  test('保存只提交入站开关与白名单两个键', /* 当前测试验证运营页保存范围隔离。 */ async () => {
    render(<OpsAgent />);
    // toggle 是入站命令开关复选框。
    const toggle = await screen.findByLabelText('启用运营 Agent（QQ 入站）') as HTMLInputElement;
    expect(toggle.checked).toBe(false);
    fireEvent.click(toggle);
    // allowlist 是命令发送者白名单输入框。
    const allowlist = screen.getByLabelText('命令发送者白名单（openid）') as HTMLTextAreaElement;
    fireEvent.change(allowlist, { target: { value: 'openid-a' } });
    fireEvent.click(screen.getByRole('button', { name: '保存运营配置' }));
    await waitFor(/* 当前回调等待保存请求发出。 */ () => expect(updateSettingsMock).toHaveBeenCalledTimes(1));
    // payload 是本次提交的运营配置载荷。
    const payload = updateSettingsMock.mock.calls[0][0] as Record<string, unknown>;
    expect(payload).toEqual({ 'qqbot.commands_enabled': true, 'qqbot.command_openids': 'openid-a' });
    expect(await screen.findByText('运营 Agent 配置已保存')).toBeTruthy();
  });

  test('QQ 凭据就绪时依赖行显示已配置且输入可用', /* 当前测试验证凭据状态来自系统设置快照。 */ async () => {
    render(<OpsAgent />);
    // row 是 QQ 机器人凭据依赖行。
    const row = await screen.findByTestId('dependency-qq-connector');
    expect(row.textContent).toContain('已配置');
    fireEvent.click(await screen.findByText('用户 · USER-OPE'));
    // input 是人工消息输入框，凭据就绪时应可输入。
    const input = await screen.findByLabelText('发送运营消息') as HTMLTextAreaElement;
    expect(input.disabled).toBe(false);
  });

  test('QQ 凭据未就绪时禁用发送并提示配置位置', /* 当前测试验证凭据缺失不会放开人工发送。 */ async () => {
    getSettingsMock.mockResolvedValue(MISSING_CREDENTIALS_SETTINGS);
    render(<OpsAgent />);
    // row 是 QQ 机器人凭据依赖行，缺失时应显示未配置。
    const row = await screen.findByTestId('dependency-qq-connector');
    expect(row.textContent).toContain('未配置');
    fireEvent.click(await screen.findByText('用户 · USER-OPE'));
    expect(await screen.findByText('先在连接器页配置 QQ 机器人')).toBeTruthy();
    // input 是人工消息输入框，凭据未就绪时必须禁用。
    const input = screen.getByLabelText('发送运营消息') as HTMLTextAreaElement;
    expect(input.disabled).toBe(true);
  });
});
