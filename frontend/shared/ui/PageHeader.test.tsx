// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { Bot } from 'lucide-react';
import { afterEach, describe, expect, test } from 'vitest';
import { PageHeader } from './PageHeader';

afterEach(/* 每个用例结束后卸载渲染树，避免跨用例的 DOM 残留。 */ () => cleanup());

describe('PageHeader', /* 用例组验证页头标题、副标题与右侧动作槽的渲染。 */ () => {
  test('渲染标题、副标题与右侧动作槽', /* 验证页头把页面身份信息与页级操作放在同一行。 */ () => {
    render(
      <PageHeader
        icon={Bot}
        title="Agent 中心"
        subtitle="三类 Agent 各自的入口与当前生效状态"
        actions={<button type="button">刷新</button>}
      />,
    );
    expect(screen.getByRole('heading', { name: 'Agent 中心' })).toBeTruthy();
    expect(screen.getByText('三类 Agent 各自的入口与当前生效状态')).toBeTruthy();
    expect(screen.getByRole('button', { name: '刷新' })).toBeTruthy();
  });

  test('省略副标题与动作时不渲染空占位', /* 验证可选槽位缺省时不会留下无意义的空白节点。 */ () => {
    render(<PageHeader icon={Bot} title="AI 设置" />);
    expect(screen.getByRole('heading', { name: 'AI 设置' })).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});
