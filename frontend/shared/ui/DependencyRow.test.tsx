// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { DependencyRow } from './DependencyRow';

afterEach(/* 每个用例结束后卸载渲染树，避免跨用例的 DOM 残留。 */ () => cleanup());

describe('DependencyRow', /* 用例组验证依赖四态的徽章文案、补充说明与跳转入口。 */ () => {
  test('读取中显示读取中而不是未配置', /* 验证数据未到达时不会误报未配置，消除状态闪烁。 */ () => {
    render(<DependencyRow label="QQ 机器人凭据" state="loading" />);
    expect(screen.getByText('读取中…')).toBeTruthy();
    expect(screen.queryByText('未配置')).toBeNull();
  });

  test('已配置显示就绪徽章并展示补充说明', /* 验证依赖就绪时同时给出名称与说明，便于对照。 */ () => {
    render(<DependencyRow label="AI 模型凭据" state="ready" detail="当前使用租户级 API Key" />);
    expect(screen.getByText('AI 模型凭据')).toBeTruthy();
    expect(screen.getByText('当前使用租户级 API Key')).toBeTruthy();
    expect(screen.getByText('已配置')).toBeTruthy();
  });

  test('回落内置与彻底未配置使用不同文案', /* 验证合法默认不被当成缺配置处理。 */ () => {
    render(<DependencyRow label="MCP 服务地址" state="fallback" />);
    expect(screen.getByText('未配置 · 回落内置')).toBeTruthy();
    cleanup();
    render(<DependencyRow label="MCP 服务地址" state="missing" />);
    expect(screen.getByText('未配置')).toBeTruthy();
  });

  test('提供文案与回调时渲染跳转按钮并触发回调', /* 验证依赖只跳转不就地编辑。 */ () => {
    // onNavigate 是依赖跳转按钮的点击回调替身。
    const onNavigate = vi.fn();
    render(<DependencyRow label="QQ 机器人凭据" state="missing" linkLabel="去连接器页配置" onNavigate={onNavigate} />);
    // link 是依赖行右侧的跳转按钮，文案带箭头提示会离开当前页。
    const link = screen.getByRole('button', { name: '去连接器页配置 →' });
    fireEvent.click(link);
    expect(onNavigate).toHaveBeenCalledTimes(1);
  });

  test('缺少回调时不渲染跳转按钮', /* 验证没有跳转目标时不留下无效入口。 */ () => {
    render(<DependencyRow label="QQ 机器人凭据" state="missing" linkLabel="去连接器页配置" testId="dependency-row" />);
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByTestId('dependency-row').className).toContain('border-gray-200');
  });
});
