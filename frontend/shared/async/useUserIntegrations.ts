import { useCallback,useEffect,useRef,useState } from 'react';
import type { SaveBarState } from '../ui/SaveBar';
import type { UserIntegrationSettings } from '../api-contract/userIntegrations';
import { getUserIntegrations,updateUserIntegrations } from '../api-contract/userIntegrations';

/** UserIntegrationsState 是当前用户 AI、QQ 与 SMTP 集成配置的异步状态。 */
export interface UserIntegrationsState {
  /** settings 是当前用户的集成配置草稿；敏感键只含 configured 标记。 */
  settings: UserIntegrationSettings;
  /** loading 表示首次读取是否仍在进行。 */
  loading: boolean;
  /** loadError 是读取失败的用户可见文案。 */
  loadError: string;
  /** saveState 是区块保存条的四态。 */
  saveState: SaveBarState;
  /** saveError 是保存失败的具体原因。 */
  saveError: string;
  /** setSettings 更新集成配置草稿。 */
  setSettings: React.Dispatch<React.SetStateAction<UserIntegrationSettings>>;
  /** load 重新读取当前用户的集成配置。 */
  load: () => Promise<void>;
  /** save 保存当前草稿；传入 override 时只提交该载荷，供只编辑部分键的页面使用。 */
  save: (override?: UserIntegrationSettings) => Promise<void>;
  /** resetSaveState 在成功提示结束后复位保存条。 */
  resetSaveState: () => void;
}

/** useUserIntegrations 统一管理当前用户 AI、QQ 与 SMTP 配置的读取与保存，供多个 feature 共用。 */
export const useUserIntegrations = (): UserIntegrationsState => {
  // settings 保存当前用户的集成配置草稿。
  const [settings, setSettings] = useState<UserIntegrationSettings>({});
  // loading 表示首次读取是否仍在进行。
  const [loading, setLoading] = useState(true);
  // loadError 保存读取失败的用户可见文案。
  const [loadError, setLoadError] = useState('');
  // saving 表示保存请求是否正在执行。
  const [saving, setSaving] = useState(false);
  // saveState 保存区块保存条的四态。
  const [saveState, setSaveState] = useState<SaveBarState>('idle');
  // saveError 保存保存失败的具体原因。
  const [saveError, setSaveError] = useState('');
  // generation 隔离读取与保存动作的旧响应。
  const generation = useRef(0);
  // abort 保存当前读取请求的取消控制器。
  const abort = useRef<AbortController | null>(null);

  // load 读取当前用户的集成配置并丢弃过期响应。
  const load = useCallback(/* 读取回调在组件挂载或手动刷新时执行。 */ async () => {
    // current 是本次读取的代次。
    const current = ++generation.current;
    abort.current?.abort();
    // controller 允许卸载或下一次读取取消当前请求。
    const controller = new AbortController();
    abort.current = controller;
    setLoading(true);
    setLoadError('');
    try {
      // result 是当前用户的集成配置，敏感键只含 configured 标记。
      const result = await getUserIntegrations({ signal: controller.signal });
      if (current !== generation.current) return;
      setSettings(result || {});
    } catch (/* error 是读取失败原因，不包含任何秘密值。 */ error) {
      if (current !== generation.current || controller.signal.aborted) return;
      console.error('加载用户集成配置失败', error);
      setLoadError('读取集成配置失败，请重试');
    } finally {
      if (current === generation.current) setLoading(false);
    }
  }, []);

  // save 提交当前集成配置草稿或调用方指定的局部载荷。
  const save = useCallback(/* 保存回调把草稿提交给用户集成接口。 */ async (override?: UserIntegrationSettings) => {
    if (saving) return;
    // current 是本次保存的代次。
    const current = ++generation.current;
    // payload 是本次实际提交的载荷；局部编辑页面用它收窄提交范围。
    const payload = override ?? settings;
    setSaving(true);
    setSaveState('saving');
    setSaveError('');
    try {
      await updateUserIntegrations(payload);
      if (current !== generation.current) return;
      setSaveState('success');
      await load();
    } catch (/* error 是保存失败原因，不包含任何秘密值。 */ error) {
      if (current !== generation.current) return;
      console.error('保存用户集成配置失败', error);
      setSaveError('保存失败，请检查输入后重试');
      setSaveState('error');
    } finally {
      if (current === generation.current) setSaving(false);
    }
  }, [load, saving, settings]);

  // resetSaveState 在成功提示结束后把保存条复位为空闲。
  const resetSaveState = useCallback(/* 复位回调只调整保存条状态。 */ () => setSaveState('idle'), []);

  useEffect(/* 挂载时读取一次，卸载时取消未完成请求。 */ () => {
    void load();
    return (/* cleanup 使旧响应失效并取消请求。 */ () => {
      generation.current += 1;
      abort.current?.abort();
    });
  }, [load]);

  return { settings, loading, loadError, saveState, saveError, setSettings, load, save, resetSaveState };
};
