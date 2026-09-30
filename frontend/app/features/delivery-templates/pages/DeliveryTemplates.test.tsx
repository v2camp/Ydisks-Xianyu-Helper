// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { listDeliveryTemplates } from '../api';
import { COPY_RISK_BANNER_TEXT, DUPLICATE_COPY_WARNING } from '../copyRisk';
import type { DeliveryTemplate } from '../types';
import DeliveryTemplates from './DeliveryTemplates';

vi.mock('../api', /* deliveryTemplatesApiMockFactory 提供发货模板页面的列表请求替身。 */ () => ({
  createDeliveryTemplate: vi.fn(),
  deleteDeliveryTemplate: vi.fn(),
  listDeliveryTemplates: vi.fn(),
  updateDeliveryTemplate: vi.fn(),
}));

// listDeliveryTemplatesMock 是模板列表读取接口的可控替身。
const listDeliveryTemplatesMock = vi.mocked(listDeliveryTemplates);

// emptyTemplateFixture 是空模板列表请求返回的稳定测试数据。
const emptyTemplateFixture: DeliveryTemplate[] = [];

// templatesWithSharedCopyFixture 是含重复话术风险的多模板测试数据。
const templatesWithSharedCopyFixture: DeliveryTemplate[] = [
  {
    id: 1,
    name: '模板甲',
    enabled: true,
    messages: [{ id: 11, sort_order: 1, content: '感谢购买！卡密如下' }],
    keys: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 2,
    name: '模板乙',
    enabled: true,
    messages: [{ id: 21, sort_order: 1, content: '欢迎再来～' }],
    keys: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
];

describe('DeliveryTemplates 页面组合行为', /* 当前回调验证模板编辑器的打开状态和空白新建流程。 */ () => {
  beforeEach(/* 当前回调重置模板接口替身。 */ () => {
    listDeliveryTemplatesMock.mockResolvedValue(emptyTemplateFixture);
  });

  afterEach(/* 当前回调清理模板页面 DOM 和接口替身。 */ () => {
    cleanup();
    vi.clearAllMocks();
  });

  test('点击新建模板会显示空白编辑器', /* 当前回调验证空白新建草稿不会因为名称为空而被条件渲染隐藏。 */ async () => {
    render(<DeliveryTemplates />);
    await waitFor(/* loadAssertion 等待初始模板列表请求完成后再触发用户操作。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));

    // createButton 是页面标题区域触发新建模板的用户操作入口。
    const createButton = screen.getByRole('button', { name: '新建模板' });
    expect(screen.queryByText('新建发货模板')).toBeNull();
    fireEvent.click(createButton);

    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByText('新建发货模板')).toBeTruthy();
    expect(screen.getByPlaceholderText('例如：数字产品发货')).toBeTruthy();
    expect(screen.getByText('内置、卡密、自定义变量均用双大括号')).toBeTruthy();
    expect(screen.getByText('{{buyer_nickname}}')).toBeTruthy();
    expect(screen.getByText('{{custom.<变量名>}}')).toBeTruthy();
    expect(screen.getByText('如何声明和使用')).toBeTruthy();

    // cancelButton 是浮窗底部放弃当前未保存模板草稿的操作入口。
    const cancelButton = screen.getByRole('button', { name: '取消' });
    fireEvent.click(cancelButton);
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});

describe('发货模板话术风控提示', /* 当前测试组验证编辑器顶部提示条与重复话术行旁警示。 */ () => {
  beforeEach(/* 当前回调准备含其它模板话术的列表数据。 */ () => {
    listDeliveryTemplatesMock.mockResolvedValue(templatesWithSharedCopyFixture);
  });

  afterEach(/* 当前回调清理模板页面 DOM 和接口替身。 */ () => {
    cleanup();
    vi.clearAllMocks();
  });

  test('编辑器顶部固定提示话术风控', /* 当前测试验证提示条在编辑器内常驻。 */ async () => {
    render(<DeliveryTemplates />);
    await waitFor(/* loadAssertion 等待初始模板列表请求完成。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));

    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByText(COPY_RISK_BANNER_TEXT)).toBeTruthy();
  });

  test('与其它模板文案一致时行旁出现警示', /* 当前测试验证重复话术命中警示。 */ async () => {
    render(<DeliveryTemplates />);
    await waitFor(/* loadAssertion 等待初始模板列表请求完成。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    expect(screen.queryByText(DUPLICATE_COPY_WARNING)).toBeNull();

    // messageInput 是新建模板的首条消息输入框。
    const messageInput = screen.getByPlaceholderText('例如：感谢购买，您的卡密是 {{cards.main}}');
    fireEvent.change(messageInput, { target: { value: '  感谢购买！卡密如下  ' } });
    expect(screen.getByText(DUPLICATE_COPY_WARNING)).toBeTruthy();

    fireEvent.change(messageInput, { target: { value: '差异化的新话术内容' } });
    expect(screen.queryByText(DUPLICATE_COPY_WARNING)).toBeNull();
  });

  test('编辑模板时不与自身消息比对', /* 当前测试验证当前模板原文不触发重复警示。 */ async () => {
    render(<DeliveryTemplates />);
    await waitFor(/* loadAssertion 等待初始模板列表请求完成。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getAllByText('编辑')[0]);

    // messageInput 是模板甲首条消息输入框，内容与模板甲自身一致。
    const messageInput = screen.getByDisplayValue('感谢购买！卡密如下');
    expect(screen.queryByText(DUPLICATE_COPY_WARNING)).toBeNull();
    fireEvent.change(messageInput, { target: { value: '欢迎再来～' } });
    expect(screen.getByText(DUPLICATE_COPY_WARNING)).toBeTruthy();
  });
});
