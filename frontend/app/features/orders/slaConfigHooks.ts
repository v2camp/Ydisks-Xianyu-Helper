import { useCallback, useEffect, useRef, useState } from 'react';
import { getDeliverySlaConfigRaw, getSlaAccounts, updateDeliverySlaConfig } from './api';
import type { SlaAccountOption } from './models';
import { parseDeliverySlaConfig, serializeDeliverySlaConfig } from './slaConfig';
import type { DeliverySlaConfig } from './slaConfig';

/** 发货 SLA 配置卡 Hook 返回的编辑状态与操作。 */
export interface DeliverySlaConfigState {
  /** config 是当前编辑中的 SLA 配置草稿。 */
  config: DeliverySlaConfig;
  /** invalidJson 表示服务端原文非法已回退默认，需要向用户提示。 */
  invalidJson: boolean;
  /** accounts 是账号下拉选项。 */
  accounts: SlaAccountOption[];
  /** loading 表示首屏读取是否正在进行。 */
  loading: boolean;
  /** saving 表示保存请求是否正在进行。 */
  saving: boolean;
  /** message 是保存成功或失败的用户可见提示。 */
  message: string;
  /** messageKind 表示提示的语义档位。 */
  messageKind: 'success' | 'error' | '';
  /** setDefaultMinutes 更新默认 SLA 分钟数，0 表示关闭。 */
  setDefaultMinutes: (minutes: number) => void;
  /** upsertAccountOverride 新增或更新指定账号的覆盖分钟数。 */
  upsertAccountOverride: (cookieId: string, minutes: number) => void;
  /** removeAccountOverride 删除指定账号的覆盖条目。 */
  removeAccountOverride: (cookieId: string) => void;
  /** save 提交当前草稿到系统设置。 */
  save: () => Promise<void>;
}

/** useDeliverySlaConfig 管理发货 SLA 配置的读取、编辑草稿与保存。 */
export const useDeliverySlaConfig = (enabled: boolean): DeliverySlaConfigState => {
  // config 保存当前编辑中的 SLA 配置草稿。
  const [config, setConfig] = useState<DeliverySlaConfig>({ default_minutes: 0, per_account: {} });
  // invalidJson 表示服务端 SLA 配置原文非法已回退默认。
  const [invalidJson, setInvalidJson] = useState(false);
  // accounts 保存账号下拉选项。
  const [accounts, setAccounts] = useState<SlaAccountOption[]>([]);
  // loading 表示首屏配置与账号读取是否正在进行。
  const [loading, setLoading] = useState(false);
  // saving 表示保存请求是否正在进行。
  const [saving, setSaving] = useState(false);
  // message 保存保存结果的用户可见提示。
  const [message, setMessage] = useState('');
  // messageKind 保存提示的语义档位。
  const [messageKind, setMessageKind] = useState<'success' | 'error' | ''>('');
  // abortRef 保存当前读取请求的取消控制器。
  const abortRef = useRef<AbortController | null>(null);

  useEffect(
    /* loadEffect 在管理员可见时读取 SLA 配置与账号下拉，卸载时取消请求。 */ () => {
    if (!enabled) return undefined;
    // controller 控制本次配置读取的取消生命周期。
    const controller = new AbortController();
    abortRef.current = controller;
    setLoading(true);
    void Promise.all([
      getDeliverySlaConfigRaw({ signal: controller.signal }),
      getSlaAccounts({ signal: controller.signal }),
    ]).then(
      /* loaded 是配置原文与账号下拉的并行读取结果。 */ ([raw, accountOptions]) => {
        if (controller.signal.aborted) return;
        // parsed 是配置原文的解析结果，非法原文回退默认并要求提示。
        const parsed = parseDeliverySlaConfig(raw);
        setConfig(parsed.config);
        setInvalidJson(parsed.invalid);
        setAccounts(accountOptions);
        setLoading(false);
      },
    ).catch(
      /* loadError 是配置读取失败原因；取消不提示，其他错误提示并结束加载。 */ (loadError: unknown) => {
        if (controller.signal.aborted) return;
        setMessage(loadError instanceof Error ? loadError.message : '加载发货 SLA 配置失败');
        setMessageKind('error');
        setLoading(false);
      },
    );
    return /* cleanup 在卸载或重新加载时取消读取请求。 */ () => controller.abort();
    },
    [enabled],
  );

  // setDefaultMinutes 更新默认 SLA 分钟数。
  const setDefaultMinutes = useCallback(/* defaultMinutesCallback 用非负整数更新默认分钟数。 */ (minutes: number) => {
    // safeMinutes 是归一后的默认分钟数，负数与非法值回退 0。
    const safeMinutes = Number.isFinite(minutes) && minutes > 0 ? Math.floor(minutes) : 0;
    setConfig(/* current 是更新前的草稿，返回默认分钟数更新后的新草稿。 */ current => ({ ...current, default_minutes: safeMinutes }));
  }, []);

  // upsertAccountOverride 新增或更新指定账号的覆盖分钟数。
  const upsertAccountOverride = useCallback(/* overrideUpsertCallback 写入指定 cookie_id 的覆盖分钟数。 */ (cookieId: string, minutes: number) => {
    if (!cookieId) return;
    // safeMinutes 是归一后的覆盖分钟数，负数与非法值回退 0。
    const safeMinutes = Number.isFinite(minutes) && minutes > 0 ? Math.floor(minutes) : 0;
    setConfig(/* current 是更新前的草稿，返回覆盖表更新后的新草稿。 */ current => ({
      ...current,
      per_account: { ...current.per_account, [cookieId]: safeMinutes },
    }));
  }, []);

  // removeAccountOverride 删除指定账号的覆盖条目。
  const removeAccountOverride = useCallback(/* overrideRemoveCallback 移除指定 cookie_id 的覆盖。 */ (cookieId: string) => {
    setConfig(/* current 是更新前的草稿，返回覆盖表移除后的新草稿。 */ current => {
      // nextOverrides 是移除目标账号后的覆盖表副本。
      const nextOverrides = { ...current.per_account };
      delete nextOverrides[cookieId];
      return { ...current, per_account: nextOverrides };
    });
  }, []);

  // save 将当前草稿序列化后写入系统设置。
  const save = useCallback(/* saveCallback 序列化草稿并提交 delivery_sla_config。 */ async (): Promise<void> => {
    if (saving) return;
    setSaving(true);
    setMessage('');
    setMessageKind('');
    try {
      // rawJson 是序列化后的 SLA 配置 JSON 串。
      const rawJson = serializeDeliverySlaConfig(config);
      await updateDeliverySlaConfig(rawJson);
      if (abortRef.current?.signal.aborted) return;
      setInvalidJson(false);
      setMessage('发货 SLA 配置已保存');
      setMessageKind('success');
    } catch (/* saveError 是保存失败原因。 */ saveError) {
      if (abortRef.current?.signal.aborted) return;
      setMessage(saveError instanceof Error ? saveError.message : '保存发货 SLA 配置失败');
      setMessageKind('error');
    } finally {
      setSaving(false);
    }
  }, [config, saving]);

  return { config, invalidJson, accounts, loading, saving, message, messageKind, setDefaultMinutes, upsertAccountOverride, removeAccountOverride, save };
};
