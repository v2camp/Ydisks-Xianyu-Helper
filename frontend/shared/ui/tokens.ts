// tokens.ts 是共享 UI 原语唯一的语义色来源：组件只引用此处导出的类型与类名映射，
// 不再各自维护色值表。调色板类名要参与 Tailwind 编译期扫描，因此必须保留完整字面量，
// 不能按字符串拼接，否则对应 utility 会被裁剪掉。

// SectionTone 是区块标题的语义色板：颜色对应业务含义，避免各页面随手取色。
export type SectionTone = 'brand' | 'violet' | 'emerald' | 'indigo' | 'slate' | 'amber';

// SECTION_TONE_STYLES 把区块语义色映射到图标槽底色，取值与设计文档 §4.3 的色板一一对应。
export const SECTION_TONE_STYLES: Record<SectionTone, string> = {
  brand: 'bg-brand',
  violet: 'bg-violet-500',
  emerald: 'bg-emerald-500',
  indigo: 'bg-indigo-500',
  slate: 'bg-slate-700',
  amber: 'bg-amber-500',
};

// StatusBadgeTone 是徽章的语义色：ready 已就绪、missing 未配置、loading 读取中、warning 合法默认（如回落内置）。
export type StatusBadgeTone = 'ready' | 'missing' | 'loading' | 'warning';

// STATUS_BADGE_STYLES 把徽章语义色映射到类名：就绪用成功色，回落内置用琥珀弱化，
// 读取与未配置保持中性，避免把「尚未读取」误显示成异常。
export const STATUS_BADGE_STYLES: Record<StatusBadgeTone, string> = {
  ready: 'bg-success-100 text-success-700',
  missing: 'bg-gray-100 text-gray-500',
  loading: 'bg-gray-100 text-gray-500',
  warning: 'bg-warning-100 text-warning-700',
};

// SaveBarState 是保存条的四态：空闲待提交、保存中、保存失败、保存成功。
export type SaveBarState = 'idle' | 'saving' | 'error' | 'success';

// SAVE_BAR_STYLES 把保存条状态映射到条身底色：失败与成功用语义色强调结果，
// 空闲与保存中保持深色底承托主按钮，两者同色以免状态切换时出现无意义的明暗跳动。
export const SAVE_BAR_STYLES: Record<SaveBarState, string> = {
  idle: 'bg-slate-800',
  saving: 'bg-slate-800',
  error: 'bg-danger-500',
  success: 'bg-success-500',
};
