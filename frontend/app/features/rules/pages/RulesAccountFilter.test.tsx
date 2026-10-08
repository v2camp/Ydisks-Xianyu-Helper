// @vitest-environment jsdom
import { fireEvent,render,screen,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { AccountDetail,ShippingRule } from '../api';
import { getAccountDetails,getAutomationIssues,getCards,getDefaultReplies,getDeliveryTemplates,getItems,getShippingRulesPage } from '../api';
import Rules from './Rules';

vi.mock('../api', /* rulesPageApiMockFactory 提供规则页账号筛选场景的确定性 API 替身。 */ () => ({
  clearDefaultReplyRecords: vi.fn(),
  deleteDefaultReply: vi.fn(),
  deleteReplyRule: vi.fn(),
  deleteShippingRule: vi.fn(),
  getAccountDetails: vi.fn(),
  getAutomationIssues: vi.fn(),
  getCards: vi.fn(),
  getDefaultReplies: vi.fn(),
  getDefaultReply: vi.fn(),
  getDeliveryTemplates: vi.fn(),
  getItems: vi.fn(),
  getReplyRules: vi.fn(),
  getShippingRules: vi.fn(),
  getShippingRulesPage: vi.fn(),
  resolveAutomationRun: vi.fn(),
  resolveDeferredAutomationTask: vi.fn(),
  updateDefaultReply: vi.fn(),
  updateReplyRule: vi.fn(),
  updateShippingRule: vi.fn(),
}));

// accountsMock 是规则页账号参考数据请求的可控替身。
const accountsMock = vi.mocked(getAccountDetails);
// cardsMock 是卡密参考数据请求的可控替身。
const cardsMock = vi.mocked(getCards);
// issuesMock 是自动化异常请求的可控替身。
const issuesMock = vi.mocked(getAutomationIssues);
// itemsMock 是商品参考数据请求的可控替身。
const itemsMock = vi.mocked(getItems);
// defaultsMock 是默认回复参考数据请求的可控替身。
const defaultsMock = vi.mocked(getDefaultReplies);
// templatesMock 是发货模板参考数据请求的可控替身。
const templatesMock = vi.mocked(getDeliveryTemplates);
// shippingPageMock 是自动化规则分页请求的可控替身。
const shippingPageMock = vi.mocked(getShippingRulesPage);

// firstAccount 是账号列表中的首个账号，对应缺陷复现中的「赚钱养家」。
const firstAccount = { id: 'account-first', enabled: true, auto_confirm: false, nickname: '赚钱养家' } as AccountDetail;
// secondAccount 是账号列表中的第二个账号，用于验证切换后不被覆盖。
const secondAccount = { id: 'account-second', enabled: true, auto_confirm: false, nickname: '闲置小号' } as AccountDetail;
// ruleFixture 是分页接口返回的一条自动化规则。
const ruleFixture = { id: '1', name: '付款发货', trigger_type: 'order_paid', cookie_id: 'account-first', item_keyword: '', card_group_id: 0, priority: 100, enabled: true, actions: [], variants: [], config_json: '{}' } as ShippingRule;

// accountFilterSelect 返回规则页顶部的账号筛选下拉框。
const accountFilterSelect = (): HTMLSelectElement => screen.getAllByRole('combobox')[0] as HTMLSelectElement;
// lastAutomationCookieId 返回最近一次自动化规则分页请求携带的账号筛选值。
const lastAutomationCookieId = (): string | undefined => shippingPageMock.mock.calls.at(-1)?.[0]?.cookieId;
// settle 等待账号切换触发的刷新链收敛，确保断言读取的是最终状态。
const settle = async (): Promise<void> => {
  await new Promise(/* settleTimer 让模拟请求的回调链全部执行完毕。 */ resolve => setTimeout(resolve, 200));
};

describe('自动化规则页账号筛选', /* 当前回调验证账号筛选只由用户操作决定，不会被参考数据加载改写。 */ () => {
  beforeEach(/* 当前回调重置规则页 API 替身为成功默认值。 */ () => {
    vi.clearAllMocks();
    accountsMock.mockResolvedValue([firstAccount, secondAccount]);
    cardsMock.mockResolvedValue([]);
    itemsMock.mockResolvedValue([]);
    defaultsMock.mockResolvedValue({});
    templatesMock.mockResolvedValue([]);
    issuesMock.mockResolvedValue({ runs: [], pending_tasks: [] });
    shippingPageMock.mockResolvedValue({ success: true, data: [ruleFixture], total: 1, page: 1, page_size: 10, total_pages: 1, trigger_counts: { order_paid: 1 } });
  });

  test('默认展示全部账号，刷新后不回落到首个账号', /* 当前回调复现并回归账号筛选被首个账号覆盖的缺陷。 */ async () => {
    render(<Rules />);
    // select 是首屏渲染出来的账号筛选控件。
    const select = accountFilterSelect();
    await waitFor(/* readyAction 等待账号参考数据加载出全部账号选项。 */ () => expect(select.options.length).toBe(3));

    expect(select.value).toBe('');
    expect(lastAutomationCookieId()).toBeUndefined();

    fireEvent.click(screen.getByRole('button', { name: '刷新' }));
    await settle();

    expect(select.value).toBe('');
    expect(lastAutomationCookieId()).toBeUndefined();
  });

  test('选择第二个账号后筛选值和请求参数都保持为第二个账号', /* 当前回调验证切换具体账号后不被刷新覆盖。 */ async () => {
    render(<Rules />);
    // select 是首屏渲染出来的账号筛选控件。
    const select = accountFilterSelect();
    await waitFor(/* readyAction 等待账号参考数据加载出全部账号选项。 */ () => expect(select.options.length).toBe(3));

    fireEvent.change(select, { target: { value: 'account-second' } });
    await settle();

    expect(select.value).toBe('account-second');
    expect(lastAutomationCookieId()).toBe('account-second');

    fireEvent.change(select, { target: { value: '' } });
    await settle();

    expect(select.value).toBe('');
    expect(lastAutomationCookieId()).toBeUndefined();
  });
});
