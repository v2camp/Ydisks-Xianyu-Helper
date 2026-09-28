// AIConfigForms 把策略 / 语料 / 边界三层 AI 客服 JSON 配置表单化：
// 每块提供结构化编辑与 JSON 高级视图切换，表单保存只改已知字段并保留未知键，
// 推荐配置可一键填入。推荐内容取自引擎内置起点话术、FAQ 语料与四条常用意图。

import React, { useState } from 'react';
import { FileJson, Sparkles } from 'lucide-react';
import type { SystemSettings } from '../api';
import { EditableList } from './EditableList';

// AIConfigBlockProps 描述单块 AI 配置表单共用的状态与回调。
export interface AIConfigBlockProps {
  /** settings 是当前系统配置草稿。 */
  settings: SystemSettings;
  /** onChange 更新系统配置草稿中的单个配置键。 */
  onChange: (patch: Partial<SystemSettings>) => void;
}

// PolicyRecommend 是「填入推荐话术」写入的两个已知策略字段。
export const POLICY_RECOMMENDED = {
  /** min_price_reply 是报价让步到最低价时的兜底话术，{amount} 运行时替换为金额。 */
  min_price_reply: '可以优惠的最低价格是 {amount} 元，低于这个价格暂时无法成交。',
  /** no_discount_reply 是拒绝继续议价时的兜底话术。 */
  no_discount_reply: '抱歉，当前价格已经是最低价，暂时不能再优惠了。',
};

// FAQ_RECOMMENDED 是「填入推荐语料」写入的 FAQ 问答起点，按实际店铺改写。
export const FAQ_RECOMMENDED = [
  { category: '发货', match: '百度|度盘|什么盘|网盘发货', answer: '本店支持百度网盘、夸克、迅雷发货，拍下后自动发链接与提取码。' },
  { category: '资源码', match: '资源码|鸿蒙', answer: '资源码是网盘极速转存码，复制到网盘 App 粘贴即可；鸿蒙系统不显示资源码按钮时可改用链接+提取码。' },
  { category: '退款', match: '退款|退钱|支持退吗', answer: '虚拟电子资源拍下即自动发货，不支持退款；资源失效或缺少内容随时找我们补发。' },
  { category: '更新', match: '更新|包更新|后续', answer: '本店承诺包更新，新一季出来会持续补档，已购用户会收到更新通知。' },
  { category: '赠送', match: '好评|赠送|送什么', answer: '收货好评后赠送万部资源包，具体入口拍下后私信发送。' },
];

// CATALOG_RECOMMENDED 是「填入推荐语料」写入的在售清单起点，可直接改成自家在售。
export const CATALOG_RECOMMENDED = [
  { title: '糯糯下山，师兄们都慌了', detail: '1-3季全，网盘发货' },
  { title: '梦遇崔郎', detail: '92集完整版，含两集未删减' },
];

// IntentTemplate 是常用意图模板芯片的展示与插入数据。
export interface IntentTemplate {
  /** id 是插入意图行的默认 ID。 */
  id: string;
  /** label 是芯片展示文案。 */
  label: string;
  /** match 是插入意图行的默认匹配规则。 */
  match: string;
}

// INTENT_TEMPLATES 是常用意图模板芯片，内容取自推荐边界配置的四条意图。
export const INTENT_TEMPLATES: IntentTemplate[] = [
  {
    id: 'bargain',
    label: '砍价',
    match: '(便宜|优惠|少点|最低|砍价|降价|打折)|(能不能&(元|块))|("\\d+(\\.\\d+)?\\s*(元|块)"&(卖|行|可以))',
  },
  {
    id: 'product_qa',
    label: '商品问答',
    match: '正版|出版|彩图|音频|目录|pdf|扫描|文字版|内容一样|有什么区别',
  },
  {
    id: 'delivery',
    label: '发货',
    match: '什么盘|网盘发货|资源码|鸿蒙|度盘|夸克|(百度网盘&发)',
  },
  {
    id: 'stock',
    label: '库存',
    match: '"第[一二三\\d]季"|单买|全集|完整版',
  },
];

// NEGATIVE_RECOMMENDED 是负向组合词推荐值，命中一律不交给 AI。
export const NEGATIVE_RECOMMENDED = '退款|退货|投诉|差评|举报|骗子|骗人|假货|被骗|维权|违规|扣分|封号|申诉';

// FAQRow 是语料 FAQ 一行的已知字段与可保留的扩展字段。
export interface FAQRow {
  /** category 是 FAQ 分类标题。 */
  category: string;
  /** match 是命中该 FAQ 的组合词。 */
  match: string;
  /** answer 是命中后注入给 AI 的回答依据。 */
  answer: string;
  /** 未知扩展键在编辑与回写时原样保留。 */
  [key: string]: unknown;
}

// CatalogRow 是在售清单一行的已知字段与可保留的扩展字段。
export interface CatalogRow {
  /** title 是在售商品标题。 */
  title: string;
  /** detail 是商品补充详情，例如季数与发货方式。 */
  detail: string;
  /** 未知扩展键在编辑与回写时原样保留。 */
  [key: string]: unknown;
}

// IntentRow 是边界意图一行的已知字段与可保留的扩展字段。
export interface IntentRow {
  /** id 是意图标识，可编辑。 */
  id: string;
  /** enabled 表示该意图是否启用。 */
  enabled: boolean;
  /** ai 表示该意图是否交给 AI 接管。 */
  ai: boolean;
  /** match 是意图匹配规则（组合词语法）。 */
  match: string;
  /** 未知扩展键在编辑与回写时原样保留。 */
  [key: string]: unknown;
}

// ConfigBlockProps 描述表单 / JSON 双视图配置块的容器属性。
interface ConfigBlockProps {
  /** icon 是标题左侧的图标节点。 */
  icon: React.ReactNode;
  /** title 是配置块标题。 */
  title: string;
  /** hint 是配置块使用说明，两种视图下都展示。 */
  hint: string;
  /** placeholder 是 JSON 视图空值示例原文。 */
  placeholder: string;
  /** value 是当前配置 JSON 草稿原文。 */
  value: string;
  /** onChange 在表单或 JSON 编辑后回写配置 JSON 草稿。 */
  onChange: (value: string) => void;
  /** children 是表单视图的结构化编辑内容。 */
  children: React.ReactNode;
}

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

// parseConfigObject 把配置 JSON 解析为对象；空、非法或非对象时返回空对象。
function parseConfigObject(raw: string): Record<string, unknown> {
  if (raw.trim() === '') return {};
  try {
    // value 是解析后的原始 JSON 值。
    const value = JSON.parse(raw);
    if (value && typeof value === 'object' && !Array.isArray(value)) return value as Record<string, unknown>;
    return {};
  } catch {
    return {};
  }
}

// parseRowArray 从配置对象中取出指定列表字段并归一为对象行数组。
function parseRowArray(parsed: Record<string, unknown>, key: string, normalize: (raw: unknown) => object): object[] {
  // rawRows 是原始列表字段值。
  const rawRows = parsed[key];
  if (!Array.isArray(rawRows)) return [];
  return rawRows.map(normalize);
}

// normalizeFaqRow 把 FAQ 原始行归一为表单已知字段，未知键原样保留。
function normalizeFaqRow(raw: unknown): FAQRow {
  // source 是待归一的原始行对象；非对象回退空对象。
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {};
  return {
    ...source,
    category: typeof source.category === 'string' ? source.category : '',
    match: typeof source.match === 'string' ? source.match : '',
    answer: typeof source.answer === 'string' ? source.answer : '',
  };
}

// normalizeCatalogRow 把在售清单原始行归一为表单已知字段，未知键原样保留。
function normalizeCatalogRow(raw: unknown): CatalogRow {
  // source 是待归一的原始行对象；非对象回退空对象。
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {};
  return {
    ...source,
    title: typeof source.title === 'string' ? source.title : '',
    detail: typeof source.detail === 'string' ? source.detail : '',
  };
}

// normalizeIntentRow 把意图原始行归一为表单已知字段，未知键原样保留。
function normalizeIntentRow(raw: unknown): IntentRow {
  // source 是待归一的原始行对象；非对象回退空对象。
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {};
  return {
    ...source,
    id: typeof source.id === 'string' ? source.id : '',
    enabled: typeof source.enabled === 'boolean' ? source.enabled : true,
    ai: typeof source.ai === 'boolean' ? source.ai : true,
    match: typeof source.match === 'string' ? source.match : '',
  };
}

// createEmptyFaq 构造新增 FAQ 行的空初始数据。
function createEmptyFaq(): FAQRow {
  return { category: '', match: '', answer: '' };
}

// createEmptyCatalog 构造新增在售清单行的空初始数据。
function createEmptyCatalog(): CatalogRow {
  return { title: '', detail: '' };
}

// createEmptyIntent 构造新增意图行的空初始数据。
function createEmptyIntent(): IntentRow {
  return { id: '', enabled: true, ai: true, match: '' };
}

// ConfigBlock 渲染配置块外壳：标题、JSON 切换按钮、JSON 编辑区与表单子内容。
const ConfigBlock: React.FC<ConfigBlockProps> = ({ icon, title, hint, placeholder, value, onChange, children }) => {
  // viewMode 是当前视图：form 表单视图，json 高级视图。
  const [viewMode, setViewMode] = useState<'form' | 'json'>('form');
  // viewError 是视图切换被阻止时的提示文案。
  const [viewError, setViewError] = useState('');

  // toggleView 在表单与 JSON 视图间切换；JSON 非法时禁止切回表单并提示错误。
  const toggleView = () => {
    if (viewMode === 'json') {
      if (!isValidJSON(value)) {
        setViewError('JSON 格式非法，修正后才能切回表单');
        return;
      }
      setViewError('');
      setViewMode('form');
      return;
    }
    setViewError('');
    setViewMode('json');
  };

  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2">
          <div className="p-1.5 rounded-lg bg-brand text-white mt-0.5">{icon}</div>
          <div>
            <h4 className="text-sm font-bold text-gray-800">{title}</h4>
            <p className="text-xs text-gray-500 mt-0.5">{hint}</p>
          </div>
        </div>
        <button
          type="button"
          onClick={toggleView}
          className="inline-flex items-center gap-1 rounded-lg border border-gray-200 px-2.5 py-1.5 text-xs font-bold text-gray-600 transition-colors hover:border-brand hover:text-brand"
        >
          <FileJson className="w-3.5 h-3.5" />
          {viewMode === 'form' ? 'JSON' : '表单'}
        </button>
      </div>
      {viewError && <p className="text-xs text-red-500">{viewError}</p>}
      {viewMode === 'form' ? children : (
        <div className="space-y-2">
          <textarea
            value={value}
            onChange={/* 输入变化直接回写配置 JSON 草稿原文。 */ event => onChange(event.target.value)}
            placeholder={placeholder}
            spellCheck={false}
            aria-label={`${title} JSON 原文`}
            className={`w-full ios-input px-4 py-3 rounded-xl h-56 font-mono text-xs leading-5 resize-y ${isValidJSON(value) ? 'border-gray-200' : 'border-red-300'}`}
          />
          {!isValidJSON(value) && (
            <p className="text-xs text-red-500">JSON 格式非法</p>
          )}
        </div>
      )}
    </section>
  );
};

// PolicyPatch 是策略表单写回草稿的已知字段补丁。
interface PolicyPatch {
  /** min_price_reply 是最低价回复话术。 */
  min_price_reply?: string;
  /** no_discount_reply 是不议价回复话术。 */
  no_discount_reply?: string;
}

// KnowledgePatch 是语料表单写回草稿的已知列表字段补丁。
interface KnowledgePatch {
  /** faq 是 FAQ 问答列表。 */
  faq?: FAQRow[];
  /** catalog 是在售清单列表。 */
  catalog?: CatalogRow[];
}

// ScopePatch 是边界表单写回草稿的已知字段补丁。
interface ScopePatch {
  /** intents 是意图白名单列表。 */
  intents?: IntentRow[];
  /** negative 是负向组合词。 */
  negative?: string;
}

// PolicyConfigForm 编辑策略配置：最低价回复与不议价回复两张文案卡。
export const PolicyConfigForm: React.FC<AIConfigBlockProps> = ({ settings, onChange }) => {
  // raw 是策略配置 JSON 草稿原文，缺省空串。
  const raw = typeof settings.ai_policy_config === 'string' ? settings.ai_policy_config : '';
  // parsed 是策略配置对象；未知键在后续序列化时原样写回。
  const parsed = parseConfigObject(raw);
  // minPriceReply 是「最低价回复」文案草稿。
  const minPriceReply = typeof parsed.min_price_reply === 'string' ? parsed.min_price_reply : '';
  // noDiscountReply 是「不议价回复」文案草稿。
  const noDiscountReply = typeof parsed.no_discount_reply === 'string' ? parsed.no_discount_reply : '';
  // isEmptyPolicy 表示两个话术字段都为空，决定是否展示推荐填充按钮。
  const isEmptyPolicy = minPriceReply.trim() === '' && noDiscountReply.trim() === '';

  // applyPolicy 把已知策略字段写回草稿对象并序列化，未触碰的未知键原样保留。
  const applyPolicy = (patch: PolicyPatch) => {
    // next 是合并已知字段后的策略配置对象。
    const next = { ...parsed, ...patch };
    onChange({ ai_policy_config: JSON.stringify(next, null, 2) });
  };

  // fillRecommended 把推荐话术写入两个已知字段，保留已存在的未知键。
  const fillRecommended = () => {
    applyPolicy(POLICY_RECOMMENDED);
  };

  return (
    <ConfigBlock
      icon={<Sparkles className="w-4 h-4" />}
      title="策略（报价兜底话术）"
      hint="min_price_reply 的 {amount} 会被替换为金额；留空该项则用内置话术。"
      placeholder={JSON.stringify(POLICY_RECOMMENDED, null, 2)}
      value={raw}
      onChange={/* value 是策略配置 JSON 草稿原文，直接写回系统配置。 */ value => onChange({ ai_policy_config: value })}
    >
      <div className="space-y-4">
        {isEmptyPolicy && (
          <button
            type="button"
            onClick={fillRecommended}
            className="inline-flex items-center gap-1.5 rounded-xl bg-violet-50 border border-violet-200 px-3 py-2 text-xs font-bold text-violet-700 transition-colors hover:bg-violet-100"
          >
            <Sparkles className="w-3.5 h-3.5" />
            填入推荐话术
          </button>
        )}
        <div className="ios-card rounded-xl p-5 bg-white space-y-2">
          <label className="block text-sm font-bold text-gray-800" htmlFor="policy-min-price-reply">最低价回复</label>
          <textarea
            id="policy-min-price-reply"
            value={minPriceReply}
            onChange={/* 输入变化更新最低价回复话术草稿。 */ event => applyPolicy({ min_price_reply: event.target.value })}
            placeholder="可以优惠的最低价格是 {amount} 元，低于这个价格暂时无法成交。"
            aria-label="最低价回复"
            className="w-full ios-input px-4 py-3 rounded-xl h-24 text-sm leading-6 resize-y"
          />
          <p className="text-xs text-gray-500">占位符 <code className="font-mono">{'{amount}'}</code> 运行时替换为金额</p>
        </div>
        <div className="ios-card rounded-xl p-5 bg-white space-y-2">
          <label className="block text-sm font-bold text-gray-800" htmlFor="policy-no-discount-reply">不议价回复</label>
          <textarea
            id="policy-no-discount-reply"
            value={noDiscountReply}
            onChange={/* 输入变化更新不议价回复话术草稿。 */ event => applyPolicy({ no_discount_reply: event.target.value })}
            placeholder="抱歉，当前价格已经是最低价，暂时不能再优惠了。"
            aria-label="不议价回复"
            className="w-full ios-input px-4 py-3 rounded-xl h-24 text-sm leading-6 resize-y"
          />
        </div>
      </div>
    </ConfigBlock>
  );
};

// KnowledgeConfigForm 编辑语料配置：FAQ 问答与在售清单两个可增删列表。
export const KnowledgeConfigForm: React.FC<AIConfigBlockProps> = ({ settings, onChange }) => {
  // raw 是语料配置 JSON 草稿原文，缺省空串。
  const raw = typeof settings.ai_knowledge_config === 'string' ? settings.ai_knowledge_config : '';
  // parsed 是语料配置对象；未知键在后续序列化时原样写回。
  const parsed = parseConfigObject(raw);
  // faqRows 是 FAQ 列表的表单行数据。
  const faqRows = parseRowArray(parsed, 'faq', normalizeFaqRow) as FAQRow[];
  // catalogRows 是在售清单的表单行数据。
  const catalogRows = parseRowArray(parsed, 'catalog', normalizeCatalogRow) as CatalogRow[];
  // isEmptyKnowledge 表示两列表都为空，决定是否展示推荐填充按钮。
  const isEmptyKnowledge = faqRows.length === 0 && catalogRows.length === 0;

  // applyKnown 把已知列表字段写回草稿对象并序列化，未触碰的未知键原样保留。
  const applyKnown = (patch: KnowledgePatch) => {
    // next 是合并已知列表字段后的语料配置对象。
    const next = { ...parsed, ...patch };
    onChange({ ai_knowledge_config: JSON.stringify(next, null, 2) });
  };

  // fillRecommended 把推荐 FAQ 与在售清单写入已知字段，保留已存在的未知键。
  const fillRecommended = () => {
    applyKnown({ faq: FAQ_RECOMMENDED.map(/* rowCopier 复制推荐 FAQ 行，避免共享可变对象。 */ row => ({ ...row })), catalog: CATALOG_RECOMMENDED.map(/* rowCopier 复制推荐在售行，避免共享可变对象。 */ row => ({ ...row })) });
  };

  return (
    <ConfigBlock
      icon={<Sparkles className="w-4 h-4" />}
      title="语料（FAQ 与在售清单）"
      hint="faq 的 match 命中后把 answer 注入给 AI 作回答依据；catalog 是在售清单，买家问有没有/第几季时 AI 依此作答。"
      placeholder={JSON.stringify({ faq: FAQ_RECOMMENDED, catalog: CATALOG_RECOMMENDED }, null, 2)}
      value={raw}
      onChange={/* value 是语料配置 JSON 草稿原文，直接写回系统配置。 */ value => onChange({ ai_knowledge_config: value })}
    >
      <div className="space-y-4">
        {isEmptyKnowledge && (
          <button
            type="button"
            onClick={fillRecommended}
            className="inline-flex items-center gap-1.5 rounded-xl bg-violet-50 border border-violet-200 px-3 py-2 text-xs font-bold text-violet-700 transition-colors hover:bg-violet-100"
          >
            <Sparkles className="w-3.5 h-3.5" />
            填入推荐语料
          </button>
        )}
        <div className="ios-card rounded-xl p-5 bg-white space-y-3">
          <label className="block text-sm font-bold text-gray-800">FAQ 问答</label>
          <EditableList
            rows={faqRows}
            onChange={/* rows 是变更后的 FAQ 列表，整体写回语料草稿。 */ rows => applyKnown({ faq: rows })}
            createRow={createEmptyFaq}
            getRowKey={/* rowKeyProvider 用行索引拼出 FAQ 行 React key。 */ (_row, index) => `faq-${index}`}
            addButtonText="添加 FAQ"
            emptyText="暂无 FAQ，可点「填入推荐语料」或手动添加"
            rowNamePrefix="FAQ"
            renderRow={/* renderRow 渲染当前列表行的编辑控件。 */ (row, index, update) => (
              <div className="space-y-2">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  <input
                    value={row.category}
                    onChange={/* 输入变化更新 FAQ 行分类。 */ event => update({ category: event.target.value })}
                    placeholder="分类，如 发货"
                    aria-label={`第 ${index + 1} 行分类`}
                    className="w-full ios-input px-3 py-2 rounded-lg text-sm"
                  />
                  <input
                    value={row.match}
                    onChange={/* 输入变化更新 FAQ 行命中词。 */ event => update({ match: event.target.value })}
                    placeholder="命中词，如 百度|度盘"
                    aria-label={`第 ${index + 1} 行命中词`}
                    className="w-full ios-input px-3 py-2 rounded-lg text-sm"
                  />
                </div>
                <textarea
                  value={row.answer}
                  onChange={/* 输入变化更新 FAQ 行回答依据。 */ event => update({ answer: event.target.value })}
                  placeholder="命中后注入给 AI 的回答依据"
                  aria-label={`第 ${index + 1} 行回答`}
                  className="w-full ios-input px-3 py-2 rounded-lg text-sm h-20 resize-y"
                />
              </div>
            )}
          />
        </div>
        <div className="ios-card rounded-xl p-5 bg-white space-y-3">
          <label className="block text-sm font-bold text-gray-800">在售清单</label>
          <EditableList
            rows={catalogRows}
            onChange={/* rows 是变更后的在售清单，整体写回语料草稿。 */ rows => applyKnown({ catalog: rows })}
            createRow={createEmptyCatalog}
            getRowKey={/* rowKeyProvider 用行索引拼出在售行 React key。 */ (_row, index) => `catalog-${index}`}
            addButtonText="添加商品"
            emptyText="暂无在售商品，可点「填入推荐语料」后改成自家在售"
            rowNamePrefix="在售"
            renderRow={/* renderRow 渲染当前列表行的编辑控件。 */ (row, index, update) => (
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <input
                  value={row.title}
                  onChange={/* 输入变化更新在售行标题。 */ event => update({ title: event.target.value })}
                  placeholder="标题，如 糯糯下山"
                  aria-label={`第 ${index + 1} 行标题`}
                  className="w-full ios-input px-3 py-2 rounded-lg text-sm"
                />
                <input
                  value={row.detail}
                  onChange={/* 输入变化更新在售行详情。 */ event => update({ detail: event.target.value })}
                  placeholder="详情，如 1-3季全，网盘发货"
                  aria-label={`第 ${index + 1} 行详情`}
                  className="w-full ios-input px-3 py-2 rounded-lg text-sm"
                />
              </div>
            )}
          />
        </div>
      </div>
    </ConfigBlock>
  );
};

// ScopeConfigForm 编辑边界配置：意图列表、常用意图模板芯片与负向词。
export const ScopeConfigForm: React.FC<AIConfigBlockProps> = ({ settings, onChange }) => {
  // raw 是边界配置 JSON 草稿原文，缺省空串。
  const raw = typeof settings.ai_scope_config === 'string' ? settings.ai_scope_config : '';
  // parsed 是边界配置对象；未知键在后续序列化时原样写回。
  const parsed = parseConfigObject(raw);
  // intentRows 是意图列表的表单行数据。
  const intentRows = parseRowArray(parsed, 'intents', normalizeIntentRow) as IntentRow[];
  // negative 是负向组合词原文，| 分隔。
  const negative = typeof parsed.negative === 'string' ? parsed.negative : '';

  // applyKnown 把已知边界字段写回草稿对象并序列化，未触碰的未知键原样保留。
  const applyKnown = (patch: ScopePatch) => {
    // next 是合并已知字段后的边界配置对象。
    const next = { ...parsed, ...patch };
    onChange({ ai_scope_config: JSON.stringify(next, null, 2) });
  };

  // insertTemplate 把常用意图模板追加到意图列表末尾。
  const insertTemplate = (template: IntentTemplate) => {
    applyKnown({ intents: [...intentRows, { id: template.id, enabled: true, ai: true, match: template.match, }] });
  };

  return (
    <ConfigBlock
      icon={<Sparkles className="w-4 h-4" />}
      title="边界（哪些消息交给 AI）"
      hint="intents 是意图白名单：enabled=false 停用，ai=false 只记录不接管；negative 是负向组合词，命中一律不接管。"
      placeholder={JSON.stringify({ intents: INTENT_TEMPLATES.map(/* template 是常用意图模板，占位示例只取已知字段。 */ template => ({ id: template.id, enabled: true, ai: true, match: template.match })), negative: NEGATIVE_RECOMMENDED }, null, 2)}
      value={raw}
      onChange={/* value 是边界配置 JSON 草稿原文，直接写回系统配置。 */ value => onChange({ ai_scope_config: value })}
    >
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-bold text-gray-500">常用意图</span>
          {INTENT_TEMPLATES.map(/* template 是待插入的常用意图模板。 */ template => (
            <button
              key={template.id}
              type="button"
              onClick={/* 当前回调把常用意图模板追加进意图列表。 */ () => insertTemplate(template)}
              className="rounded-full border border-violet-200 bg-violet-50 px-3 py-1.5 text-xs font-bold text-violet-700 transition-colors hover:bg-violet-100"
            >
              + {template.label}
            </button>
          ))}
        </div>
        <div className="ios-card rounded-xl p-5 bg-white space-y-3">
          <label className="block text-sm font-bold text-gray-800">意图列表</label>
          <EditableList
            rows={intentRows}
            onChange={/* rows 是变更后的意图列表，整体写回边界草稿。 */ rows => applyKnown({ intents: rows })}
            createRow={createEmptyIntent}
            getRowKey={/* rowKeyProvider 用行索引拼出意图行 React key。 */ (_row, index) => `intent-${index}`}
            addButtonText="添加意图"
            emptyText="暂无意图，点上方常用意图芯片可一键插入"
            rowNamePrefix="意图"
            renderRow={/* renderRow 渲染当前列表行的编辑控件。 */ (row, index, update) => (
              <div className="space-y-2">
                <div className="flex flex-wrap items-center gap-3">
                  <input
                    value={row.id}
                    onChange={/* 输入变化更新意图 ID 草稿。 */ event => update({ id: event.target.value })}
                    placeholder="意图 ID，如 bargain"
                    aria-label={`第 ${index + 1} 行意图 ID`}
                    className="w-full sm:w-40 ios-input px-3 py-2 rounded-lg text-sm font-mono"
                  />
                  <label className="flex items-center gap-1.5 text-xs font-bold text-gray-600">
                    <input
                      type="checkbox"
                      checked={row.enabled}
                      onChange={/* 勾选变化更新意图启用开关。 */ event => update({ enabled: event.target.checked })}
                    />
                    启用
                  </label>
                  <label className="flex items-center gap-1.5 text-xs font-bold text-gray-600">
                    <input
                      type="checkbox"
                      checked={row.ai}
                      onChange={/* 勾选变化更新意图 AI 接管开关。 */ event => update({ ai: event.target.checked })}
                    />
                    AI 接管
                  </label>
                </div>
                <input
                  value={row.match}
                  onChange={/* 输入变化更新意图匹配规则。 */ event => update({ match: event.target.value })}
                  placeholder="匹配规则，如 (便宜|优惠) & (元|块)"
                  aria-label={`第 ${index + 1} 行匹配规则`}
                  className="w-full ios-input px-3 py-2 rounded-lg text-sm font-mono"
                />
              </div>
            )}
          />
        </div>
        <div className="ios-card rounded-xl p-5 bg-white space-y-2">
          <label className="block text-sm font-bold text-gray-800">负向词</label>
          <input
            value={negative}
            onChange={/* 输入变化更新负向组合词草稿。 */ event => applyKnown({ negative: event.target.value })}
            placeholder={NEGATIVE_RECOMMENDED}
            aria-label="负向词"
            className="w-full ios-input px-4 py-3 rounded-xl text-sm"
          />
          <p className="text-xs text-gray-500">多词用 <code className="font-mono">|</code> 分隔，命中负向词的消息一律不交给 AI</p>
        </div>
      </div>
    </ConfigBlock>
  );
};
