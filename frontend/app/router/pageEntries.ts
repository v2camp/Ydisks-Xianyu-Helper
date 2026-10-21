// 页面清单的唯一来源：架构边界测试、路由懒加载测试与分片预算测试都从这里取清单，
// 避免同一份页面在多个测试文件里各写一遍而产生缺漏。

// pageEntryFiles 是全部页面入口文件，包含认证页 SessionGate。
// 认证页由路由壳同步装配并负责登录态分流，不产生独立动态分片，因此不进入 lazyPageNames。
export const pageEntryFiles: readonly string[] = [
  'app/features/accounts/pages/AccountList.tsx',
  'app/features/cards/pages/CardList.tsx',
  'app/features/chat/pages/Chat.tsx',
  'app/features/dashboard/pages/Dashboard.tsx',
  'app/features/delivery-templates/pages/DeliveryTemplates.tsx',
  'app/features/items/pages/ItemList.tsx',
  'app/features/connectors/pages/Connectors.tsx',
  'app/features/orders/pages/OrderList.tsx',
  'app/features/rules/pages/Rules.tsx',
  'app/features/session/pages/SessionGate.tsx',
  'app/features/settings/pages/AgentCenter.tsx',
  'app/features/settings/pages/AISettings.tsx',
  'app/features/settings/pages/Settings.tsx',
];

// lazyPageNames 是由 AuthenticatedShell 懒加载并各自产出独立分片的页面名，顺序与路由壳声明一致。
// 这里刻意不按“大写字母前缀”自动识别分片：商品页的 PublishSpecsEditor 与 PublishImagesEditor 也是懒加载组件，
// 但它们属于页面内部组件而非页面，自动识别会把它们误判为页面并让预算与计数断言失真。
export const lazyPageNames: readonly string[] = [
  'Dashboard',
  'AccountList',
  'OrderList',
  'CardList',
  'ItemList',
  'Settings',
  'AISettings',
  'AgentCenter',
  'Rules',
  'DeliveryTemplates',
  'Connectors',
  'Chat',
];
