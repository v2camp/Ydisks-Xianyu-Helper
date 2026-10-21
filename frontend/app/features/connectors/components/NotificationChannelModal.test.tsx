// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { NotificationForm } from '../types';
import { NotificationChannelModal } from './NotificationChannelModal';

afterEach(/* cleanupDom 隔离每个用例创建的弹窗 DOM。 */ () => cleanup());

// qqForm 是 qq 渠道弹窗使用的最小表单，凭据字段由各场景按需填入。
const qqForm: NotificationForm = { name: 'QQ 通知', type: 'qq', enabled: true, config: { user_openid: 'user-openid' }, event_types: [] };

// ModalCredentialProps 是各场景下 qq 渠道弹窗的凭据入参。
interface ModalCredentialProps {
  // connectorReady 表示系统级连接器凭据是否已就绪。
  connectorReady?: boolean;
  // hasChannelCredentials 表示当前渠道是否仍保存独立凭据。
  hasChannelCredentials?: boolean;
}

// renderModal 渲染 qq 渠道弹窗并返回表单更新替身，供断言用户操作写回的配置。
const renderModal = (form: NotificationForm, props: ModalCredentialProps) => {
  // setForm 是记录弹窗写回表单的替身；React 未重渲染时测试直接调用其更新函数。
  const setForm = vi.fn();
  render(
    <NotificationChannelModal
      showModal
      editing={null}
      form={form}
      setForm={setForm}
      smtp={{}}
      showChannelSmtpPassword={false}
      setShowChannelSmtpPassword={vi.fn()}
      saving={false}
      connectorReady={props.connectorReady}
      hasChannelCredentials={props.hasChannelCredentials}
      onClose={vi.fn()}
      onSave={vi.fn()}
    />,
  );
  return setForm;
};

// findField 按配置字段键读取弹窗中的输入框。
const findField = (key: string): HTMLInputElement | null => document.querySelector(`input[data-field="${key}"]`);

describe('NotificationChannelModal 的 QQ 凭据来源', /* 当前测试组验证连接器就绪与渠道独立凭据对表单形态的影响。 */ () => {
  test('连接器就绪时隐藏凭据字段并说明来源', /* readyCase 验证字段隐藏与勾选后的删除标记。 */ () => {
    // setForm 是连接器就绪场景下记录表单写回的替身。
    const setForm = renderModal(qqForm, { connectorReady: true, hasChannelCredentials: true });
    expect(findField('app_id')).toBeNull();
    expect(findField('app_secret')).toBeNull();
    expect(screen.getByText('AppID / AppSecret 使用「连接器 → QQ 机器人」的系统级凭据。')).toBeTruthy();
    expect(screen.getByText('当前渠道仍保存着独立凭据，继续保存将沿用它们。')).toBeTruthy();
    // 未勾选时表单里不出现 app 键，保存即沿用渠道现有凭据。
    expect(qqForm.config).not.toHaveProperty('app_id');

    fireEvent.click(screen.getByRole('checkbox'));
    // updater 是勾选后弹窗交给 setForm 的表单更新函数。
    const updater = setForm.mock.calls[0][0] as (current: NotificationForm) => NotificationForm;
    expect(updater(qqForm).config).toEqual({ user_openid: 'user-openid', app_id: '', app_secret: '' });
  });

  test('连接器未就绪但渠道有独立凭据时字段保留且不再必填', /* optionalCase 验证留空沿用已保存凭据的提示。 */ () => {
    renderModal(qqForm, { connectorReady: false, hasChannelCredentials: true });
    expect(findField('app_id')?.placeholder).toBe('已保存，留空则保持原值');
    expect(findField('app_secret')?.placeholder).toBe('已保存，留空则保持原值');
    expect(screen.queryByText('当前渠道仍保存着独立凭据，继续保存将沿用它们。')).toBeNull();
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  test('连接器未就绪且渠道无凭据时维持原有必填提示', /* requiredCase 验证默认场景未被新逻辑放宽。 */ () => {
    renderModal(qqForm, { connectorReady: false, hasChannelCredentials: false });
    expect(findField('app_id')?.placeholder).toBe('你的 QQ 机器人 AppID');
    expect(findField('app_secret')?.placeholder).toBe('QQ 机器人 AppSecret');
    expect(screen.getAllByText('*')).toHaveLength(2);
  });
});
