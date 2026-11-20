// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { NotificationForm } from '../types';
import { NotificationChannelModal } from './NotificationChannelModal';

afterEach(/* cleanupDom 隔离每个用例创建的弹窗 DOM。 */ () => cleanup());

// qqForm 是 qq 渠道弹窗使用的最小表单，凭据由当前用户的集成配置统一提供。
const qqForm: NotificationForm = { name: 'QQ 通知', type: 'qq', enabled: true, config: { user_openid: 'user-openid' }, event_types: [] };
// barkForm 是 bark 渠道弹窗使用的最小表单，device_key 是按已保存凭据留空的字段。
const barkForm: NotificationForm = { name: 'Bark', type: 'bark', enabled: true, config: { server_url: 'https://api.day.app' }, event_types: [] };
// dingtalkForm 是钉钉渠道弹窗使用的最小表单，两个必填字段都是凭据。
const dingtalkForm: NotificationForm = { name: '钉钉', type: 'dingtalk', enabled: true, config: {}, event_types: [] };
// emailForm 是邮件渠道弹窗使用的最小表单，只包含收件地址。
const emailForm: NotificationForm = { name: '邮件', type: 'email', enabled: true, config: { to_email: 'receiver@example.com' }, event_types: [] };

// renderModal 渲染渠道弹窗并返回表单更新与重置替身，供断言用户操作写回的状态。
const renderModal = (form: NotificationForm, savedCredentialKeys: string[] = []) => {
  // setForm 是记录弹窗写回表单的替身；React 未重渲染时测试直接调用其更新函数。
  const setForm = vi.fn();
  // resetCredentialState 是记录类型切换重置调用的替身。
  const resetCredentialState = vi.fn();
  render(
    <NotificationChannelModal
      showModal
      editing={null}
      form={form}
      setForm={setForm}
      saving={false}
      savedCredentialKeys={savedCredentialKeys}
      resetCredentialState={resetCredentialState}
      onClose={vi.fn()}
      onSave={vi.fn()}
    />,
  );
  return { setForm, resetCredentialState };
};

// findField 按配置字段键读取弹窗中的输入框。
const findField = (key: string): HTMLInputElement | null => document.querySelector(`input[data-field="${key}"]`);

// applyUpdater 取出替身收到的第一个表单更新函数并应用，得到弹窗期望写回的表单。
const applyUpdater = (setForm: ReturnType<typeof vi.fn>, current: NotificationForm, callIndex = 0): NotificationForm => (setForm.mock.calls[callIndex][0] as (form: NotificationForm) => NotificationForm)(current);

describe('NotificationChannelModal 的凭据字段', /* 当前测试组验证渠道级凭据、已保存凭据与用户级集成配置的表单形态。 */ () => {
  test('qq 渠道不再展示 AppID 与 AppSecret，只保留目标 openid', /* qqCase 验证凭据字段已迁出渠道弹窗。 */ () => {
    renderModal(qqForm, ['app_id', 'app_secret']);
    expect(findField('app_id')).toBeNull();
    expect(findField('app_secret')).toBeNull();
    expect(findField('user_openid')?.value).toBe('user-openid');
    // qq 不再声明渠道级凭据，历史凭据键不会在弹窗内触发沿用或清除提示。
    expect(screen.queryByText('当前渠道仍保存着独立凭据，继续保存将沿用它们。')).toBeNull();
    // qq 渠道没有必填字段，不再显示任何必填星号。
    expect(screen.queryAllByText('*')).toHaveLength(0);
  });

  test('勾选改用用户级集成凭据后只记录清除意图', /* clearCase 验证清除意图的写回。 */ () => {
    // setForm 是记录表单写回的替身。
    const { setForm } = renderModal(dingtalkForm, ['secret', 'webhook_url']);
    expect(dingtalkForm.credentialAction).toBeUndefined();
    fireEvent.click(screen.getByRole('checkbox'));
    expect(applyUpdater(setForm, dingtalkForm).credentialAction).toBe('clear');
  });

  test('已保存凭据的字段留空沿用且不再必填', /* optionalCase 验证已保存凭据的提示。 */ () => {
    renderModal(barkForm, ['device_key']);
    expect(findField('server_url')?.value).toBe('https://api.day.app');
    expect(findField('server_url')?.placeholder).toBe('https://api.day.app');
    expect(findField('device_key')?.placeholder).toBe('已保存，留空则保持原值');
    // 只有 server_url 还带必填星号，device_key 已由服务端保存值兜底。
    expect(screen.getAllByText('*')).toHaveLength(1);
  });

  test('钉钉的加签密钥按密码框渲染且已保存时可留空', /* dingtalkCase 验证密码控件与全凭据渠道的可选提示。 */ () => {
    renderModal(dingtalkForm, ['secret', 'webhook_url']);
    expect(findField('webhook_url')?.placeholder).toBe('已保存，留空则保持原值');
    expect(findField('secret')?.type).toBe('password');
    expect(findField('secret')?.placeholder).toBe('已保存，留空则保持原值');
    expect(screen.queryAllByText('*')).toHaveLength(0);
  });

  test('邮件渠道只设置收件地址并提示使用用户级 SMTP', /* emailCase 验证渠道级 SMTP 字段已删除。 */ () => {
    renderModal(emailForm);
    expect(findField('to_email')?.value).toBe('receiver@example.com');
    expect(findField('smtp_server')).toBeNull();
    expect(findField('smtp_password')).toBeNull();
    expect(screen.getByText('邮件渠道使用「当前用户的集成配置 → 邮件发送（SMTP）」中的发件配置，本弹窗只设置收件邮箱。')).toBeTruthy();
  });

  test('切换渠道类型时清空上一类型的凭据状态', /* typeSwitchCase 验证类型切换触发的重置。 */ () => {
    // resetCredentialState 是类型切换场景记录重置调用的替身。
    const { setForm, resetCredentialState } = renderModal(qqForm, ['app_id', 'app_secret']);
    fireEvent.click(document.querySelector('button[data-type="bark"]') as HTMLButtonElement);
    expect(resetCredentialState).toHaveBeenCalledTimes(1);
    expect(applyUpdater(setForm, qqForm)).toMatchObject({ type: 'bark', config: {} });
  });

  test('重复点击当前渠道类型不重置凭据状态', /* sameTypeCase 验证误触已选类型不会丢弃已填配置。 */ () => {
    // resetCredentialState 是重复点击场景记录重置调用的替身。
    const { resetCredentialState } = renderModal(barkForm, ['device_key']);
    fireEvent.click(document.querySelector('button[data-type="bark"]') as HTMLButtonElement);
    expect(resetCredentialState).not.toHaveBeenCalled();
  });
});
