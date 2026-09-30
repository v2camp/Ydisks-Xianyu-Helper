// @vitest-environment jsdom
import { cleanup, render, screen, act } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { SlaCountdownCell } from './SlaCountdownCell';

describe('SlaCountdownCell', /* 当前测试组验证倒计时单元格渲染与 30 秒本地重算。 */ () => {
  beforeEach(/* 当前回调启用伪造定时器并冻结系统时刻。 */ () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'));
  });

  afterEach(/* 当前回调清理 DOM 并恢复真实定时器。 */ () => {
    cleanup();
    vi.useRealTimers();
  });

  test('未启用显示破折号', /* 当前测试验证无截止时刻的占位展示。 */ () => {
    render(<SlaCountdownCell slaDeadline={null} slaMinutes={0} />);
    expect(screen.getByTestId('sla-countdown').textContent).toContain('—');
  });

  test('剩余时间按分秒展示', /* 当前测试验证剩余 5 分 30 秒的文案。 */ () => {
    // deadline 是 20 分钟窗口内剩余 5 分 30 秒的截止时刻，比例高于 20% 不预警。
    const deadline = new Date(Date.now() + 5 * 60_000 + 30_000).toISOString();
    render(<SlaCountdownCell slaDeadline={deadline} slaMinutes={20} />);
    expect(screen.getByTestId('sla-countdown').textContent).toContain('5 分 30 秒');
    expect(screen.queryByText('将超时')).toBeNull();
    expect(screen.queryByText('已超时')).toBeNull();
  });

  test('剩余不足 20% 显示将超时徽标', /* 当前测试验证预警档位的徽标渲染。 */ () => {
    // deadline 是 100 分钟窗口内剩余 10 分钟的截止时刻。
    const deadline = new Date(Date.now() + 10 * 60_000).toISOString();
    render(<SlaCountdownCell slaDeadline={deadline} slaMinutes={100} />);
    expect(screen.getByText('将超时')).toBeTruthy();
  });

  test('已超时显示已超时徽标', /* 当前测试验证超时档位的红色文案与徽标。 */ () => {
    // deadline 是已超时 20 分钟的截止时刻。
    const deadline = new Date(Date.now() - 20 * 60_000).toISOString();
    render(<SlaCountdownCell slaDeadline={deadline} slaMinutes={60} />);
    expect(screen.getByText('已超时')).toBeTruthy();
    expect(screen.getByTestId('sla-countdown').textContent).toContain('已超时 20 分 0 秒');
  });

  test('每 30 秒本地重算剩余时间', /* 当前测试验证定时器推进后倒计时文案刷新。 */ () => {
    // deadline 是固定时刻后 10 分钟的截止时刻。
    const deadline = new Date(Date.now() + 10 * 60_000).toISOString();
    render(<SlaCountdownCell slaDeadline={deadline} slaMinutes={60} />);
    expect(screen.getByTestId('sla-countdown').textContent).toContain('10 分 0 秒');
    act(/* advance 通过伪造定时器推进 30 秒触发本地重算。 */ () => {
      vi.advanceTimersByTime(30_000);
    });
    expect(screen.getByTestId('sla-countdown').textContent).toContain('9 分 30 秒');
    act(/* advance 再推进 30 秒确认持续重算。 */ () => {
      vi.advanceTimersByTime(30_000);
    });
    expect(screen.getByTestId('sla-countdown').textContent).toContain('9 分 0 秒');
  });

  test('卸载后清理定时器', /* 当前测试验证组件卸载时移除本地重算定时器。 */ () => {
    // deadline 是固定时刻后 10 分钟的截止时刻。
    const deadline = new Date(Date.now() + 10 * 60_000).toISOString();
    // view 是本次渲染结果，随后立即卸载。
    const view = render(<SlaCountdownCell slaDeadline={deadline} slaMinutes={60} />);
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
