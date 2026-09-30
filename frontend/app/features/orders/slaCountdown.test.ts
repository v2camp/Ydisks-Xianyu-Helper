import { describe, expect, test } from 'vitest';
import { formatSlaDuration, slaCountdownView } from './slaCountdown';

// fixedNow 是倒计时测试使用的固定本地时刻：2026-01-01T00:00:00Z。
const fixedNow = Date.parse('2026-01-01T00:00:00Z');

describe('slaCountdownView', /* 当前测试组验证发货倒计时的未启用、剩余、预警与超时档位。 */ () => {
  test('未启用显示破折号', /* 当前测试验证没有截止时刻时按未启用展示。 */ () => {
    expect(slaCountdownView(undefined, 0, fixedNow)).toEqual({ text: '—', tone: 'muted' });
    expect(slaCountdownView(null, 60, fixedNow)).toEqual({ text: '—', tone: 'muted' });
  });

  test('非法截止时刻按未启用展示', /* 当前测试验证无法解析的截止时刻回退破折号。 */ () => {
    expect(slaCountdownView('not-a-date', 30, fixedNow)).toEqual({ text: '—', tone: 'muted' });
  });

  test('剩余充足显示小时分钟且无徽标', /* 当前测试验证剩余超过一小时的展示格式。 */ () => {
    // deadline 是剩余 2 小时 5 分的截止时刻。
    const deadline = new Date(fixedNow + (2 * 60 + 5) * 60_000).toISOString();
    expect(slaCountdownView(deadline, 240, fixedNow)).toEqual({ text: '2 小时 5 分', tone: 'normal' });
  });

  test('剩余不足一小时显示分秒', /* 当前测试验证剩余不足一小时的展示格式。 */ () => {
    // deadline 是 20 分钟窗口内剩余 5 分 30 秒的截止时刻，比例高于 20% 不预警。
    const deadline = new Date(fixedNow + 5 * 60_000 + 30_000).toISOString();
    expect(slaCountdownView(deadline, 20, fixedNow)).toEqual({ text: '5 分 30 秒', tone: 'normal' });
  });

  test('剩余不足 20% 显示将超时预警', /* 当前测试验证剩余低于窗口 20% 的黄色预警。 */ () => {
    // deadline 是 100 分钟窗口内剩余 10 分钟的截止时刻，比例正好低于 20%。
    const deadline = new Date(fixedNow + 10 * 60_000).toISOString();
    expect(slaCountdownView(deadline, 100, fixedNow)).toEqual({ text: '10 分 0 秒', tone: 'warning', badge: '将超时' });
  });

  test('剩余等于 20% 不触发预警', /* 当前测试验证 20% 边界本身不判将超时。 */ () => {
    // deadline 是 100 分钟窗口内剩余 20 分钟的截止时刻，比例正好等于 20%。
    const deadline = new Date(fixedNow + 20 * 60_000).toISOString();
    expect(slaCountdownView(deadline, 100, fixedNow)).toEqual({ text: '20 分 0 秒', tone: 'normal' });
  });

  test('已超时显示红色徽标与超时时长', /* 当前测试验证超过截止时刻后的红色超时展示。 */ () => {
    // deadline 是已超时 35 分钟的截止时刻。
    const deadline = new Date(fixedNow - 35 * 60_000).toISOString();
    expect(slaCountdownView(deadline, 60, fixedNow)).toEqual({ text: '已超时 35 分 0 秒', tone: 'danger', badge: '已超时' });
  });

  test('缺 sla_minutes 时不启用 20% 预警阈值', /* 当前测试验证没有窗口总长时剩余按普通档位展示。 */ () => {
    // deadline 是剩余 1 分钟的截止时刻，但窗口总长缺失。
    const deadline = new Date(fixedNow + 60_000).toISOString();
    expect(slaCountdownView(deadline, undefined, fixedNow)).toEqual({ text: '1 分 0 秒', tone: 'normal' });
  });
});

describe('formatSlaDuration', /* 当前测试组验证倒计时时长的两种文案格式。 */ () => {
  test('满一小时用小时分钟', /* 当前测试验证超过或等于一小时的格式化。 */ () => {
    expect(formatSlaDuration(3_600_000)).toBe('1 小时 0 分');
    expect(formatSlaDuration((2 * 60 + 5) * 60_000)).toBe('2 小时 5 分');
  });

  test('不足一小时用分秒', /* 当前测试验证不足一小时的格式化。 */ () => {
    expect(formatSlaDuration(5 * 60_000 + 30_000)).toBe('5 分 30 秒');
    expect(formatSlaDuration(0)).toBe('0 分 0 秒');
  });
});
