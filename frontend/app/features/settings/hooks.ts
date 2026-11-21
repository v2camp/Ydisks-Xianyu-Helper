import type React from 'react';
import { useCallback,useEffect,useRef,useState } from 'react';
import type { SystemSettings } from './api';
import { getSystemSettings,updateLoginCredentials,updateSystemSettings,verifySession } from './api';
import { buildPersistableSettings,createCredentials,createCredentialsMessage,isCurrentSettingsRequest,isSettingsAbortError,settingsErrorMessage,validateCredentials } from './state';
import type { CredentialsForm,CredentialsMessage,SettingsFeatureState,SettingsRequestStatus,SettingsScope } from './types';
import { appDialog } from '../../../shared/ui/dialog';

/** SCOPE_SAVED_LABELS 是各保存范围对应的成功提示文案，避免新增范围时回落到错误文案。 */
const SCOPE_SAVED_LABELS: Readonly<Record<SettingsScope, string>> = {
  system: '系统配置已保存',
  ai: 'AI 配置已保存',
  ops: '运营 Agent 配置已保存',
};

/** 保存系统配置的可选行为开关。 */
export interface SettingsSaveOptions {
  /** silentSuccess 为真时跳过成功弹窗，由调用方用内联提示自反馈（如 AI 设置页的保存条）；缺省保持弹窗反馈。 */
  silentSuccess?: boolean;
}

/** 系统配置保存函数：返回值表示是否保存成功，便于调用方用内联提示替代弹窗。 */
export interface SettingsSaveFunction {
  /** 不传参数时保持既有行为：成功后弹窗提示，供系统设置页直接绑定到按钮。 */
  (): Promise<boolean>;
  /** 传入 silentSuccess 时由调用方自管成功反馈。 */
  (options: SettingsSaveOptions): Promise<boolean>;
}

/** Settings feature 的 Hook 返回值。 */
export type UseSettingsResult = SettingsFeatureState & {
  /** 重新加载实例级系统配置。 */
  loadSettings: () => void;
  /** 保存系统配置；返回是否保存成功，供调用方选择弹窗之外的反馈方式。 */
  handleSave: SettingsSaveFunction;
  /** 保存登录凭据。 */
  handleCredentialsSave: (event: React.FormEvent) => Promise<void>;
  /** 更新配置草稿。 */
  setSettings: React.Dispatch<React.SetStateAction<SystemSettings | null>>;
  /** 更新远程秘钥显示状态。 */
  setShowCaptchaSecret: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新当前密码显示状态。 */
  setShowCurrentPassword: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新新密码显示状态。 */
  setShowNewPassword: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新凭据表单。 */
  setCredentials: React.Dispatch<React.SetStateAction<CredentialsForm>>;
  /** 更新凭据提示。 */
  setCredentialsMessage: React.Dispatch<React.SetStateAction<CredentialsMessage>>;
};

/** 管理实例级设置草稿与登录凭据的请求和表单状态；scope 决定保存白名单。 */
export const useSettings = (scope: SettingsScope): UseSettingsResult => {
  // settings 保存当前系统配置草稿。
  const [settings, setSettings] = useState<SystemSettings | null>(null);
  // loading 表示系统配置是否正在加载。
  const [loading, setLoading] = useState(false);
  // loadError 保存系统配置加载失败信息。
  const [loadError, setLoadError] = useState('');
  // saving 表示系统配置是否正在保存。
  const [saving, setSaving] = useState(false);
  // saveError 保存系统配置保存失败信息。
  const [saveError, setSaveError] = useState('');
  // showCaptchaSecret 控制远程验证秘钥是否明文显示。
  const [showCaptchaSecret, setShowCaptchaSecret] = useState(false);
  // showCurrentPassword 控制当前密码是否明文显示。
  const [showCurrentPassword, setShowCurrentPassword] = useState(false);
  // showNewPassword 控制新密码是否明文显示。
  const [showNewPassword, setShowNewPassword] = useState(false);
  // credentialsSaving 表示登录凭据是否正在保存。
  const [credentialsSaving, setCredentialsSaving] = useState(false);
  // credentialsMessage 保存登录凭据操作提示。
  const [credentialsMessage, setCredentialsMessage] = useState<CredentialsMessage>(null);
  // credentials 保存登录凭据编辑草稿。
  const [credentials, setCredentials] = useState<CredentialsForm>(/* 当前回调处理用户交互或异步状态变化。 */ () => createCredentials());
  // requestStatus 表示最近一次设置请求的阶段。
  const [requestStatus, setRequestStatus] = useState<SettingsRequestStatus>('idle');
  // requestSequence 隔离刷新产生的旧响应。
  const requestSequence = useRef(0);
  // requestController 保存当前可取消的设置请求。
  const requestController = useRef<AbortController | null>(null);
  // credentialsRequestSequence 隔离登录凭据保存的旧响应，不与设置加载或保存共用代次。
  const credentialsRequestSequence = useRef(0);
  // credentialsRequestController 保存当前可取消的登录凭据保存请求。
  const credentialsRequestController = useRef<AbortController | null>(null);

  /** 取消当前设置请求并创建新的请求控制器。 */
  const beginRequest = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    // controller 是本次请求的取消控制器。
    requestController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    requestController.current = controller;
    requestSequence.current += 1;
    return { controller, sequence: requestSequence.current };
  }, []);

  /** 取消旧的登录凭据保存请求并创建独立的当前请求。 */
  const beginCredentialsRequest = useCallback(/* 当前回调隔离登录凭据保存的取消和晚到响应。 */ () => {
    credentialsRequestController.current?.abort();
    // controller 是本次登录凭据保存的取消控制器。
    const controller = new AbortController();
    credentialsRequestController.current = controller;
    credentialsRequestSequence.current += 1;
    return { controller, sequence: credentialsRequestSequence.current };
  }, []);

  // loadSettings 加载当前数据（设置）。
  const loadSettings = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    // request 是本次设置读取的代次与控制器。
    const { controller, sequence } = beginRequest();
    setLoading(true);
    setRequestStatus('loading');
    setLoadError('');
    Promise.all([
      getSystemSettings({ signal: controller.signal }),
      verifySession({ signal: controller.signal }),
    ]).then(/* 当前回调处理用户交互或异步状态变化。 */ ([data, session]) => {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) return;
      setSettings(data);
      if (session.username) setCredentials(/* 当前回调处理用户交互或异步状态变化。 */ previous => ({ ...previous, new_username: session.username || '' }));
      setRequestStatus('success');
    }).catch(/* 当前回调处理用户交互或异步状态变化。 */ error => {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return;
      setSettings(null);
      setLoadError(settingsErrorMessage(error, '加载配置失败'));
      setRequestStatus('error');
    }).finally(/* 当前回调处理用户交互或异步状态变化。 */ () => {
      if (isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) setLoading(false);
    });
  }, [beginRequest]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    loadSettings();
    return /* 当前回调处理用户交互或异步状态变化。 */ () => {
      requestController.current?.abort();
      credentialsRequestController.current?.abort();
    };
  }, [loadSettings]);

  // handleSave 提交当前配置草稿并返回保存结果；silentSuccess 让调用方用内联提示替代成功弹窗。
  const handleSave = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (options?: SettingsSaveOptions): Promise<boolean> => {
    // handleSave 提交当前配置草稿并保护过期响应；草稿缺失或正在保存时视为未成功。
    if (!settings || saving) return false;
    // request 是本次保存动作的代次与控制器。
    const { controller, sequence } = beginRequest();
    setSaving(true);
    setSaveError('');
    try {
      // payload 只包含当前 scope 白名单内的字段，保证系统页与 AI 页保存互不覆盖。
      await updateSystemSettings(buildPersistableSettings(settings, scope), { signal: controller.signal });
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) return false;
      // 保存成功后按当前范围给出对应的中文成功提示；页面自管提示时跳过弹窗以免两次反馈。
      if (!options?.silentSuccess) await appDialog.alert(SCOPE_SAVED_LABELS[scope], { variant: 'success' });
      return true;
    } catch (/* error 保存系统设置提交请求的失败原因；过期响应不会覆盖当前表单。 */ error) {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return false;
      setSaveError(settingsErrorMessage(error, '保存配置失败'));
      return false;
    } finally {
      if (isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) setSaving(false);
    }
  }, [beginRequest, saving, scope, settings]);

  // handleCredentialsSave 处理当前用户操作（CredentialsSave）。
  const handleCredentialsSave = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (event: React.FormEvent) => {
    // handleCredentialsSave 校验并提交登录凭据表单。
    event.preventDefault();
    setCredentialsMessage(null);
    // validationError 是前端校验得到的第一条可见错误。
    const validationError = validateCredentials(credentials);
    if (validationError) {
      setCredentialsMessage(createCredentialsMessage('error', validationError));
      return;
    }
    // request 是本次凭据保存动作的专属代次与控制器。
    const { controller, sequence } = beginCredentialsRequest();
    setCredentialsSaving(true);
    try {
      // result 是后端返回的凭据更新结果。
      const result = await updateLoginCredentials({
        current_password: credentials.current_password,
        new_username: credentials.new_username.trim(),
        new_password: credentials.new_password || undefined,
      }, { signal: controller.signal });
      if (!isCurrentSettingsRequest(credentialsRequestSequence.current, sequence, controller.signal)) return;
      if (!result.success) {
        setCredentialsMessage(createCredentialsMessage('error', result.message || '登录凭据更新失败'));
        return;
      }
      setCredentialsMessage(createCredentialsMessage('success', result.message || '登录凭据已更新'));
      window.setTimeout(/* 当前回调处理用户交互或异步状态变化。 */ () => window.location.reload(), 1400);
    } catch (/* error 保存登录凭据提交请求的失败原因；不写入日志或持久化状态。 */ error) {
      if (!isCurrentSettingsRequest(credentialsRequestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return;
      setCredentialsMessage(createCredentialsMessage('error', settingsErrorMessage(error, '登录凭据更新失败')));
    } finally {
      if (isCurrentSettingsRequest(credentialsRequestSequence.current, sequence, controller.signal)) setCredentialsSaving(false);
    }
  }, [beginCredentialsRequest, credentials]);

  return {
    settings, loading, loadError, saving, saveError,
    showCaptchaSecret, showCurrentPassword, showNewPassword, credentialsSaving, credentialsMessage,
    credentials, requestStatus, loadSettings, handleSave, handleCredentialsSave,
    setSettings, setShowCaptchaSecret,
    setShowCurrentPassword, setShowNewPassword, setCredentials, setCredentialsMessage,
  };
};
