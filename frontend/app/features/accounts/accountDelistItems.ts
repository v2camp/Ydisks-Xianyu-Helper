// accountDelistItems 提供每日定时下架白名单所需的账号在售商品读取。
// 商品列表来自本地接口，不下发平台请求，因此打开弹窗即可安全加载。
import { useEffect,useRef,useState } from 'react';
import type { AccountItemOption } from './api';
import { getAccountItemOptions } from './api';

/** 下架白名单商品读取状态。 */
export type AccountDelistItemsState = {
  /** 账号在售商品选项。 */
  options: AccountItemOption[];
  /** 商品选项是否正在读取。 */
  loading: boolean;
  /** 商品读取失败时的非敏感提示；为空表示无错误。 */
  error: string;
};

/** 读取失败时的统一提示文案。 */
const delistItemsErrorMessage = '读取账号商品列表失败，可先保存设置稍后重试';

/** useAccountDelistItems 读取账号在售商品，供每日下架白名单勾选。 */
export const useAccountDelistItems = (accountID: string): AccountDelistItemsState => {
  // options 保存账号在售商品选项。
  const [options, setOptions] = useState<AccountItemOption[]>([]);
  // loading 表示商品选项是否正在读取。
  const [loading, setLoading] = useState(true);
  // error 保存商品读取失败提示。
  const [error, setError] = useState('');
  // controllerRef 保存当前商品读取请求控制器，账号切换时必须中断旧请求。
  const controllerRef = useRef<AbortController | null>(null);

  useEffect(/* 当前回调同步账号切换时的商品读取副作用。 */ () => {
    controllerRef.current?.abort();
    // controller 是本次商品读取请求的取消控制器。
    const controller = new AbortController();
    controllerRef.current = controller;
    setLoading(true);
    setError('');
    getAccountItemOptions(accountID, { signal: controller.signal }).then(/* 当前回调处理商品读取成功结果。 */ items => {
      if (controller.signal.aborted) return;
      setOptions(items);
    }).catch(/* 当前回调处理商品读取失败结果。 */ () => {
      if (controller.signal.aborted) return;
      setOptions([]);
      setError(delistItemsErrorMessage);
    }).finally(/* 当前回调收口商品读取加载状态。 */ () => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return /* 当前回调在账号切换或卸载时中断商品读取。 */ () => controller.abort();
  }, [accountID]);

  useEffect(/* 当前回调确保弹窗卸载时中断商品读取。 */ () => /* 当前回调在组件卸载时中断仍在进行的商品读取。 */ () => controllerRef.current?.abort(), []);

  return { options, loading, error };
};
