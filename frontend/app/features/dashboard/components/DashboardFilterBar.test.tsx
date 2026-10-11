// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import type { DashboardFilterBarProps } from './DashboardFilterBar';
import { DashboardFilterBar } from './DashboardFilterBar';

afterEach(/* 当前回调清理过滤器测试 DOM，避免多个用例共用文档产生重复节点。 */ () => cleanup());

// accountsFixture 是账号下拉选项的测试数据。
const accountsFixture = [{ id: 'account-1', name: '一号账号' }, { id: 'account-2', name: '二号账号' }];

// renderBar 渲染过滤器并返回可断言的回调替身。
const renderBar = (overrides: Partial<DashboardFilterBarProps> = {}) => {
  // handlers 是过滤器需要上报给页面的选择结果替身。
  const handlers = {
    onRangeChange: vi.fn(),
    onCustomRangeApply: vi.fn(),
    onCookieChange: vi.fn(),
  };
  // props 是当前测试使用的完整过滤器入参。
  const props: DashboardFilterBarProps = {
    range: '7days',
    customStartDate: '',
    customEndDate: '',
    cookieID: '',
    accounts: accountsFixture,
    summary: '2026-10-01 至 2026-10-08 · 全部账号',
    ...handlers,
    ...overrides,
  };
  render(<DashboardFilterBar {...props} />);
  return handlers;
};

describe('DashboardFilterBar', /* 当前回调验证时间范围与账号过滤器的交互流程。 */ () => {
  test('默认收起下拉并在展开后列出快捷区间与自定义入口', /* 当前回调验证下拉初始状态与选项清单。 */ () => {
    renderBar();
    expect(screen.queryByText('3 天内')).toBeNull();
    fireEvent.click(screen.getByLabelText('选择统计时间范围'));
    // menu 是时间下拉展开后的面板，选项文本可能与触发器重复故需限定范围。
    const menu = within(screen.getByRole('menu'));
    expect(menu.getByText('今天')).toBeTruthy();
    expect(menu.getByText('昨天')).toBeTruthy();
    expect(menu.getByText('3 天内')).toBeTruthy();
    expect(menu.getByText('7 天内')).toBeTruthy();
    expect(menu.getByText('1 个月内')).toBeTruthy();
    expect(menu.getByText('自定义区间')).toBeTruthy();
  });

  test('选择快捷区间后立即生效并收起面板', /* 当前回调验证快捷区间选择与面板收起。 */ () => {
    // handlers 是过滤器上报给页面的选择结果替身。
    const handlers = renderBar();
    fireEvent.click(screen.getByLabelText('选择统计时间范围'));
    fireEvent.click(screen.getByText('昨天'));
    expect(handlers.onRangeChange).toHaveBeenCalledWith('yesterday');
    expect(screen.queryByText('自定义区间')).toBeNull();
  });

  test('自定义区间在下拉内部展开且不提交非法草稿', /* 当前回调验证区间倒置时的失败分支。 */ () => {
    // handlers 是过滤器上报给页面的选择结果替身。
    const handlers = renderBar();
    fireEvent.click(screen.getByLabelText('选择统计时间范围'));
    fireEvent.click(screen.getByText('自定义区间'));
    fireEvent.change(screen.getByLabelText('自定义开始日期'), { target: { value: '2026-10-08' } });
    fireEvent.change(screen.getByLabelText('自定义结束日期'), { target: { value: '2026-10-01' } });
    expect(screen.getByText('开始日期不能晚于结束日期')).toBeTruthy();
    // confirmButton 是区间非法时被禁用的确定按钮。
    const confirmButton = screen.getByText('确定').closest('button');
    expect(confirmButton?.disabled).toBe(true);
    expect(handlers.onCustomRangeApply).not.toHaveBeenCalled();
  });

  test('确认合法自定义区间后提交并收起面板', /* 当前回调验证自定义区间成功分支。 */ () => {
    // handlers 是过滤器上报给页面的选择结果替身。
    const handlers = renderBar();
    fireEvent.click(screen.getByLabelText('选择统计时间范围'));
    fireEvent.click(screen.getByText('自定义区间'));
    fireEvent.change(screen.getByLabelText('自定义开始日期'), { target: { value: '2026-10-01' } });
    fireEvent.change(screen.getByLabelText('自定义结束日期'), { target: { value: '2026-10-08' } });
    fireEvent.click(screen.getByText('确定'));
    expect(handlers.onCustomRangeApply).toHaveBeenCalledWith('2026-10-01', '2026-10-08');
    expect(screen.queryByLabelText('自定义开始日期')).toBeNull();
  });

  test('取消自定义编辑后回到快捷区间列表且不提交', /* 当前回调验证自定义取消分支。 */ () => {
    // handlers 是过滤器上报给页面的选择结果替身。
    const handlers = renderBar();
    fireEvent.click(screen.getByLabelText('选择统计时间范围'));
    fireEvent.click(screen.getByText('自定义区间'));
    fireEvent.click(screen.getByText('取消'));
    expect(handlers.onCustomRangeApply).not.toHaveBeenCalled();
    expect(screen.getByText('1 个月内')).toBeTruthy();
  });

  test('切换账号时上报账号标识并收起面板', /* 当前回调验证账号过滤切换分支。 */ () => {
    // handlers 是过滤器上报给页面的选择结果替身。
    const handlers = renderBar();
    fireEvent.click(screen.getByLabelText('按闲鱼账号筛选'));
    fireEvent.click(screen.getByText('二号账号'));
    expect(handlers.onCookieChange).toHaveBeenCalledWith('account-2');
    expect(screen.queryByText('一号账号')).toBeNull();
  });

  test('Esc 与外部点击都能收起已展开的面板', /* 当前回调验证浮层的键盘与鼠标关闭路径。 */ () => {
    renderBar();
    fireEvent.click(screen.getByLabelText('选择统计时间范围'));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText('自定义区间')).toBeNull();
    fireEvent.click(screen.getByLabelText('按闲鱼账号筛选'));
    fireEvent.mouseDown(document.body);
    expect(screen.queryByText('二号账号')).toBeNull();
  });

  test('自定义范围生效后触发器展示压缩日期区间', /* 当前回调验证触发器文本的自定义形态。 */ () => {
    renderBar({ range: 'custom', customStartDate: '2026-10-01', customEndDate: '2026-10-08' });
    // trigger 是时间下拉触发器按钮。
    const trigger = screen.getByLabelText('选择统计时间范围');
    expect(trigger.textContent).toContain('10-01 ~ 10-08');
  });
});
