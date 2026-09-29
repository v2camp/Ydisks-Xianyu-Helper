// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { useState } from 'react';
import type { SystemSettings } from '../api';
import { DeliveryGuardCard } from './DeliveryGuardCard';

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

// GuardHarnessProps 描述发货内容门禁卡受控测试容器的输入。
interface GuardHarnessProps {
  /** initial 是渲染前的系统配置草稿初值。 */
  initial: SystemSettings;
}

// GuardHarness 是门禁配置卡的受控测试容器，保存草稿并展示门禁 JSON 序列化结果。
function GuardHarness({ initial }: GuardHarnessProps) {
  // settings 是受控的系统配置草稿状态。
  const [settings, setSettings] = useState<SystemSettings>(initial);
  return (
    <div>
      <DeliveryGuardCard settings={settings} onChange={/* patch 合并进测试草稿状态。 */ patch => setSettings(/* prev 是合并补丁前的草稿，返回合并后的新草稿。 */ prev => ({ ...prev, ...patch }))} />
      <pre data-testid="draft">{typeof settings.delivery_content_guard === 'string' ? settings.delivery_content_guard : ''}</pre>
    </div>
  );
}

// parseDraft 读取测试容器底部展示的门禁 JSON 草稿对象。
function parseDraft(): Record<string, unknown> {
  return JSON.parse(screen.getByTestId('draft').textContent || '{}');
}

describe('DeliveryGuardCard', /* 当前测试组验证门禁开关往返、非法 JSON 回退与额外词保存。 */ () => {
  test('缺失配置展示默认开启并可关闭开关后序列化回写', /* 当前测试验证开关往返与 JSON 序列化。 */ () => {
    render(<GuardHarness initial={{}} />);
    // enabledBox 是违禁词门禁开关，缺省展示默认开启。
    const enabledBox = screen.getByLabelText('发货内容违禁词门禁') as HTMLInputElement;
    // linkBox 是链接健康检查开关，缺省展示默认开启。
    const linkBox = screen.getByLabelText('发货前链接健康检查') as HTMLInputElement;
    expect(enabledBox.checked).toBe(true);
    expect(linkBox.checked).toBe(true);
    fireEvent.click(enabledBox);
    expect(parseDraft()).toEqual({ enabled: false, link_check: true, extra_block_words: '' });
    fireEvent.click(linkBox);
    expect(parseDraft()).toEqual({ enabled: false, link_check: false, extra_block_words: '' });
    fireEvent.click(enabledBox);
    expect(parseDraft()).toEqual({ enabled: true, link_check: false, extra_block_words: '' });
  });

  test('已有 JSON 解析展示且切换开关保留额外违禁词', /* 当前测试验证既有配置往返与字段保留。 */ () => {
    render(<GuardHarness initial={{ delivery_content_guard: JSON.stringify({ enabled: false, link_check: true, extra_block_words: '加微信, 扫码|QQ' }) }} />);
    // enabledBox 是违禁词门禁开关，应展示已关闭。
    const enabledBox = screen.getByLabelText('发货内容违禁词门禁') as HTMLInputElement;
    expect(enabledBox.checked).toBe(false);
    fireEvent.click(enabledBox);
    expect(parseDraft()).toEqual({ enabled: true, link_check: true, extra_block_words: '加微信, 扫码|QQ' });
  });

  test('非法 JSON 回退默认并提示，修改后写回合法 JSON', /* 当前测试验证非法 JSON 回退与修复路径。 */ () => {
    render(<GuardHarness initial={{ delivery_content_guard: '{not json' }} />);
    expect(screen.getByText(/配置 JSON 非法/)).toBeTruthy();
    // enabledBox 是违禁词门禁开关，非法 JSON 时按默认开启展示。
    const enabledBox = screen.getByLabelText('发货内容违禁词门禁') as HTMLInputElement;
    expect(enabledBox.checked).toBe(true);
    fireEvent.click(enabledBox);
    // draft 是修复后的门禁 JSON，应为合法对象。
    const draft = parseDraft();
    expect(draft).toEqual({ enabled: false, link_check: true, extra_block_words: '' });
    expect(screen.queryByText(/配置 JSON 非法/)).toBeNull();
  });

  test('额外违禁词输入保存逗号竖线原文', /* 当前测试验证额外词保存。 */ () => {
    render(<GuardHarness initial={{}} />);
    // wordsInput 是额外违禁词多行输入框。
    const wordsInput = screen.getByLabelText('额外违禁词') as HTMLTextAreaElement;
    fireEvent.change(wordsInput, { target: { value: '加微信, 扫码进群|站外引流' } });
    expect(parseDraft()).toEqual({ enabled: true, link_check: true, extra_block_words: '加微信, 扫码进群|站外引流' });
  });

  test('渲染快照包含门禁标题、开关说明与占位示例', /* 当前测试验证渲染文案与结构。 */ () => {
    // container 是默认门禁卡渲染根节点。
    const { container } = render(<GuardHarness initial={{}} />);
    expect(screen.getByText('发货内容门禁')).toBeTruthy();
    expect(screen.getByText(/命中联系方式\/引流链接\/违禁词时拒发并转人工处理/)).toBeTruthy();
    expect(screen.getByText(/4xx 反爬不拦截/)).toBeTruthy();
    expect(screen.getByPlaceholderText(/加微信/)).toBeTruthy();
    expect(container).toMatchSnapshot();
  });
});
