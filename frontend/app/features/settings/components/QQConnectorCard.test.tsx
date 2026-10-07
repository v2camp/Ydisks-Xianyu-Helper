// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { useState } from 'react';
import type { SystemSettings } from '../api';
import { QQConnectorCard, QQ_OPEN_PLATFORM_URL } from './QQConnectorCard';

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

// ConnectorHarnessProps 描述 QQ 连接器卡受控测试容器的输入。
interface ConnectorHarnessProps {
  /** initial 是渲染前的系统配置草稿初值。 */
  initial: SystemSettings;
}

// ConnectorHarness 是 QQ 连接器卡的受控测试容器，保存草稿并展示最新补丁。
function ConnectorHarness({ initial }: ConnectorHarnessProps) {
  // settings 是受控的系统配置草稿状态。
  const [settings, setSettings] = useState<SystemSettings>(initial);
  return (
    <div>
      <QQConnectorCard settings={settings} onChange={/* patch 合并进测试草稿状态。 */ patch => setSettings(/* prev 是合并补丁前的草稿，返回合并后的新草稿。 */ prev => ({ ...prev, ...patch }))} />
      <pre data-testid="draft">{JSON.stringify({ app_id: settings['qqbot.app_id'] ?? null, app_secret: settings['qqbot.app_secret'] ?? null })}</pre>
    </div>
  );
}

// readDraft 读取测试容器底部展示的凭据草稿对象。
function readDraft(): Record<string, string | null> {
  return JSON.parse(screen.getByTestId('draft').textContent || '{}');
}

describe('QQConnectorCard', /* 当前测试组验证开放平台引导、凭据输入往返与配置状态展示。 */ () => {
  test('未配置时展示未配置状态并引导前往 QQ 开放平台', /* 当前测试验证空配置的引导文案与入口链接。 */ () => {
    render(<ConnectorHarness initial={{}} />);
    expect(screen.getByTestId('qq-connector-status').textContent).toBe('未配置');
    // link 是前往 QQ 开放平台的外部链接。
    const link = screen.getByText('前往 QQ 开放平台') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe(QQ_OPEN_PLATFORM_URL);
    expect(link.getAttribute('target')).toBe('_blank');
    expect(screen.getByText(/用手机 QQ 扫码登录/)).toBeTruthy();
    expect(screen.getByText(/创建机器人/)).toBeTruthy();
  });

  test('输入 AppID 与 AppSecret 写回 qqbot 前缀设置键', /* 当前测试验证凭据输入按敏感键分流写回草稿。 */ () => {
    render(<ConnectorHarness initial={{}} />);
    // idInput 是机器人 AppID 输入框。
    const idInput = screen.getByLabelText('QQ 机器人 AppID') as HTMLInputElement;
    // secretInput 是机器人 AppSecret 输入框。
    const secretInput = screen.getByLabelText('QQ 机器人 AppSecret') as HTMLInputElement;
    fireEvent.change(idInput, { target: { value: '102012345' } });
    fireEvent.change(secretInput, { target: { value: 'bot-secret-plain' } });
    expect(readDraft()).toEqual({ app_id: '102012345', app_secret: 'bot-secret-plain' });
    expect(screen.getByTestId('qq-connector-status').textContent).toBe('凭据已就绪');
  });

  test('服务端已配置 AppSecret 时只回显 AppID 并提示已配置', /* 当前测试验证敏感值不回显、AppID 明文回显的往返行为。 */ () => {
    render(<ConnectorHarness initial={{ 'qqbot.app_id': '102012345', 'qqbot.app_secret_configured': true }} />);
    // idInput 是机器人 AppID 输入框，应回显服务端保存的明文。
    const idInput = screen.getByLabelText('QQ 机器人 AppID') as HTMLInputElement;
    // secretInput 是机器人 AppSecret 输入框，服务端不回显明文。
    const secretInput = screen.getByLabelText('QQ 机器人 AppSecret') as HTMLInputElement;
    expect(idInput.value).toBe('102012345');
    expect(secretInput.value).toBe('');
    expect(secretInput.getAttribute('placeholder')).toBe('已配置，如需替换请输入新 AppSecret');
    expect(screen.getByTestId('qq-connector-status').textContent).toBe('凭据已就绪');
    expect(screen.getByText(/服务端已保存 AppSecret 且仅本地加密存储/)).toBeTruthy();
  });

  test('AppSecret 默认掩码且可切换明文显示', /* 当前测试验证敏感输入的默认掩码与明文切换。 */ () => {
    render(<ConnectorHarness initial={{}} />);
    // secretInput 是机器人 AppSecret 输入框，默认应为掩码类型。
    const secretInput = screen.getByLabelText('QQ 机器人 AppSecret') as HTMLInputElement;
    expect(secretInput.getAttribute('type')).toBe('password');
    fireEvent.click(screen.getByTitle('显示 AppSecret'));
    expect((screen.getByLabelText('QQ 机器人 AppSecret') as HTMLInputElement).getAttribute('type')).toBe('text');
    fireEvent.click(screen.getByTitle('隐藏 AppSecret'));
    expect((screen.getByLabelText('QQ 机器人 AppSecret') as HTMLInputElement).getAttribute('type')).toBe('password');
  });

  test('仅有 AppID 时仍未就绪并展示主动推送额度说明', /* 当前测试验证半配置状态与官方通道限制提示。 */ () => {
    render(<ConnectorHarness initial={{ 'qqbot.app_id': '102012345' }} />);
    expect(screen.getByTestId('qq-connector-status').textContent).toBe('未配置');
    expect(screen.getByText(/每月仅 4 条主动消息/)).toBeTruthy();
    expect(screen.getByText(/单聊 60 分钟、群聊 5 分钟/)).toBeTruthy();
  });
});
