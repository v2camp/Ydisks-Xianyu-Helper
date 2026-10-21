// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { PageShell } from './PageShell';

afterEach(/* 每个用例结束后卸载渲染树，避免跨用例的 DOM 残留。 */ () => cleanup());

describe('PageShell', /* 用例组验证页面外壳的容器约束与子节点渲染。 */ () => {
  test('包裹子节点并提供统一的宽度、间距与底部避让', /* 验证页面外壳替掉各页面重复书写的容器类名。 */ () => {
    render(
      <PageShell className="extra-page-class">
        <p>页面主体内容</p>
      </PageShell>,
    );
    // container 是外壳渲染出的根容器，用于断言统一类名与调用方追加类名同时生效。
    const container = screen.getByText('页面主体内容').parentElement as HTMLElement;
    expect(container.className).toContain('mx-auto');
    expect(container.className).toContain('max-w-6xl');
    expect(container.className).toContain('space-y-8');
    expect(container.className).toContain('animate-fade-in');
    expect(container.className).toContain('pb-24');
    expect(container.className).toContain('extra-page-class');
    expect(screen.getByText('页面主体内容')).toBeTruthy();
  });
});
