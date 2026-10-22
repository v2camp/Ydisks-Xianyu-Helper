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

// ChipTone 是列表标签的语义色：ai 由模型生成、review 待人工复核、polish 建议润色、
// delivery 交付相关、confirm 待确认、exempt 豁免（无需处理）。
export type ChipTone = 'ai' | 'review' | 'polish' | 'delivery' | 'confirm' | 'exempt';

// CHIP_TONE_STYLES 把标签语义映射到「浅底 + 同族深字」：同一族取 100 底色配 700 字色保证对比度，
// 豁免态用中性灰弱化，避免把「无需处理」误读成告警。
export const CHIP_TONE_STYLES: Record<ChipTone, string> = {
  ai: 'bg-accent-100 text-accent-700',
  review: 'bg-success-100 text-success-700',
  polish: 'bg-warning-100 text-warning-700',
  delivery: 'bg-brand-100 text-brand-700',
  confirm: 'bg-info-100 text-info-700',
  exempt: 'bg-gray-100 text-gray-500',
};

// StatusTone 是行内状态的语义色：ready 可执行、info 仅提示、warning 需留意、danger 需处置、neutral 中性无结论。
export type StatusTone = 'ready' | 'info' | 'warning' | 'danger' | 'neutral';

// STATUS_TONE_STYLES 把状态语义映射到「浅底 + 同族深字」，与 CHIP_TONE_STYLES 共用同一套明度关系，
// 使标签与状态在并排展示时不会出现粗细、深浅不一致的视觉噪音。
export const STATUS_TONE_STYLES: Record<StatusTone, string> = {
  ready: 'bg-success-100 text-success-700',
  info: 'bg-brand-100 text-brand-700',
  warning: 'bg-warning-100 text-warning-700',
  danger: 'bg-danger-100 text-danger-700',
  neutral: 'bg-gray-100 text-gray-500',
};
