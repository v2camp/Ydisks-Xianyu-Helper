// AIConfigEditor 组装策略 / 语料 / 边界三块表单化配置，并保留组合词语法速查；
// 是 AI 客服配置编辑区的单一入口，AISettings 只装配本组件。

import React from 'react';
import { FileJson, Sparkles } from 'lucide-react';
import type { SystemSettings } from '../api';
import { KnowledgeConfigForm, PolicyConfigForm, ScopeConfigForm } from './AIConfigForms';

// AIConfigEditorProps 描述 AI 客服配置编辑区需要的状态与回调。
export interface AIConfigEditorProps {
  /** settings 是当前系统配置草稿。 */
  settings: SystemSettings;
  /** onChange 更新系统配置草稿中的单个配置键。 */
  onChange: (patch: Partial<SystemSettings>) => void;
}

// AIConfigEditor 渲染 AI 客服三层配置表单与组合词语法速查。
export const AIConfigEditor: React.FC<AIConfigEditorProps> = ({ settings, onChange }) => {
  return (
    <section className="space-y-4">
      <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
        <div className="p-1.5 rounded-lg bg-violet-500 text-white">
          <FileJson className="w-4 h-4" />
        </div>
        AI 客服配置（可配置接管范围）
      </h3>
      <div className="ios-card rounded-xl p-6 bg-white space-y-6">
        <div className="bg-violet-50 border border-violet-200 rounded-xl p-4">
          <h4 className="font-bold text-violet-900 mb-2 flex items-center gap-2"><Sparkles className="w-4 h-4" />组合词语法</h4>
          <ul className="text-xs text-violet-800 space-y-1">
            <li>• 或 <code className="font-mono">|</code>　且 <code className="font-mono">&amp;</code>（相邻词默认且）　非 <code className="font-mono">!</code>　分组 <code className="font-mono">( )</code></li>
            <li>• 例：<code className="font-mono">(便宜 | 优惠) &amp; (元 | 块)</code>　例：<code className="font-mono">夸克 &amp; !会员</code></li>
            <li>• 双引号包裹的词按正则匹配：<code className="font-mono">"第[一二三\\d]季"</code></li>
            <li>• 留空表示不配置该项，系统回落内置行为（仅砍价接管）</li>
          </ul>
        </div>
        <PolicyConfigForm settings={settings} onChange={onChange} />
        <KnowledgeConfigForm settings={settings} onChange={onChange} />
        <ScopeConfigForm settings={settings} onChange={onChange} />
      </div>
    </section>
  );
};

export default AIConfigEditor;
