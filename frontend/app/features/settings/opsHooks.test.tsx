// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { OpsConversation,OpsMessage,OpsMessagePage } from './models';
import { getOpsConversations,getOpsMessages,sendOpsMessage } from './api';
import { useOpsConversations } from './opsHooks';

vi.mock('./api', /* opsApiMockFactory 提供运营对话 Hook 的确定性 API 替身。 */ () => ({
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

// conversationA 是首个群聊会话摘要。
const conversationA: OpsConversation = {
  id: 1, channel: 'qq', targetOpenid: 'GROUP-OPENID-0001', isGroup: true,
  lastMessageAt: 1_700_000_000_000, lastMessagePreview: '早上好',
};
// conversationB 是第二个单聊会话摘要。
const conversationB: OpsConversation = {
  id: 2, channel: 'qq', targetOpenid: 'USER-OPENID-0002', isGroup: false,
  lastMessageAt: 1_700_000_100_000, lastMessagePreview: '在吗',
};
// messageA 是会话 A 的入站消息。
const messageA: OpsMessage = { id: 11, conversationId: 1, direction: 'inbound', source: 'inbound', content: '早上好', createdAt: 1_700_000_000_000 };
// messageB 是会话 B 的入站消息。
const messageB: OpsMessage = { id: 21, conversationId: 2, direction: 'inbound', source: 'inbound', content: '在吗', createdAt: 1_700_000_100_000 };
// outboundMessage 是发送成功后服务端返回的出站消息。
const outboundMessage: OpsMessage = { id: 12, conversationId: 1, direction: 'outbound', source: 'operator', content: '收到', createdAt: 1_700_000_200_000 };

// pageOf 构造单页会话列表响应，totalPages 用于控制是否还有更多。
const pageOf = (conversations: OpsConversation[], page = 1, totalPages = 1) => ({
  conversations,
  total: conversations.length,
  page,
  pageSize: 20,
  totalPages,
});

beforeEach(/* 当前回调重置运营 API 替身的默认成功响应。 */ () => {
  vi.clearAllMocks();
  conversationsMock.mockResolvedValue(pageOf([conversationA, conversationB]));
  messagesMock.mockResolvedValue({ messages: [messageA], hasMore: false });
  sendMock.mockResolvedValue({ message: outboundMessage, conversation: { ...conversationA, lastMessagePreview: '收到', lastMessageAt: outboundMessage.createdAt } });
});

describe('useOpsConversations', /* 当前测试组验证会话分页、消息分页、切换会话与人工发送。 */ () => {
  test('加载会话并在选中后读取消息', /* 当前测试验证首屏会话与消息请求的入参。 */ async () => {
    // hook 是运营对话区的渲染结果。
    const hook = renderHook(/* opsHookFactory 创建运营对话 Hook。 */ () => useOpsConversations());
    await waitFor(/* loadedAssertion 等待会话列表加载完成。 */ () => expect(hook.result.current.conversationsLoading).toBe(false));
    expect(hook.result.current.conversations).toEqual([conversationA, conversationB]);
    expect(conversationsMock).toHaveBeenCalledWith(expect.objectContaining({ page: 1 }), expect.objectContaining({ signal: expect.any(AbortSignal) }));

    await act(/* selectAction 选中群聊会话。 */ async () => hook.result.current.selectConversation(conversationA));
    expect(messagesMock).toHaveBeenCalledWith(1, expect.objectContaining({ beforeId: undefined }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    await waitFor(/* messagesAssertion 等待消息落地。 */ () => expect(hook.result.current.messages).toEqual([messageA]));
    expect(hook.result.current.selected).toEqual(conversationA);
    hook.unmount();
  });

  test('切换会话时丢弃旧会话的晚到响应', /* 当前测试验证消息代次隔离。 */ async () => {
    // resolveStale 控制旧会话消息响应的完成时机。
    let resolveStale: (value: OpsMessagePage) => void = () => undefined;
    // hook 是运营对话区的渲染结果。
    const hook = renderHook(/* staleHookFactory 创建运营对话 Hook。 */ () => useOpsConversations());
    await waitFor(/* loadedAssertion 等待会话列表加载完成。 */ () => expect(hook.result.current.conversationsLoading).toBe(false));

    messagesMock.mockImplementationOnce(/* pendingStaleResponse 暂存旧会话响应。 */ () => new Promise(/* staleExecutor 保存旧响应完成函数。 */ resolve => { resolveStale = resolve; }));
    await act(/* selectStaleAction 选中旧会话，消息请求保持挂起。 */ async () => hook.result.current.selectConversation(conversationA));
    messagesMock.mockResolvedValueOnce({ messages: [messageB], hasMore: false });
    await act(/* selectFreshAction 切到新会话并先完成新响应。 */ async () => hook.result.current.selectConversation(conversationB));
    await waitFor(/* freshAssertion 等待新会话消息落地。 */ () => expect(hook.result.current.messages.map(/* message 是当前消息流中的消息。 */ message => message.id)).toEqual([messageB.id]));

    await act(/* resolveStaleAction 让旧会话响应最后到达。 */ async () => {
      resolveStale({ messages: [messageA], hasMore: false });
      await Promise.resolve();
    });
    expect(hook.result.current.messages.map(/* message 是当前消息流中的消息。 */ message => message.id)).toEqual([messageB.id]);
    hook.unmount();
  });

  test('发送成功增量追加消息并更新列表项，不整表重拉', /* 当前测试验证发送后的本地增量更新。 */ async () => {
    // hook 是运营对话区的渲染结果。
    const hook = renderHook(/* sendHookFactory 创建运营对话 Hook。 */ () => useOpsConversations());
    await waitFor(/* loadedAssertion 等待会话列表加载完成。 */ () => expect(hook.result.current.conversationsLoading).toBe(false));
    await act(/* selectAction 选中群聊会话。 */ async () => hook.result.current.selectConversation(conversationA));
    await waitFor(/* messagesLoaded 等待首屏消息落地后再发送。 */ () => expect(hook.result.current.messages).toEqual([messageA]));
    // conversationRequests 记录发送前的会话列表请求次数。
    const conversationRequests = conversationsMock.mock.calls.length;

    // sent 是本次发送是否成功。
    let sent = false;
    await act(/* sendAction 提交带首尾空白的人工消息。 */ async () => { sent = await hook.result.current.send('  收到  '); });
    expect(sent).toBe(true);
    expect(sendMock).toHaveBeenCalledWith(1, '收到', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.messages.map(/* message 是当前消息流中的消息。 */ message => message.id)).toEqual([messageA.id, outboundMessage.id]);
    expect(hook.result.current.conversations[0].lastMessagePreview).toBe('收到');
    expect(hook.result.current.conversations).toHaveLength(2);
    // 发送成功后不整表重拉会话列表。
    expect(conversationsMock.mock.calls.length).toBe(conversationRequests);
    hook.unmount();
  });

  test('发送失败返回 false 并保留错误，不写入消息', /* 当前测试验证失败不产生半条消息。 */ async () => {
    sendMock.mockRejectedValueOnce(new Error('发送失败了'));
    // hook 是运营对话区的渲染结果。
    const hook = renderHook(/* failureHookFactory 创建运营对话 Hook。 */ () => useOpsConversations());
    await waitFor(/* loadedAssertion 等待会话列表加载完成。 */ () => expect(hook.result.current.conversationsLoading).toBe(false));
    await act(/* selectAction 选中群聊会话。 */ async () => hook.result.current.selectConversation(conversationA));
    await waitFor(/* messagesLoaded 等待首屏消息落地。 */ () => expect(hook.result.current.messages).toEqual([messageA]));

    // sent 是本次发送是否成功；失败时必须为 false 以便组件保留输入。
    let sent = true;
    await act(/* failedSendAction 触发发送失败。 */ async () => { sent = await hook.result.current.send('收到'); });
    expect(sent).toBe(false);
    expect(hook.result.current.sendError).toBe('发送失败了');
    expect(hook.result.current.messages.map(/* message 是当前消息流中的消息。 */ message => message.id)).toEqual([messageA.id]);
    hook.unmount();
  });

  test('空内容与未选中会话不会发出请求', /* 当前测试验证发送前的守卫。 */ async () => {
    // hook 是运营对话区的渲染结果。
    const hook = renderHook(/* guardHookFactory 创建运营对话 Hook。 */ () => useOpsConversations());
    await waitFor(/* loadedAssertion 等待会话列表加载完成。 */ () => expect(hook.result.current.conversationsLoading).toBe(false));
    await act(/* emptySendAction 未选中会话时发送。 */ async () => { expect(await hook.result.current.send('收到')).toBe(false); });
    await act(/* selectAction 选中会话。 */ async () => hook.result.current.selectConversation(conversationA));
    await act(/* blankSendAction 仅空白内容发送。 */ async () => { expect(await hook.result.current.send('   ')).toBe(false); });
    expect(sendMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('会话与消息都支持分页追加', /* 当前测试验证列表加载更多与消息加载更早。 */ async () => {
    conversationsMock.mockResolvedValueOnce(pageOf([conversationA], 1, 2));
    messagesMock.mockResolvedValueOnce({ messages: [messageA], hasMore: true });
    // hook 是运营对话区的渲染结果。
    const hook = renderHook(/* paginationHookFactory 创建运营对话 Hook。 */ () => useOpsConversations());
    await waitFor(/* loadedAssertion 等待首屏会话加载完成。 */ () => expect(hook.result.current.conversationsLoading).toBe(false));
    expect(hook.result.current.conversationsHasMore).toBe(true);

    conversationsMock.mockResolvedValueOnce(pageOf([conversationB], 2, 2));
    await act(/* moreSessionsAction 加载下一页会话。 */ async () => hook.result.current.loadMoreConversations());
    expect(conversationsMock).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }), expect.anything());
    expect(hook.result.current.conversations.map(/* conversation 是会话列表中的会话。 */ conversation => conversation.id)).toEqual([1, 2]);
    expect(hook.result.current.conversationsHasMore).toBe(false);

    await act(/* selectAction 选中群聊会话。 */ async () => hook.result.current.selectConversation(conversationA));
    await waitFor(/* hasMoreAssertion 等待首屏消息与分页标记落地。 */ () => expect(hook.result.current.messagesHasMore).toBe(true));
    // olderMessage 是更早一页消息。
    const olderMessage: OpsMessage = { id: 10, conversationId: 1, direction: 'inbound', source: 'inbound', content: '更早', createdAt: 1_600_000_000_000 };
    messagesMock.mockResolvedValueOnce({ messages: [olderMessage], hasMore: false });
    await act(/* earlierAction 加载更早消息。 */ async () => hook.result.current.loadEarlierMessages());
    expect(messagesMock).toHaveBeenLastCalledWith(1, expect.objectContaining({ beforeId: messageA.id }), expect.anything());
    await waitFor(/* earlierAssertion 等待更早消息合并到消息流。 */ () => expect(hook.result.current.messages.map(/* message 是当前消息流中的消息。 */ message => message.id)).toEqual([olderMessage.id, messageA.id]));
    hook.unmount();
  });
});
