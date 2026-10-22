// color-baseline 是调色板类门禁的扫描器与基线维护工具：
// 正则与扫描逻辑集中在此，colorTokens.test.ts 直接 import 复用，避免脚本与测试两处口径漂移。
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";

// FRONTEND_ROOT 是 frontend 目录；脚本与基线 JSON 都相对它定位，保证在任意工作目录下执行结果一致。
export const FRONTEND_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

// PALETTE_CLASS_SOURCE 匹配 Tailwind 调色板类：前缀允许 hover:/md: 等变体与任意 utility 名，
// 色板只收录非中性族。中性族（gray/slate/zinc/neutral/stone）与语义令牌
// （brand/danger-*/success-*/warning-*/ink/canvas）不在其中。
export const PALETTE_CLASS_SOURCE = String.raw`\b[a-z][a-z0-9]*(?:-[a-z0-9]+)*-(?:blue|sky|violet|indigo|emerald|amber|green|red|purple|rose|orange|teal|cyan|lime|fuchsia|pink)-\d{2,3}\b`;

// PALETTE_CLASS_FLAGS 使用全局匹配，便于通过 matchAll 统计同一文件内的全部命中。
export const PALETTE_CLASS_FLAGS = "g";

// PALETTE_SCAN_ROOTS 是门禁扫描的源码根，与 Tailwind 生产扫描的 feature/shared 目录保持一致。
export const PALETTE_SCAN_ROOTS = ["app/features", "shared/ui"];

// PALETTE_EXCLUDED_FILES 是唯一允许持有调色板类字的语义令牌文件，不参与门禁统计。
export const PALETTE_EXCLUDED_FILES = ["shared/ui/tokens.ts"];

// PALETTE_SOURCE_EXTENSIONS 是参与统计的扩展名：TSX 承载组件，TS 承载逻辑层拼接出的类名。
export const PALETTE_SOURCE_EXTENSIONS = [".tsx", ".ts"];

// PALETTE_BASELINE_FILE 是基线文件相对 frontend 根目录的路径。
export const PALETTE_BASELINE_FILE = "colorPaletteBaseline.json";

// createPaletteClassPattern 每次返回全新的正则实例，避免共享实例的 lastIndex 在多次扫描间互相污染。
export function createPaletteClassPattern() {
  return new RegExp(PALETTE_CLASS_SOURCE, PALETTE_CLASS_FLAGS);
}

// countPaletteClasses 统计一段源码文本中非中性调色板类的出现次数。
export function countPaletteClasses(sourceText) {
  return [...sourceText.matchAll(createPaletteClassPattern())].length;
}

// isPaletteSourceFile 判断文件名是否属于门禁扫描范围：排除测试、声明文件与语义令牌层。
export function isPaletteSourceFile(relativePath) {
  if (PALETTE_EXCLUDED_FILES.includes(relativePath)) {
    return false;
  }
  if (/\.test\.tsx?$/u.test(relativePath) || /\.d\.ts$/u.test(relativePath)) {
    return false;
  }
  return PALETTE_SOURCE_EXTENSIONS.some((extension) => relativePath.endsWith(extension));
}

// collectPaletteSources 递归收集扫描根下的非测试 TS/TSX 源文件，返回相对 frontend 根目录的稳定路径。
export function collectPaletteSources(rootDirectory = FRONTEND_ROOT) {
  // relativePaths 保存跨平台稳定的源码相对路径。
  const relativePaths = [];
  // visit 深度遍历单个目录，跳过依赖与构建产物。
  function visit(directory) {
    // entries 是当前目录的文件系统条目。
    const entries = fs.readdirSync(directory, { withFileTypes: true });
    for (const entry of entries) {
      if (entry.isDirectory()) {
        if (["node_modules", "dist", "coverage"].includes(entry.name)) {
          continue;
        }
        visit(path.join(directory, entry.name));
        continue;
      }
      // relativePath 是当前文件相对 frontend 根目录的路径。
      const relativePath = path.relative(rootDirectory, path.join(directory, entry.name)).split(path.sep).join("/");
      if (isPaletteSourceFile(relativePath)) {
        relativePaths.push(relativePath);
      }
    }
  }
  for (const relativeRoot of PALETTE_SCAN_ROOTS) {
    visit(path.join(rootDirectory, relativeRoot));
  }
  return relativePaths.sort();
}

// scanPaletteUsage 扫描全部源文件并汇总计数：entries 只保留命中数大于 0 的文件。
export function scanPaletteUsage(rootDirectory = FRONTEND_ROOT) {
  // entries 是「相对路径 → 命中次数」的明细，路径按升序排列。
  const entries = collectPaletteSources(rootDirectory).map((relativePath) => ({
    file: relativePath,
    count: countPaletteClasses(fs.readFileSync(path.join(rootDirectory, relativePath), "utf8")),
  }));
  // hitEntries 只保留确有调色板类的文件，未命中的文件不进入基线。
  const hitEntries = entries.filter((entry) => entry.count > 0);
  // sumByExtension 汇总指定扩展名的文件数与命中数，用于报告 .ts 与 .tsx 各自占比。
  const sumByExtension = (extension) => hitEntries
    .filter((entry) => entry.file.endsWith(extension))
    .reduce((accumulator, entry) => ({ files: accumulator.files + 1, hits: accumulator.hits + entry.count }), { files: 0, hits: 0 });
  return {
    entries: hitEntries,
    scannedFiles: entries.length,
    totalFiles: hitEntries.length,
    totalHits: hitEntries.reduce((accumulator, entry) => accumulator + entry.count, 0),
    tsx: sumByExtension(".tsx"),
    ts: sumByExtension(".ts"),
  };
}

// readPaletteBaseline 读取冻结基线，文件缺失时视为空基线，避免首次接入时脚本直接崩溃。
export function readPaletteBaseline(rootDirectory = FRONTEND_ROOT) {
  // baselinePath 是基线 JSON 的绝对路径。
  const baselinePath = path.join(rootDirectory, PALETTE_BASELINE_FILE);
  if (!fs.existsSync(baselinePath)) {
    return {};
  }
  return JSON.parse(fs.readFileSync(baselinePath, "utf8"));
}

// formatBaseline 把扫描结果写成按路径升序排列的基线文本，末尾保留换行以便 diff 稳定。
function formatBaseline(entries) {
  // sortedEntries 按路径的字符编码升序排列，保证同一份源码永远生成相同文本，也便于人工 diff。
  const sortedEntries = [...entries].sort((left, right) => (left.file < right.file ? -1 : left.file > right.file ? 1 : 0));
  // lines 是 JSON 的逐条文本行。
  const lines = sortedEntries.map((entry) => `  ${JSON.stringify(entry.file)}: ${entry.count}`);
  return `{\n${lines.join(",\n")}\n}\n`;
}

// writePaletteBaseline 用当前扫描结果重写基线文件，只保留命中数大于 0 的文件。
export function writePaletteBaseline(rootDirectory = FRONTEND_ROOT, usage = scanPaletteUsage(rootDirectory)) {
  // baselinePath 是待写入的基线文件路径。
  const baselinePath = path.join(rootDirectory, PALETTE_BASELINE_FILE);
  fs.writeFileSync(baselinePath, formatBaseline(usage.entries), "utf8");
  return baselinePath;
}

// collectRegressions 找出实际命中数高于基线的文件，这些文件说明有代码新增了调色板类。
export function collectRegressions(usage, baseline) {
  return usage.entries
    .filter((entry) => entry.count > (baseline[entry.file] ?? 0))
    .map((entry) => ({ file: entry.file, count: entry.count, baseline: baseline[entry.file] ?? 0 }));
}

// collectShrunken 找出实际命中数低于基线的文件，仅作可下调提示，不参与门禁失败。
export function collectShrunken(usage, baseline) {
  return usage.entries
    .filter((entry) => entry.count < (baseline[entry.file] ?? 0))
    .map((entry) => ({ file: entry.file, count: entry.count, baseline: baseline[entry.file] ?? 0 }));
}

// printUsageSummary 输出总文件数、总处数与 .ts/.tsx 分布，脚本与门禁共用同一份统计口径。
export function printUsageSummary(usage) {
  console.log(`color-baseline: 扫描 ${usage.scannedFiles} 个源文件，${usage.totalFiles} 个文件命中 ${usage.totalHits} 处调色板类`);
  console.log(`color-baseline: .tsx ${usage.tsx.files} 个文件 ${usage.tsx.hits} 处；.ts ${usage.ts.files} 个文件 ${usage.ts.hits} 处`);
}

// main 解析命令行开关：--write 重写基线，--check（默认）只做只读校验。
function main(argv) {
  // mode 是本次运行模式，缺省按门禁校验处理。
  const mode = argv.includes("--write") ? "write" : "check";
  // usage 是本次扫描的完整统计结果。
  const usage = scanPaletteUsage();
  printUsageSummary(usage);
  if (mode === "write") {
    // baselinePath 是本次被重写的基线文件路径。
    const baselinePath = writePaletteBaseline(FRONTEND_ROOT, usage);
    console.log(`color-baseline: 已重写基线 ${path.relative(FRONTEND_ROOT, baselinePath)}`);
    return;
  }
  // baseline 是冻结的基线内容。
  const baseline = readPaletteBaseline();
  // regressions 是需要开发者整改的新增调色板类。
  const regressions = collectRegressions(usage, baseline);
  // shrunken 是可以手工下调基线的文件，只提示不失败。
  const shrunken = collectShrunken(usage, baseline);
  for (const item of shrunken) {
    console.log(`color-baseline: 可下调基线 ${item.file}（实际 ${item.count} 处，基线 ${item.baseline} 处）`);
  }
  if (regressions.length > 0) {
    for (const item of regressions) {
      console.error(`color-baseline: 超出基线 ${item.file}（实际 ${item.count} 处，基线 ${item.baseline} 处）`);
    }
    console.error("color-baseline: 请改用语义令牌（brand/success-*/warning-*/danger-*/ink/canvas 或 shared/ui/tokens.ts），禁止抬高基线");
    process.exitCode = 1;
    return;
  }
  console.log("color-baseline: 通过（没有文件新增调色板类）");
}

// 仅在直接执行脚本时运行主流程，被测试 import 时保持纯函数导出。
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main(process.argv.slice(2));
}
