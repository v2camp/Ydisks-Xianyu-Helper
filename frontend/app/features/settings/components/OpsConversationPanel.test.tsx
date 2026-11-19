// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { OpsConversationPanel } from './OpsConversationPanel';
import { getOpsConversations,getOpsMessages,sendOpsMessage } from '../api';
import type { OpsConversation,OpsMessage } from '../models';

vi.mock('../api', /* opsPanelApiMockFactory 提供运营对话面板的确定性 API 替身。 */ () => ({
  getOpsConversations: vi.fn(),
  getOpsMessages: vi.fn(),
  sendOpsMessage: vi.fn(),
}));

// conversationsMock 是运营会话列表读取的可控替身。
const conversationsMock = vi.mocked(getOpsConversations);
// messagesMock 是运营消息读取的可控替身。
const messagesMock = vi.mocked(getOpsMessages);
// sendMock 是人工发送请求的可控替身。
const sendMock = vi.mocked(sendOpsMessage);

// groupConversation 是群聊会话摘要，用于验证「群 · openid 前 8 位」。
const groupConversation: OpsConversation = {
  id: 1, channel: 'qq', targetOpenid: 'GROUP-OPENID-0001', isGroup: true,
  lastMessageAt: 1_700_000_000_000, lastMessagePreview: '早上好',
};
// userConversation 是单聊会话摘要，用于验证「用户 · openid 前 8 位」。
const userConversation: OpsConversation = {
  id: 2, channel: 'qq', targetOpenid: 'USER-OPENID-0002', isGroup: false,
  lastMessageAt: 1_700_000_100_000, lastMessagePreview: '在吗',
};
// inboundMessage 是群聊里的入站消息。
const inboundMessage: OpsMessage = { id: 11, conversationId: 1, direction: 'inbound', source: 'inbound', content: '早上好', createdAt: 1_700_000_000_000 };
// operatorMessage 是人工通过 Web 发出的出站消息。
const operatorMessage: OpsMessage = { id: 12, conversationId: 1, direction: 'outbound', source: 'operator', content: '人工回复', createdAt: 1_700_000_010_000 };
// commandMessage 是命令自动回复的出站消息。
const commandMessage: OpsMessage = { id: 13, conversationId: 1, direction: 'outbound', source: 'command', content: '销量 10', createdAt: 1_700_000_020_000 };

beforeEach(/* 当前回调重置运营面板 API 替身的默认成功响应。 */ () => {
  vi.clearAllMocks();
  conversationsMock.mockResolvedValue({
    conversations: [groupConversation, userConversation],
    total: 2, page: 1, pageSize: 20, totalPages: 1,
  });
  messagesMock.mockResolvedValue({ messages: [inboundMessage, operatorMessage, commandMessage], hasMore: false });
  sendMock.mockResolvedValue({
    message: { id: 14, conversationId: 1, direction: 'outbound', source: 'operator', content: '收到', createdAt: 1_700_000_030_000 },
    conversation: { ...groupConversation, lastMessagePreview: '收到', lastMessageAt: 1_700_000_030_000 },
  });
});

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

describe('OpsConversationPanel', /* 当前测试组验证会话列表、消息方向、来源标注与人工输入限制。 */ () => {
  test('会话项按群聊/单聊展示 openid 前缀，消息标注来源', /* 当前测试验证列表标签与出站来源文案。 */ async () => {
    render(<OpsConversationPanel credentialsReady />);
    expect(await screen.findByText('群 · GROUP-OP')).toBeTruthy();
    expect(screen.getByText('用户 · USER-OPE')).toBeTruthy();
    fireEvent.click(screen.getByText('群 · GROUP-OP'));
    expect(await screen.findByText('早上好')).toBeTruthy();
    expect(screen.getByText('人工回复')).toBeTruthy();
    expect(screen.getByText('销量 10')).toBeTruthy();
    expect(screen.getByText('人工')).toBeTruthy();
    expect(screen.getByText('命令回复')).toBeTruthy();
  });

  test('凭据未就绪时禁用输入并提示先配置 QQ 机器人', /* 当前测试验证凭据依赖被显式阻断。 */ async () => {
    render(<OpsConversationPanel credentialsReady={false} />);
    fireEvent.click(await screen.findByText('群 · GROUP-OP'));
    expect(await screen.findByText('先在连接器页配置 QQ 机器人')).toBeTruthy();
    // input 是人工消息输入框，凭据未就绪时必须禁用。
    const input = screen.getByLabelText('发送运营消息') as HTMLTextAreaElement;
    expect(input.disabled).toBe(true);
    expect((screen.getByRole('button', { name: /发送/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('Enter 发送成功并清空输入', /* 当前测试验证快捷键发送与成功清空。 */ async () => {
    render(<OpsConversationPanel credentialsReady />);
    fireEvent.click(await screen.findByText('群 · GROUP-OP'));
    // input 是人工消息输入框。
    const input = await screen.findByLabelText('发送运营消息') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: '收到' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(/* 当前回调等待发送请求发出。 */ () => expect(sendMock).toHaveBeenCalledWith(1, '收到', expect.objectContaining({ signal: expect.any(AbortSignal) })));
    await waitFor(/* 当前回调等待发送成功后清空输入。 */ () => expect(input.value).toBe(''));
  });

  test('Shift+Enter 只换行不发送', /* 当前测试验证组合键不会误触发发送。 */ async () => {
    render(<OpsConversationPanel credentialsReady />);
    fireEvent.click(await screen.findByText('群 · GROUP-OP'));
    // input 是人工消息输入框。
    const input = await screen.findByLabelText('发送运营消息') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: '第一行' } });
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
    expect(sendMock).not.toHaveBeenCalled();
    expect(input.value).toBe('第一行');
  });

  test('输入长度受 1500 字符上限约束并显示计数', /* 当前测试验证超长输入被截断且计数同步。 */ async () => {
    render(<OpsConversationPanel credentialsReady />);
    fireEvent.click(await screen.findByText('群 · GROUP-OP'));
    // input 是人工消息输入框。
    const input = await screen.findByLabelText('发送运营消息') as HTMLTextAreaElement;
    expect(input.getAttribute('maxlength')).toBe('1500');
    fireEvent.change(input, { target: { value: 'a'.repeat(1600) } });
    expect(input.value).toHaveLength(1500);
    expect(screen.getByText('1500/1500')).toBeTruthy();
    expect((screen.getByRole('button', { name: /发送/ }) as HTMLButtonElement).disabled).toBe(false);
  });

  test('发送失败保留输入原文并展示错误', /* 当前测试验证失败不清空用户已输入的内容。 */ async () => {
    sendMock.mockRejectedValueOnce(new Error('发送失败了'));
    render(<OpsConversationPanel credentialsReady />);
    fireEvent.click(await screen.findByText('群 · GROUP-OP'));
    // input 是人工消息输入框。
    const input = await screen.findByLabelText('发送运营消息') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: '还在吗' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(await screen.findByText('发送失败了')).toBeTruthy();
    expect(input.value).toBe('还在吗');
  });

  test('空列表与空消息展示可解释空态', /* 当前测试验证没有数据时不渲染空白区域。 */ async () => {
    conversationsMock.mockResolvedValue({ conversations: [], total: 0, page: 1, pageSize: 20, totalPages: 0 });
    render(<OpsConversationPanel credentialsReady />);
    expect(await screen.findByText('暂无会话')).toBeTruthy();
    expect(screen.getByText('选择左侧会话查看消息')).toBeTruthy();
  });
});
