// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import CustomerAgent from './CustomerAgent';
import { formatLocalDateTime } from '../../../../dateTime';
import {
  fetchAIModels,getAgentSupportSettings,getAgentTakeoverConversations,getAgentTakeoverMessages,
  getSystemSettings,updateAgentSupportSettings,updateSystemSettings,verifySession,
} from '../api';
import type { AgentConversationMessage,AgentTakeoverConversationPage,SystemSettings } from '../models';

vi.mock('../api', /* mockSettingsApi 用可断言的替身替换客服 Agent 页依赖的适配层，避免测试触网。 */ () => ({
  fetchAIModels: vi.fn(),
  getAgentSupportSettings: vi.fn(),
  getAgentTakeoverConversations: vi.fn(),
  getAgentTakeoverMessages: vi.fn(),
  getSystemSettings: vi.fn(),
  testAIConnection: vi.fn(),
  updateAgentSupportSettings: vi.fn(),
  updateLoginCredentials: vi.fn(),
  updateSystemSettings: vi.fn(),
  verifySession: vi.fn(),
}));

// getSettingsMock 是系统设置读取的替身，负责让页级 AI 草稿离开加载态。
const getSettingsMock = vi.mocked(getSystemSettings);
// verifySessionMock 是会话校验的替身，页面只消费其中的用户名。
const verifySessionMock = vi.mocked(verifySession);
// fetchModelsMock 是模型发现的替身，返回固定候选列表。
const fetchModelsMock = vi.mocked(fetchAIModels);
// updateSettingsMock 是页级 AI 配置保存的替身。
const updateSettingsMock = vi.mocked(updateSystemSettings);
// agentSettingsMock 是能力档位读取的替身。
const agentSettingsMock = vi.mocked(getAgentSupportSettings);
// agentSettingsUpdateMock 是能力档位保存的替身。
const agentSettingsUpdateMock = vi.mocked(updateAgentSupportSettings);
// takeoverConversationsMock 是接管会话列表读取的替身。
const takeoverConversationsMock = vi.mocked(getAgentTakeoverConversations);
// takeoverMessagesMock 是接管会话消息读取的替身。
const takeoverMessagesMock = vi.mocked(getAgentTakeoverMessages);

// AI_SETTINGS_FIXTURE 是页级 AI 表单渲染所需的最小系统设置快照。
const AI_SETTINGS_FIXTURE: SystemSettings = {
  ai_api_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
  ai_api_key: 'sk-test',
  ai_model: 'qwen-plus',
};

// TAKEOVER_PAGE_FIXTURE 是分页响应：一条 AI 接管过的会话，字段覆盖列表展示所需信息。
const TAKEOVER_PAGE_FIXTURE: AgentTakeoverConversationPage = {
  sessions: [{
    accountId: 'acc-1',
    chatId: 'chat-1',
    peerUserId: 'buyer-1',
    peerName: '买家小明',
    peerAvatarUrl: '',
    accountRole: 'seller',
    buyerUserId: 'buyer-1',
    sellerUserId: 'seller-1',
    roleItemId: '',
    roleSource: '',
    itemId: 'item-1',
    itemTitle: '二手机械键盘',
    itemImageUrl: '',
    lastMessage: '还在吗',
    lastMessageAt: 1_700_000_000_000,
    unreadCount: 1,
    takeoverCount: 3,
  }],
  hasMore: false,
  nextBeforeLastMessageAt: undefined,
  nextBeforeChatId: undefined,
};

// TAKEOVER_MESSAGES_FIXTURE 是会话消息分页：一条入站文本与一条出站图片。
const TAKEOVER_MESSAGES_FIXTURE: AgentConversationMessage[] = [
  {
    id: 1, accountId: 'acc-1', chatId: 'chat-1', messageKey: 'm1', direction: 'incoming',
    senderId: 'buyer-1', senderName: '买家小明', messageType: 'text', content: '你好，还有货吗',
    mediaDuration: 0, status: 'sent', readStatus: 1, readAt: 1_700_000_000_000, sentAt: 1_700_000_000_000,
  },
  {
    id: 2, accountId: 'acc-1', chatId: 'chat-1', messageKey: 'm2', direction: 'outgoing',
    senderId: 'seller-1', senderName: '卖家', messageType: 'image', content: 'https://example.com/a.png',
    mediaDuration: 0, status: 'sent', readStatus: 0, readAt: 0, sentAt: 1_700_000_010_000,
  },
];

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

beforeEach(/* 当前回调为每个用例重置适配器替身的默认成功响应。 */ () => {
  vi.clearAllMocks();
  getSettingsMock.mockResolvedValue(AI_SETTINGS_FIXTURE);
  verifySessionMock.mockResolvedValue({ authenticated: true, username: 'admin' });
  fetchModelsMock.mockResolvedValue(['qwen-plus']);
  updateSettingsMock.mockResolvedValue({ success: true });
  agentSettingsMock.mockResolvedValue({
    preset: 'readonly',
    presets: [
      { value: 'readonly', label: '只读', description: '只查询与答复，不产生对外动作' },
      { value: 'advanced', label: '高级', description: '在标准之上允许可逆的数据写入' },
    ],
  });
  agentSettingsUpdateMock.mockResolvedValue();
  takeoverConversationsMock.mockResolvedValue(TAKEOVER_PAGE_FIXTURE);
  takeoverMessagesMock.mockResolvedValue({ messages: TAKEOVER_MESSAGES_FIXTURE, hasMore: false });
});

describe('CustomerAgent', /* 当前测试组验证客服 Agent 页头、能力档位保存与接管历史只读回放。 */ () => {
  test('页头展示客服 Agent 与共用说明', /* 当前测试验证页头文案与副标题归属。 */ async () => {
    render(<CustomerAgent />);
    expect(await screen.findByRole('heading', { name: '客服 Agent' })).toBeTruthy();
    expect(screen.getByText('AI 引擎与接管范围；被规则通道与运营 Agent 共用')).toBeTruthy();
  });

  test('能力档位保存只提交档位', /* 当前测试验证档位保存走区块级保存条且载荷只有档位。 */ async () => {
    render(<CustomerAgent />);
    fireEvent.click(await screen.findByRole('button', { name: /高级/ }));
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(/* 当前回调等待档位保存请求发出。 */ () => expect(agentSettingsUpdateMock).toHaveBeenCalledWith({ preset: 'advanced' }));
    expect(await screen.findByText('客服 Agent 配置已保存')).toBeTruthy();
  });

  test('接管历史加载会话并展开只读消息', /* 当前测试验证列表字段、消息方向与非文本占位。 */ async () => {
    render(<CustomerAgent />);
    // 会话列表展示买家、商品、最近消息时间与接管次数。
    expect(await screen.findByText('买家小明')).toBeTruthy();
    expect(screen.getByText('商品 二手机械键盘')).toBeTruthy();
    expect(screen.getByText(`接管 3 次`)).toBeTruthy();
    expect(screen.getByText(formatLocalDateTime(TAKEOVER_PAGE_FIXTURE.sessions[0].lastMessageAt))).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /买家小明/ }));
    expect(await screen.findByText('你好，还有货吗')).toBeTruthy();
    expect(screen.getByText('[图片]')).toBeTruthy();
    await waitFor(/* 当前回调等待消息请求携带会话身份。 */ () => expect(takeoverMessagesMock).toHaveBeenCalledWith(
      expect.objectContaining({ accountId: 'acc-1', chatId: 'chat-1' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    ));
  });

  test('接管历史空态显示可解释文案', /* 当前测试验证没有接管会话时不渲染空列表。 */ async () => {
    takeoverConversationsMock.mockResolvedValue({ sessions: [], hasMore: false });
    render(<CustomerAgent />);
    expect(await screen.findByText('暂无 AI 接管过的会话')).toBeTruthy();
  });

  test('接管历史读取失败时可重试恢复', /* 当前测试验证失败信息落在页面上且可恢复。 */ async () => {
    takeoverConversationsMock.mockRejectedValueOnce(new Error('读取接管会话失败'));
    render(<CustomerAgent />);
    expect(await screen.findByText('读取接管会话失败')).toBeTruthy();
    // retryButton 是接管历史区块的重试入口。
    const retryButton = screen.getByRole('button', { name: '重新加载' });
    takeoverConversationsMock.mockResolvedValue(TAKEOVER_PAGE_FIXTURE);
    fireEvent.click(retryButton);
    expect(await screen.findByText('买家小明')).toBeTruthy();
  });
});
