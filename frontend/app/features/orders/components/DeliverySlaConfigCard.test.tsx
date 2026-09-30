// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { getDeliverySlaConfigRaw, getSlaAccounts, updateDeliverySlaConfig } from '../api';
import { DeliverySlaConfigCard } from './DeliverySlaConfigCard';

// 通过模块替身隔离 SLA 配置卡的网络读写。
vi.mock('../api', /* slaApiMockFactory 提供 SLA 配置读写与账号下拉替身。 */ () => ({
  getDeliverySlaConfigRaw: vi.fn(),
  getSlaAccounts: vi.fn(),
  updateDeliverySlaConfig: vi.fn(),
}));

// rawMock 是系统设置 delivery_sla_config 读取替身。
const rawMock = vi.mocked(getDeliverySlaConfigRaw);
// accountsMock 是账号下拉读取替身。
const accountsMock = vi.mocked(getSlaAccounts);
// updateMock 是 SLA 配置保存替身。
const updateMock = vi.mocked(updateDeliverySlaConfig);

describe('DeliverySlaConfigCard', /* 当前测试组验证 SLA 配置卡的读取展示、保存与非法 JSON 回退。 */ () => {
  beforeEach(/* 当前回调重置 SLA API 替身并提供默认账号下拉。 */ () => {
    rawMock.mockResolvedValue(JSON.stringify({ default_minutes: 30, per_account: { acc1: 45 } }));
    accountsMock.mockResolvedValue([
      { cookie_id: 'acc1', label: '账号一' },
      { cookie_id: 'acc2', label: '账号二' },
    ]);
    updateMock.mockResolvedValue({ success: true });
  });

  afterEach(/* 当前回调清理 DOM 与 API 替身。 */ () => {
    cleanup();
    vi.clearAllMocks();
  });

  test('非管理员不渲染配置卡', /* 当前测试验证 visible 为假时无任何 SLA 配置 UI。 */ () => {
    render(<DeliverySlaConfigCard visible={false} />);
    expect(screen.queryByText('发货 SLA')).toBeNull();
    expect(rawMock).not.toHaveBeenCalled();
  });

  test('读取配置并展示默认分钟数与账号覆盖', /* 当前测试验证首屏读取与表单回填。 */ async () => {
    render(<DeliverySlaConfigCard visible />);
    await waitFor(/* loadAssertion 等待配置与账号并行读取完成。 */ () => expect(rawMock).toHaveBeenCalledTimes(1));
    // defaultInput 是默认分钟数输入框，应回填 30。
    const defaultInput = screen.getByLabelText('默认分钟数') as HTMLInputElement;
    expect(defaultInput.value).toBe('30');
    expect(screen.getByLabelText('覆盖分钟数 acc1')).toBeTruthy();
    expect(screen.getByText('账号一')).toBeTruthy();
  });

  test('修改默认分钟数并保存写回 JSON 串', /* 当前测试验证编辑后的序列化保存。 */ async () => {
    render(<DeliverySlaConfigCard visible />);
    await waitFor(/* loadAssertion 等待首屏读取完成。 */ () => expect(rawMock).toHaveBeenCalledTimes(1));
    fireEvent.change(screen.getByLabelText('默认分钟数'), { target: { value: '60' } });
    fireEvent.click(screen.getByText('保存 SLA 配置'));
    await waitFor(/* saveAssertion 等待保存请求完成。 */ () => expect(updateMock).toHaveBeenCalledTimes(1));
    // saved 是保存调用写入的 JSON 串。
    const saved = String(updateMock.mock.calls[0][0]);
    expect(JSON.parse(saved)).toEqual({ default_minutes: 60, per_account: { acc1: 45 } });
    await waitFor(/* messageAssertion 等待保存成功提示出现。 */ () => expect(screen.getByText('发货 SLA 配置已保存')).toBeTruthy());
  });

  test('非法 JSON 回退默认并提示，保存后覆盖为合法 JSON', /* 当前测试验证非法原文回退与修复路径。 */ async () => {
    rawMock.mockResolvedValue('{not json');
    render(<DeliverySlaConfigCard visible />);
    await waitFor(/* loadAssertion 等待读取完成。 */ () => expect(screen.getByText(/配置 JSON 非法/)).toBeTruthy());
    // defaultInput 是非法 JSON 回退后的默认分钟数输入框，应为 0。
    const defaultInput = screen.getByLabelText('默认分钟数') as HTMLInputElement;
    expect(defaultInput.value).toBe('0');
    fireEvent.click(screen.getByText('保存 SLA 配置'));
    await waitFor(/* saveAssertion 等待保存请求完成。 */ () => expect(updateMock).toHaveBeenCalledTimes(1));
    // saved 是覆盖写回的 JSON 串，应为合法对象。
    const saved = String(updateMock.mock.calls[0][0]);
    expect(JSON.parse(saved)).toEqual({ default_minutes: 0, per_account: {} });
  });

  test('账号覆盖行支持新增与删除', /* 当前测试验证覆盖列表的增删交互。 */ async () => {
    render(<DeliverySlaConfigCard visible />);
    await waitFor(/* loadAssertion 等待首屏读取完成。 */ () => expect(rawMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByText('添加账号覆盖'));
    // secondLabel 是新增行选中的第二个账号下拉标签。
    expect(screen.getAllByText('账号二').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByLabelText('删除账号覆盖 acc1'));
    expect(screen.queryByLabelText('覆盖分钟数 acc1')).toBeNull();
    fireEvent.click(screen.getByText('保存 SLA 配置'));
    await waitFor(/* saveAssertion 等待保存请求完成。 */ () => expect(updateMock).toHaveBeenCalledTimes(1));
    // saved 是覆盖增删后写入的 JSON 串。
    const saved = String(updateMock.mock.calls[0][0]);
    expect(JSON.parse(saved)).toEqual({ default_minutes: 30, per_account: { acc2: 30 } });
  });
});
