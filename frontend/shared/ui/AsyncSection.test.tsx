// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { AsyncSection } from './AsyncSection';

afterEach(/* 每个用例结束后卸载渲染树，避免跨用例的 DOM 残留。 */ () => cleanup());

describe('AsyncSection', /* 用例组验证加载、错误与内容三态的互斥展示。 */ () => {
  test('加载态展示读取提示且不渲染内容', /* 验证数据未到达时不先闪出空表单。 */ () => {
    render(
      <AsyncSection loading>
        <p>依赖配置内容</p>
      </AsyncSection>,
    );
    expect(screen.getByRole('status').textContent).toContain('加载中');
    expect(screen.queryByText('依赖配置内容')).toBeNull();
  });

  test('错误态展示文案并支持重试', /* 验证读取失败对用户可见，且重试入口接到调用方回调。 */ () => {
    // onRetry 是错误态重试按钮的回调替身。
    const onRetry = vi.fn();
    render(
      <AsyncSection error="依赖状态读取失败" onRetry={onRetry} retryLabel="重新读取">
        <p>依赖配置内容</p>
      </AsyncSection>,
    );
    expect(screen.getByRole('alert').textContent).toContain('依赖状态读取失败');
    expect(screen.queryByText('依赖配置内容')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '重新读取' }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  test('无错误且不在加载态时渲染内容', /* 验证就绪态直接透传区块内容。 */ () => {
    render(
      <AsyncSection className="content-wrapper">
        <p>依赖配置内容</p>
      </AsyncSection>,
    );
    expect(screen.getByText('依赖配置内容')).toBeTruthy();
    // wrapper 是内容态的容器，应保留调用方传入的布局类名。
    const wrapper = screen.getByText('依赖配置内容').parentElement as HTMLElement;
    expect(wrapper.className).toContain('content-wrapper');
  });
});
