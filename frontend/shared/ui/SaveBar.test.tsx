// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { SaveBar } from './SaveBar';

afterEach(/* 每个用例结束后卸载渲染树并还原真实定时器，避免跨用例互相干扰。 */ () => {
  cleanup();
  vi.useRealTimers();
});

describe('SaveBar', /* 用例组验证保存条四态、页面粘性与成功回落。 */ () => {
  test('空闲态展示作用范围并在点击主按钮时提交', /* 验证草稿型配置由显式点击触发保存。 */ () => {
    // onSave 是主保存按钮的点击回调替身。
    const onSave = vi.fn();
    render(<SaveBar state="idle" onSave={onSave} label="保存 AI 配置" scopeLabel="保存客服 Agent 配置" />);
    expect(screen.getByText('保存客服 Agent 配置')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存 AI 配置' }));
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  test('空闲态缺省提示存在未保存修改', /* 验证调用方不传作用范围时的兜底文案。 */ () => {
    render(<SaveBar state="idle" onSave={vi.fn()} label="保存 AI 配置" />);
    expect(screen.getByText('有未保存的修改')).toBeTruthy();
  });

  test('保存中禁用按钮并显示旋转图标', /* 验证提交进行中不能重复触发保存。 */ () => {
    // onSave 用于确认禁用态点击不会产生第二次提交。
    const onSave = vi.fn();
    render(<SaveBar state="saving" onSave={onSave} label="保存 AI 配置" />);
    // button 是保存中的按钮，应被禁用且文案切换为保存中。
    const button = screen.getByRole('button', { name: '保存中...' }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    fireEvent.click(button);
    expect(onSave).not.toHaveBeenCalled();
  });

  test('失败态展示错误文案并提供重试入口', /* 验证保存失败不再依赖弹窗提示，重试按钮直接重新提交。 */ () => {
    // onSave 是失败态重试按钮的点击回调替身。
    const onSave = vi.fn();
    render(<SaveBar state="error" onSave={onSave} label="保存 AI 配置" errorMessage="网络异常，配置未保存" />);
    expect(screen.getByRole('alert').className).toContain('bg-danger-500');
    expect(screen.getByText('网络异常，配置未保存')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  test('成功态在 2 秒后回调 onSettled 以复位保存态', /* 验证绿色提示到时自动回落，页面无需自行管理定时器。 */ () => {
    vi.useFakeTimers();
    // onSettled 是成功提示回落时通知页面复位保存态的回调替身。
    const onSettled = vi.fn();
    render(<SaveBar state="success" onSave={vi.fn()} label="保存 AI 配置" successMessage="AI 配置已保存" onSettled={onSettled} />);
    expect(screen.getByText('AI 配置已保存')).toBeTruthy();
    expect(screen.getByRole('status').className).toContain('bg-success-500');
    act(/* 推进到回落阈值前 1 毫秒，成功提示仍应保持。 */ () => {
      vi.advanceTimersByTime(1999);
    });
    expect(onSettled).not.toHaveBeenCalled();
    act(/* 越过 2 秒阈值，应触发一次回落回调。 */ () => {
      vi.advanceTimersByTime(1);
    });
    expect(onSettled).toHaveBeenCalledTimes(1);
  });

  test('页级粘性仅在 sticky 开启时生效', /* 验证区块级保存条留在文档流内，不遮挡下方内容。 */ () => {
    render(<SaveBar state="idle" onSave={vi.fn()} label="保存 AI 配置" sticky />);
    expect(screen.getByRole('status').className).toContain('sticky');
    cleanup();
    render(<SaveBar state="idle" onSave={vi.fn()} label="保存 AI 配置" />);
    expect(screen.getByRole('status').className).not.toContain('sticky');
  });
});
