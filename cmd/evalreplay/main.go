// evalreplay 发布前真实模型评测：读项目 .env 配置，回放缺陷样本库，产出分层报告与门禁结论。
//
// 用法：
//
//	cd <项目根>
//	go run ./cmd/evalreplay                    # 读 .env 配置，真实评测并判定门禁
//	go run ./cmd/evalreplay -dry-run           # 自检：不调用模型，用历史回复验证链路与样本库
//	go run ./cmd/evalreplay -update-baseline   # 评测并把本次结果记录为新基线
//	go run ./cmd/evalreplay -limit 5 -cases 3  # 只跑 5 条，每条采样 3 次取多数
//
// 模型配置优先级：.env 中的 EVAL_* 覆盖项 > 数据库当前生效配置（只读解密）。
// 本命令只读：不写业务表、不迁移、不修改生产配置；报告只出现模型名与收敛后的地址。
// 退出码：0 门禁通过；1 门禁未通过；2 参数或运行错误。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/evalharness"
	"xianyu-go/internal/logsafe"
)

// envPassthroughKeys 是需要从 .env 注入进程环境的键。
// 只注入评测链路真正消费的项，避免 .env 的其它配置意外改变本进程行为。
var envPassthroughKeys = []string{"DATABASE_URL", "XIANYU_DATA_KEY"}

// main 是评测 CLI 的进程入口。
func main() {
	// root 是项目根目录，.env 与默认产物路径都相对它解析。
	root := flag.String("root", ".", "项目根目录（含 .env）")
	// samplesPath 覆盖样本库路径。
	samplesPath := flag.String("samples", "", "样本库路径（默认 data/evalexport/samples.jsonl）")
	// baselinePath 覆盖基线路径。
	baselinePath := flag.String("baseline", "", "基线路径（默认 data/evalexport/baseline.json）")
	// outDir 覆盖报告输出目录。
	outDir := flag.String("out", "", "报告输出目录")
	// cases 覆盖每个样本的采样次数。
	cases := flag.Int("cases", 0, "每个样本的采样次数（0 表示用默认值）")
	// tolerance 覆盖劣化容差。
	tolerance := flag.Float64("tolerance", -1, "允许的通过率劣化容差（0.05 表示 5 个百分点）")
	// limit 限制参与回放的样本数。
	limit := flag.Int("limit", 0, "最多回放的样本数（0 表示不限制）")
	// updateBaseline 表示评测后把本次结果记录为新基线。
	updateBaseline := flag.Bool("update-baseline", false, "把本次结果记录为新基线")
	// dryRun 表示不调用模型，仅用样本历史回复自检链路。
	dryRun := flag.Bool("dry-run", false, "自检模式：不调用模型，用样本历史回复验证判定链路")
	flag.Parse()

	os.Exit(run(opts{
		root:           *root,
		samplesPath:    *samplesPath,
		baselinePath:   *baselinePath,
		outDir:         *outDir,
		cases:          *cases,
		tolerance:      *tolerance,
		limit:          *limit,
		updateBaseline: *updateBaseline,
		dryRun:         *dryRun,
	}))
}

// opts 是本命令的全部命令行选项。
type opts struct {
	// root 是项目根目录。
	root string
	// samplesPath 是样本库路径覆盖项。
	samplesPath string
	// baselinePath 是基线路径覆盖项。
	baselinePath string
	// outDir 是输出目录覆盖项。
	outDir string
	// cases 是采样次数覆盖项。
	cases int
	// tolerance 是容差覆盖项。
	tolerance float64
	// limit 是样本数上限。
	limit int
	// updateBaseline 表示评测后更新基线。
	updateBaseline bool
	// dryRun 表示自检模式。
	dryRun bool
}

// run 执行评测主流程并返回退出码。
func run(o opts) int {
	// ctx 是评测总上下文；真实模型逐条调用，给足总预算。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// values 是项目 .env 的解析结果；envErr 是读取失败原因。
	values, envErr := evalharness.LoadDotenv(o.root)
	if envErr != nil {
		fmt.Printf("读取 .env 失败: %s\n", logsafe.Error(envErr))
		return 2
	}
	// 把白名单键注入进程环境，数据库解密密钥必须经此注入才能生效。
	applyDotenv(values)

	// cfg 是运行配置：先套默认值，再叠加 .env 配置，最后合并命令行覆盖。
	cfg := evalharness.DefaultConfig()
	cfg.SamplesPerCase = evalharness.EnvInt(values, cfg.SamplesPerCase, "EVAL_SAMPLES_PER_CASE")
	cfg.Tolerance = evalharness.EnvFloat(values, cfg.Tolerance, "EVAL_TOLERANCE")
	// err 是套用命令行覆盖项时的参数校验失败原因。
	if err := cfg.ApplyOverrides(evalharness.Overrides{
		SamplesPath:    o.samplesPath,
		BaselinePath:   o.baselinePath,
		OutputDir:      o.outDir,
		SamplesPerCase: o.cases,
		Tolerance:      o.tolerance,
	}); err != nil {
		fmt.Printf("参数错误: %v\n", err)
		return 2
	}
	cfg.SamplesPath = resolvePath(o.root, cfg.SamplesPath)
	cfg.BaselinePath = resolvePath(o.root, cfg.BaselinePath)
	cfg.OutputDir = resolvePath(o.root, cfg.OutputDir)

	// genCfg 是被测模型配置；judgeCfg 是判定模型配置，两者在两个分支中分别确定。
	var genCfg evalharness.ModelConfig
	// judgeCfg 先声明，具体取值取决于自检模式还是真实评测。
	var judgeCfg evalharness.ModelConfig
	if o.dryRun {
		genCfg = evalharness.ModelConfig{Model: "dry-run", Source: "dry-run"}
		judgeCfg = evalharness.ModelConfig{Model: "dry-run", Source: "dry-run"}
		fmt.Println("自检模式：不调用任何模型，仅用样本历史回复验证判定链路")
	} else {
		// store 是只读数据库连接，仅在需要读取数据库模型配置时建立。
		store, closeStore, storeErr := openReadOnlyStore(ctx, values)
		if storeErr != nil {
			fmt.Printf("准备模型配置失败: %s\n", logsafe.Error(storeErr))
			return 2
		}
		if closeStore != nil {
			defer closeStore()
		}
		// resolved、resolveErr 是本次解析出的被测模型配置与失败原因。
		resolved, resolveErr := evalharness.ResolveGeneratorConfig(ctx, store, values)
		if resolveErr != nil {
			fmt.Printf("解析被测模型配置失败: %s\n", logsafe.Error(resolveErr))
			return 2
		}
		genCfg = resolved
		judgeCfg = evalharness.ResolveJudgeConfig(values, genCfg)
	}

	// all 是样本库全量内容。
	all, err := evalharness.LoadSamples(cfg.SamplesPath)
	if err != nil {
		fmt.Printf("加载样本库失败: %s\n", logsafe.Error(err))
		return 2
	}
	// playable、skipped 是可回放样本与因缺少快照被跳过的数量。
	playable, skipped, err := evalharness.ValidateSamples(all)
	if err != nil {
		fmt.Printf("%s\n", logsafe.Error(err))
		return 2
	}

	// results 是逐条评测结果。
	results := evalharness.Run(ctx, playable, nil, genCfg, judgeCfg, evalharness.RunOptions{
		SamplesPerCase: cfg.SamplesPerCase,
		DryRun:         o.dryRun,
		Limit:          o.limit,
	})

	// baseline 是历史基线；首次运行为 nil。
	baseline, err := evalharness.LoadBaseline(cfg.BaselinePath)
	if err != nil {
		fmt.Printf("加载基线失败: %s\n", logsafe.Error(err))
		return 2
	}

	// rep 是汇总后的评测报告。
	rep := evalharness.ComputeReport(results, baseline, genCfg.Model, judgeCfg.Model, genCfg.Source, cfg.SamplesPerCase, skipped, cfg.Tolerance)

	// mdPath、jsonPath、err 是两个落盘产物的路径与写出失败原因。
	mdPath, jsonPath, err := evalharness.WriteArtifacts(cfg.OutputDir, rep)
	if err != nil {
		fmt.Printf("写出报告失败: %s\n", logsafe.Error(err))
		return 2
	}

	if o.updateBaseline {
		// err 是写入基线文件的失败原因。
		if err := evalharness.SaveBaseline(cfg.BaselinePath, rep.ToBaseline(cfg.Tolerance)); err != nil {
			fmt.Printf("更新基线失败: %s\n", logsafe.Error(err))
			return 2
		}
		fmt.Printf("已更新基线: %s\n", cfg.BaselinePath)
	}

	fmt.Printf("被测模型 %s（来源 %s），判定模型 %s\n", rep.Model, rep.ModelSource, rep.JudgeModel)
	fmt.Printf("通过率 %.1f%%（%d/%d），跳过 %d，判定失败 %d\n", rep.PassRate*100, rep.Passed, rep.Total, rep.Skipped, rep.JudgeFailures)
	if rep.GatePassed {
		fmt.Println("门禁：通过")
	} else {
		fmt.Println("门禁：未通过")
	}
	// reason 是当前遍历到的门禁说明。
	for _, reason := range rep.GateReasons {
		fmt.Printf("  - %s\n", reason)
	}
	fmt.Printf("报告：%s\n指标：%s\n", mdPath, jsonPath)

	if !rep.GatePassed {
		return 1
	}
	return 0
}

// openReadOnlyStore 在需要读取数据库模型配置时建立只读连接。
// 返回的关闭函数为 nil 表示不需要连接（.env 已提供完整覆盖项）。
func openReadOnlyStore(ctx context.Context, values map[string]string) (*db.Store, func(), error) {
	// apiKey 是 .env 提供的密钥覆盖项。
	apiKey := evalharness.EnvValue(values, "EVAL_API_KEY")
	// model 是 .env 提供的模型覆盖项。
	model := evalharness.EnvValue(values, "EVAL_MODEL")
	if apiKey != "" && model != "" {
		return nil, nil, nil
	}
	// dsn 是数据库连接串。
	dsn := evalharness.EnvValue(values, "DATABASE_URL")
	if dsn == "" {
		return nil, nil, fmt.Errorf("未配置 DATABASE_URL，且 .env 未提供完整的 EVAL_API_KEY/EVAL_MODEL")
	}
	// database、dialect、err 是只读连接池、方言与失败原因。
	database, dialect, err := db.OpenReadOnly(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	return db.NewStore(database, dialect), func() { _ = database.Close() }, nil
}

// applyDotenv 把 .env 中白名单键注入进程环境，已存在的真实环境变量优先。
// 数据库解密密钥必须经此注入，否则敏感设置读取会失败。
func applyDotenv(values map[string]string) {
	// key 是当前遍历到的待注入键名。
	for _, key := range envPassthroughKeys {
		if os.Getenv(key) != "" {
			continue
		}
		// value 是该键在 .env 中的取值。
		if value := values[key]; value != "" {
			_ = os.Setenv(key, value)
		}
	}
}

// resolvePath 把相对路径按项目根目录展开；已是绝对路径时原样返回。
func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
