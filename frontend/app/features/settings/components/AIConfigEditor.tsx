// AIConfigEditor 提供 AI 客服三层可配置的 JSON 编辑区：边界（ai_scope_config）、
// 语料（ai_knowledge_config）、策略（ai_policy_config）。本地即时校验 JSON 合法性，
// 保存时随系统配置整体提交，改完即时生效无需重启。

import React from 'react';
import { FileJson,Sparkles } from 'lucide-react';
import type { SystemSettings } from '../api';

// SCOPE_DEFAULT 是边界配置的推荐起点：砍价+商品/发货/库存咨询交给 AI，投诉负向单独拦截。
const SCOPE_DEFAULT = `{
  "intents": [
    {"id": "bargain", "enabled": true, "ai": true, "match": "(便宜|优惠|少点|最低|砍价|降价|打折)|(能不能&(元|块))|(\\"\\d+(\\.\\d+)?\\s*(元|块)\\"&(卖|行|可以))"},
    {"id": "product_qa", "enabled": true, "ai": true, "match": "正版|出版|彩图|音频|目录|pdf|扫描|文字版|内容一样|有什么区别"},
    {"id": "delivery", "enabled": true, "ai": true, "match": "什么盘|网盘发货|资源码|鸿蒙|度盘|夸克|(百度网盘&发)"},
    {"id": "stock", "enabled": true, "ai": true, "match": "\\"第[一二三\\d]季\\"|单买|全集|完整版"}
  ],
  "negative": "退款|退货|投诉|差评|举报|骗子|骗人|假货|被骗|维权|违规|扣分|封号|申诉"
}`;

// KNOWLEDGE_DEFAULT 是语料配置的推荐起点：FAQ 问答与在售清单按实际店铺维护。
const KNOWLEDGE_DEFAULT = `{
  "faq": [
    {"category": "发货", "match": "百度|度盘|什么盘|网盘发货", "answer": "本店支持百度网盘、夸克、迅雷发货，拍下后自动发链接与提取码。"},
    {"category": "资源码", "match": "资源码|鸿蒙", "answer": "资源码是网盘极速转存码，复制到网盘 App 粘贴即可；鸿蒙系统不显示资源码按钮时可改用链接+提取码。"},
    {"category": "退款", "match": "退款|退钱|支持退吗", "answer": "虚拟电子资源拍下即自动发货，不支持退款；资源失效或缺少内容随时找我们补发。"},
    {"category": "更新", "match": "更新|包更新|后续", "answer": "本店承诺包更新，新一季出来会持续补档，已购用户会收到更新通知。"},
    {"category": "赠送", "match": "好评|赠送|送什么", "answer": "收货好评后赠送万部资源包，具体入口拍下后私信发送。"}
  ],
  "catalog": [
    {"title": "糯糯下山，师兄们都慌了", "detail": "1-3季全，网盘发货"},
    {"title": "梦遇崔郎", "detail": "92集完整版，含两集未删减"}
  ]
}`;

// POLICY_DEFAULT 是策略配置的推荐起点：报价与拒绝兜底话术。
const POLICY_DEFAULT = `{
  "min_price_reply": "可以优惠的最低价格是 {amount} 元，低于这个价格暂时无法成交。",
  "no_discount_reply": "抱歉，当前价格已经是最低价，暂时不能再优惠了。"
}`;

// AIConfigEditorProps 描述 AI 客服配置编辑区需要的状态与回调。
export interface AIConfigEditorProps {
  // settings 是当前系统配置草稿。
  settings: SystemSettings;
  // onChange 更新系统配置草稿中的单个配置键。
  onChange: (patch: Partial<SystemSettings>) => void;
}

// JsonField 描述一条 JSON 编辑框的展示元数据。
interface JsonField {
  // key 是系统设置键名。
  key: string;
  // label 是编辑框标题。
  label: string;
  // hint 是编辑框下方的使用说明。
  hint: string;
  // placeholder 是空值时的推荐配置示例。
  placeholder: string;
}

// FIELDS 是三条可配置 JSON 的展示配置。
const FIELDS: JsonField[] = [
  {
    key: 'ai_scope_config',
    label: '边界（哪些消息交给 AI）',
    hint: 'intents 是意图白名单：enabled=false 停用，ai=false 只记录不接管；negative 是负向组合词，命中一律不接管。',
    placeholder: SCOPE_DEFAULT,
  },
  {
    key: 'ai_knowledge_config',
    label: '语料（FAQ 与在售清单）',
    hint: 'faq 的 match 命中后把 answer 注入给 AI 作回答依据；catalog 是在售清单，买家问有没有/第几季时 AI 依此作答。',
    placeholder: KNOWLEDGE_DEFAULT,
  },
  {
    key: 'ai_policy_config',
    label: '策略（报价兜底话术）',
    hint: 'min_price_reply 的 {amount} 会被替换为金额；留空该项则用内置话术。',
    placeholder: POLICY_DEFAULT,
  },
];

// isValidJSON 判断文本是否为合法 JSON（空值视为空配置合法）。
function isValidJSON(value: string): boolean {
  if (value.trim() === '') return true;
  try {
    JSON.parse(value);
    return true;
  } catch {
    return false;
  }
}

// AIConfigEditor 渲染三层可配置的美化 JSON 编辑区。
export const AIConfigEditor: React.FC<AIConfigEditorProps> = ({ settings, onChange }) => {
  // renderField 渲染单条 JSON 编辑框。
  const renderField = (field: JsonField) => {
    // value 是当前草稿中的配置原文，缺省空串。
    const value = settings[field.key] as string | undefined ?? '';
    // valid 是本地 JSON 校验结果，非法时显示红色提示。
    const valid = isValidJSON(value);
    return (
      <div key={field.key} className="space-y-2">
        <label className="block text-sm font-bold text-gray-800">{field.label}</label>
        <textarea
          // 更新当前配置键草稿。
          onChange={/* 当前回调持久化单个 JSON 配置键的编辑。 */ event => onChange({ [field.key]: event.target.value })}
          value={value}
          placeholder={field.placeholder}
          spellCheck={false}
          className={`w-full ios-input px-4 py-3 rounded-xl h-56 font-mono text-xs leading-5 resize-y ${valid ? 'border-gray-200' : 'border-red-300'}`}
        />
        {!valid && <p className="text-xs text-red-500">JSON 格式非法，保存后将被忽略并回落默认行为</p>}
        <p className="text-xs text-gray-500">{field.hint}</p>
      </div>
    );
  };

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
        {FIELDS.map(renderField)}
      </div>
    </section>
  );
};

export default AIConfigEditor;