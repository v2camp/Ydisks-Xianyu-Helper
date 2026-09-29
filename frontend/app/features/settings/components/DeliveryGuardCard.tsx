import React from 'react';
import { Ban, Link2, ShieldAlert } from 'lucide-react';
import type { SystemSettings } from '../api';
import { parseDeliveryGuardConfig, serializeDeliveryGuardConfig } from '../state';
import type { DeliveryGuardConfig } from '../state';

// DeliveryGuardCardProps 描述发货内容门禁配置卡的受控输入。
interface DeliveryGuardCardProps {
  /** settings 是当前系统配置草稿，读取 delivery_content_guard JSON 原文。 */
  settings: SystemSettings;
  /** onChange 在开关或额外违禁词变化后写回序列化的门禁 JSON 补丁。 */
  onChange: (patch: Partial<SystemSettings>) => void;
}

// DeliveryGuardCard 渲染安全阀门区的「发货内容门禁」配置卡：违禁词门禁、链接健康检查与额外违禁词。
export const DeliveryGuardCard: React.FC<DeliveryGuardCardProps> = ({ settings, onChange }) => {
  // guardParse 是门禁 JSON 解析结果；非法文本回退默认配置并要求展示提示。
  const guardParse = parseDeliveryGuardConfig(settings.delivery_content_guard);
  // guard 是当前展示与编辑的门禁配置（非法 JSON 时等同默认值）。
  const guard = guardParse.config;

  // applyGuard 把字段补丁合并进当前门禁配置后序列化写回设置草稿。
  const applyGuard = (patch: Partial<DeliveryGuardConfig>) => {
    // next 是合并补丁后的完整门禁配置。
    const next = { ...guard, ...patch };
    onChange({ delivery_content_guard: serializeDeliveryGuardConfig(next) });
  };

  return (
    <div className="ios-card rounded-xl p-6 bg-white space-y-5">
      <div className="flex items-start gap-2">
        <div className="p-1.5 rounded-lg bg-red-500 text-white mt-0.5">
          <ShieldAlert className="w-4 h-4" />
        </div>
        <div>
          <h4 className="text-sm font-bold text-gray-800">发货内容门禁</h4>
          <p className="text-xs text-gray-500 mt-0.5">出站发货文本统一过门禁，命中即拒发并转人工处理。</p>
        </div>
      </div>

      {guardParse.invalid && (
        <p className="text-xs font-medium text-red-600">配置 JSON 非法，已回退默认值；修改任一项后保存将覆盖为合法 JSON。</p>
      )}

      <label className="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 cursor-pointer" htmlFor="delivery-guard-enabled">
        <input
          id="delivery-guard-enabled"
          type="checkbox"
          aria-label="发货内容违禁词门禁"
          className="mt-1"
          checked={guard.enabled}
          onChange={/* 违禁词门禁开关变化后合并写回门禁 JSON 草稿。 */ event => applyGuard({ enabled: event.target.checked })}
        />
        <span>
          <span className="block text-sm font-bold text-amber-900">发货内容违禁词门禁</span>
          <span className="mt-1 block text-xs leading-5 text-amber-800">自动发货文本命中联系方式/引流链接/违禁词时拒发并转人工处理。</span>
        </span>
      </label>

      <label className="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 cursor-pointer" htmlFor="delivery-guard-link-check">
        <input
          id="delivery-guard-link-check"
          type="checkbox"
          aria-label="发货前链接健康检查"
          className="mt-1"
          checked={guard.link_check}
          onChange={/* 链接健康检查开关变化后合并写回门禁 JSON 草稿。 */ event => applyGuard({ link_check: event.target.checked })}
        />
        <span>
          <span className="block text-sm font-bold text-amber-900">发货前链接健康检查</span>
          <span className="mt-1 block text-xs leading-5 text-amber-800">发送前校验网盘链接可达性，失效不发并转人工（4xx 反爬不拦截）。</span>
        </span>
      </label>

      <div className="space-y-2">
        <label className="block text-sm font-bold text-gray-800" htmlFor="delivery-guard-extra-words">额外违禁词</label>
        <textarea
          id="delivery-guard-extra-words"
          aria-label="额外违禁词"
          value={guard.extra_block_words}
          onChange={/* 额外违禁词输入变化后合并写回门禁 JSON 草稿。 */ event => applyGuard({ extra_block_words: event.target.value })}
          placeholder="例如：加微信, 扫码进群 | 站外引流"
          className="w-full ios-input px-4 py-3 rounded-xl h-24 text-sm leading-6 resize-y"
        />
        <p className="text-xs text-gray-500 flex items-center gap-1">
          <Ban className="w-3.5 h-3.5" />
          逗号或竖线分隔多个词条，命中即与内置规则同样拒发。
          <Link2 className="w-3.5 h-3.5 ml-1.5" />
          仅额外补充，不影响内置联系方式/外链检测。
        </p>
      </div>
    </div>
  );
};
