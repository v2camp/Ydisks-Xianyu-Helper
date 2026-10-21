// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { StatusBadge } from './StatusBadge';

afterEach(/* 每个用例结束后卸载渲染树，避免跨用例的 DOM 残留。 */ () => cleanup());

describe('StatusBadge', /* 用例组验证徽章四种语义色与稳定测试标识。 */ () => {
  test('已就绪使用成功色', /* 验证配置齐备时徽章以成功色呈现。 */ () => {
    render(<StatusBadge tone="ready" testId="badge-ready">已配置</StatusBadge>);
    // badge 是通过测试标识定位的徽章节点，用于同时断言文案与底色。
    const badge = screen.getByTestId('badge-ready');
    expect(badge.textContent).toBe('已配置');
    expect(badge.className).toContain('bg-success-100');
  });

  test('未配置使用中性色', /* 验证未配置不误用告警色，避免制造紧张感。 */ () => {
    render(<StatusBadge tone="missing">未配置</StatusBadge>);
    expect(screen.getByText('未配置').className).toContain('bg-gray-100');
  });

  test('读取中使用中性色', /* 验证数据未到达时的状态与未配置在视觉上同属中性。 */ () => {
    render(<StatusBadge tone="loading">读取中…</StatusBadge>);
    expect(screen.getByText('读取中…').className).toContain('bg-gray-100');
  });

  test('回落内置使用琥珀弱化提示', /* 验证合法默认与彻底未配置在视觉上可区分。 */ () => {
    render(<StatusBadge tone="warning">未配置 · 回落内置</StatusBadge>);
    expect(screen.getByText('未配置 · 回落内置').className).toContain('bg-warning-100');
  });
});
