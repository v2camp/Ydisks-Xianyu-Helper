-- =============================================================================
-- docs/sql/ai-enabled-zero-audit.sql
-- 诊断对象：ai_reply_settings.ai_enabled = 0 的行（E1 语义反转后的「残余关闭清单」）
-- 方言：SQLite 3.8+ 与 PostgreSQL 12+ 可直接执行（未使用任何方言专属函数）
-- =============================================================================
--
-- 【一、只读声明】
--   本文件只包含 SELECT 与 WITH ... SELECT，不含任何写入语句：
--   不改 ai_reply_settings、不写 ai_decision_log、不产生 security_audit_logs。
--   执行它不改变任何一行，也不会触发应用层的敏感读审计（它不经过 Settings.Get）。
--   本文件已按只读方式在一个临时 SQLite 副本上实跑核对语法（本机数据目录内没有库文件），
--   未改变任何一行，记录见文末【实跑记录】。
--
-- 【二、必读前提：reason 计数不是分类判据，只作旁证列】
--   ★ account-off / master-off 是 E1 之后才存在的 reason 取值。
--   ★ 语义反转之前，ai.go 的闸门是「账号未启用就直接 return，不写任何日志」，
--     所以历史库里不可能存在 account-off 记录。
--   ★ 因此本文件**不把 ai_decision_log.reason 的任何计数作为 A/B/C 的分类判据**，
--     它只出现在查询 2 的旁证列里，用于确认「这条 0 在反转后是否真的在拦消息」。
--   ★ 分类判据是静态结构指纹（created_at = updated_at + 有无 agent_* 痕迹），
--     即「这一行只可能由哪条写入通路建出来」，与日志无关。
--   ★ 反过来说：查不到 account-off 绝不等于「用户从未关闭过」，详见【六】L1。
--
-- 【三、这条 0 为什么值得审】
--   E1（提交 02132065）把开关语义反转为「无行 = 跟随全局总闸 ai_master_enabled（默认开）」。
--   反转后 生效值 = 总闸 && (无行 || ai_enabled = 1)，因此一个 ai_enabled = 0 的行是
--   唯一还能把单个账号压在关闭状态的数据。反转前「无行」本来就等于关闭，这类 0 值在
--   当时不产生任何可见差异，反转后才变成显性问题。
--
--   全仓只有两条写入 ai_reply_settings 的通路（按代码确认，非推测）：
--     P1 设置通路 internal/db/ai_reply.go UpsertSettings
--        一定写 ai_enabled（取提交值），从不写 agent_preset / agent_enabled。
--     P2 档位通路 internal/db/agent_config.go SetAgentConfig
--        旧版（8d6c653a，2026-10-10 17:38 +0800 引入）INSERT 列表里没有 ai_enabled，
--        落库值来自建表默认 0；冲突分支也不改该列。
--        E9 已把 INSERT 改成显式写 1，但**不修存量数据**。
--
--   推论：一行 ai_enabled = 0 且带 agent_* 痕迹，只可能由 P2 建出 —— 那个 0 是列默认值，
--   不是任何人的表态。这是本文件判定「疑似误伤」的结构性依据。
--
-- 【四、结论口径（三分类）】
--   A 疑似用户显式关闭：ai_enabled = 0，created_at = updated_at，且无 agent_* 痕迹。
--     只有 P1 能建出这种形状，而 P1 建行写入的 0 来自设置表单提交值。
--     仍可能混入「旧前端把未勾选直接当 0 提交」的情况，本库无法区分（见 L3）。
--   B 疑似档位通路误伤：ai_enabled = 0，created_at = updated_at，且 agent_preset 或
--     agent_enabled 非空。0 来自列默认值，与用户意志无关。
--   C 无法判定：created_at <> updated_at（行被二次写入过，静态特征失效），或时间列为 NULL。
--
-- 【五、ai_decision_log.reason 能证明什么、不能证明什么（【二】的展开）】
--   account-off / master-off 两个取值由 E1 引入。反转前 ai.go 的闸门是
--   `if err != nil || cfg == nil || !cfg.AIEnabled { return nil, nil }`，**不写日志**。
--   所以：
--     · 出现 account-off 只证明「E1 上线后闸门确实按账号级关闭拦下了消息」，
--       不证明「有人主动关过」。反转上线后，任何 ai_enabled = 0 的行只要收到消息就会
--       持续产生 account-off —— B 类行同样会产生。
--     · 没有 account-off 也不证明「从未关闭过」，至少有四种解释：
--       (1) 该账号反转后没收到过消息；
--       (2) 总闸关着，闸门在账号判定之前就以 master-off 短路；
--       (3) 相关行已超出保留窗口被清理；
--       (4) 反转前的写通道不会记录任何开关变更，本来就没有证据可留。
--   因此 reason 计数只作「运行期旁证」列呈现，不参与 A/B/C 判定。
--
-- 【六、局限】
--   L1 保留窗口：ai_decision_log 由 internal/automation/scheduler_retention.go 按
--      system_settings.log_retention_days（缺省 30 天，见 internal/db/retention.go 的
--      DefaultRetentionDays）清理，条件是 created_at < 当前时间 - N 天。
--      **查不到 ≠ 没发生**。查询 3 输出真实窗口，用它与 N 天对照即可判断覆盖范围。
--   L2 秒级精度（仅 SQLite）：CURRENT_TIMESTAMP 只到秒。若「先建档位、同一秒内又保存
--      设置」，created_at 仍等于 updated_at，B 类会把一次真实的显式关闭误收进来。
--      PostgreSQL 的 CURRENT_TIMESTAMP 到微秒，无此问题。反向误判（A 被判成 B）不会发生。
--   L3 旧前端提交语义：反转前的设置页若把「未勾选」直接当 false 提交，也会造出 A 类形状
--      的行。本库无法回溯提交内容，需要产品侧确认旧前端的提交行为。
--   L4 存量不可考：ai_reply_settings 只有 created_at / updated_at，没有写入来源列，也没有
--      变更历史表。A/B/C 全部是旁证推理而非事实判定；做任何数据修正前必须逐账号由 owner
--      确认，本文件不提供也不建议任何修正语句。
--   L5 NULL 行不在 `= 0` 的范围内：该列可空（00001 建表未加 NOT NULL），代码按
--      COALESCE(ai_enabled, 1) 视为开启。查询 4 单独列出，避免漏账。
--
-- 【七、执行方式（供人工复核时使用；本文件已按此方式只读实跑过一次）】
--   SQLite（本机分析库，读写同文件，务必带 -readonly）：
--     sqlite3 -readonly "$HOME/Library/Application Support/YdisksXianyuHelper/data/xianyu_data.db" \
--             < docs/sql/ai-enabled-zero-audit.sql
--     路径来源：cmd/server/main.go:62（默认 data/xianyu_data.db）、
--               main.go:63（userDataDirName = "YdisksXianyuHelper"）、main.go:615（os.UserConfigDir）。
--   PostgreSQL：
--     psql "$DATABASE_URL" -f docs/sql/ai-enabled-zero-audit.sql
--
-- 【八、方言差异（正文对应处另有注释，此处汇总一次）】
--   1) system_settings 的列名 key 是关键字：两侧统一写 "key"（双引号在两个方言里都是
--      标识符引用）。不要写裸 key，也不要用反引号（PostgreSQL 不认反引号）。
--   2) ai_enabled 两侧都按整数比较 `= 0`：SQLite 的 BOOLEAN 列实际存 0/1，PostgreSQL 的
--      同名列建表就是 INTEGER（00001_init.sql）。永远不要写 `= FALSE`，PostgreSQL 会因
--      布尔与整数不可比直接报错。
--   3) 时间一律不加减区间：SQLite 的 datetime('now','-30 days') 与 PostgreSQL 的
--      now() - interval '30 days' 不互认。本文件只输出原始时间戳，由人对照判断。
--   4) 聚合用 SUM(CASE WHEN ... THEN 1 ELSE 0 END)，不用 COUNT(*) FILTER：FILTER 需要
--      SQLite 3.30+ / PostgreSQL 9.4+，前者的下限不可控。
-- =============================================================================


-- -----------------------------------------------------------------------------
-- 查询 0：运行参数（后面所有结论的前置条件）
--   回答：总闸现在是开还是关？保留窗口配了几天？
--   读法：
--     · 没有 ai_master_enabled 行 = 总闸开启（默认开），这是最常见情形。
--     · 有行时按值判定：'' / true / 1 / yes / on / enabled（忽略大小写与首尾空白）
--       为开，其它值（含 false、0、no）为关，与 db.aiMasterEnabledFromValue 完全一致。
--     · 没有 log_retention_days 行 = 30 天。
--   注意：AI 总闸关闭时，本文件列出的所有 0 行都不构成「当前实际影响」，
--         应先把总闸恢复再评估。
-- -----------------------------------------------------------------------------
SELECT "key",
       value
  FROM system_settings
 WHERE "key" IN ('ai_master_enabled', 'log_retention_days')
 ORDER BY "key";


-- -----------------------------------------------------------------------------
-- 查询 1：全量开关盘点（含无行账号）
--   回答：每个账号当前的开关原值、生效值，以及行的时间形态。
--   读法：
--     · has_row = 0：没有显式行，跟随总闸。master_on = 1 时 effective_enabled = 1。
--     · effective_enabled = 0 且 has_row = 1：当前真实被压着关闭的账号 —— 这是要处置的集合。
--     · never_rewritten = 1：该行自建行后没被任何通路二次写过。
--     · master_on 由 LOWER(TRIM(value)) 归一后判定，与 Go 侧语义一致；
--       表达式只能写在子查询里再引用，因为 PostgreSQL 不允许同层 select 引用别名。
--   注意：nickname / remark 都不是凭据列，未触碰 cookies.value。
-- -----------------------------------------------------------------------------
WITH accounted AS (
    SELECT c.id AS cookie_id,
           COALESCE(NULLIF(c.nickname, ''), NULLIF(c.remark, ''), '') AS account_label,
           CASE WHEN s.cookie_id IS NULL THEN 0 ELSE 1 END AS has_row,
           s.ai_enabled AS ai_enabled_raw,
           s.agent_preset AS agent_preset,
           s.agent_enabled AS agent_enabled,
           s.created_at AS row_created_at,
           s.updated_at AS row_updated_at,
           CASE
               WHEN LOWER(TRIM(COALESCE(m.value, ''))) IN ('', 'true', '1', 'yes', 'on', 'enabled')
                   THEN 1
               ELSE 0
           END AS master_on
      FROM cookies c
      LEFT JOIN ai_reply_settings s ON s.cookie_id = c.id
      LEFT JOIN system_settings m ON m."key" = 'ai_master_enabled'
)
SELECT cookie_id,
       account_label,
       has_row,
       ai_enabled_raw,
       agent_preset,
       agent_enabled,
       row_created_at,
       row_updated_at,
       CASE
           WHEN row_created_at IS NOT NULL AND row_created_at = row_updated_at THEN 1
           ELSE 0
       END AS never_rewritten,
       master_on,
       CASE
           WHEN master_on = 1 AND COALESCE(ai_enabled_raw, 1) <> 0 THEN 1
           ELSE 0
       END AS effective_enabled
  FROM accounted
 ORDER BY has_row DESC, COALESCE(ai_enabled_raw, -1) ASC, cookie_id;


-- -----------------------------------------------------------------------------
-- 查询 2：主体 —— ai_enabled = 0 行的三分类 + 运行期旁证
--   回答：每一行 0 是「用户显式关闭」「疑似档位通路误伤」还是「无法判定」，
--         以及它在 ai_decision_log 里留下了多少 account-off / master-off。
--   读法：
--     · verdict 的判定只看静态形状，不看 reason（理由见文件头【二】与【五】）。
--       B = 0 来自列默认值，属于需要人工确认后回写的候选；
--       A = 来自设置通路提交值，属于用户表态，原则上不要动；
--       C = 形状不足以判断，必须看 owner 记忆或确认旧前端行为。
--     · with_account_off 只说明「E1 上线后该账号确实被按账号级拦过消息」，
--       不说明谁关的；B 类行也会是 1。
--     · decision_rows = 0 表示反转后该账号没有任何决策日志：可能没流量，也可能
--       超出保留窗口（对照查询 3 的窗口下界）。
--     · first_account_off_at 用于和上线时间比对：晚于上线时间不足以证明反转前有人关闭。
-- -----------------------------------------------------------------------------
WITH decision AS (
    SELECT cookie_id,
           COUNT(*) AS decision_rows,
           SUM(CASE WHEN reason = 'account-off' THEN 1 ELSE 0 END) AS account_off_rows,
           SUM(CASE WHEN reason = 'master-off'  THEN 1 ELSE 0 END) AS master_off_rows,
           MIN(CASE WHEN reason = 'account-off' THEN created_at END) AS first_account_off_at,
           MAX(created_at) AS last_decision_at
      FROM ai_decision_log
     GROUP BY cookie_id
)
SELECT s.cookie_id,
       COALESCE(NULLIF(c.nickname, ''), NULLIF(c.remark, ''), '') AS account_label,
       s.ai_enabled,
       s.agent_preset,
       s.agent_enabled,
       s.created_at,
       s.updated_at,
       CASE
           WHEN s.created_at IS NOT NULL AND s.created_at = s.updated_at
                AND (s.agent_preset IS NOT NULL OR s.agent_enabled IS NOT NULL) THEN 'B-疑似档位通路误伤'
           WHEN s.created_at IS NOT NULL AND s.created_at = s.updated_at THEN 'A-疑似用户显式关闭'
           ELSE 'C-无法判定'
       END AS verdict,
       COALESCE(d.decision_rows, 0) AS decision_rows,
       COALESCE(d.account_off_rows, 0) AS account_off_rows,
       COALESCE(d.master_off_rows, 0) AS master_off_rows,
       d.first_account_off_at,
       d.last_decision_at
  FROM ai_reply_settings s
  LEFT JOIN cookies c ON c.id = s.cookie_id
  LEFT JOIN decision d ON d.cookie_id = s.cookie_id
 WHERE s.ai_enabled = 0
 ORDER BY verdict, s.cookie_id;


-- -----------------------------------------------------------------------------
-- 查询 3：日志覆盖窗口 + reason 取值分布
--   回答：ai_decision_log 实际能回溯多久？reason 词表当前有哪些取值、各出现多少次？
--   读法：
--     · 覆盖窗口 = 本表 first_seen_at 的最小值 ~ last_seen_at 的最大值。
--       把它与查询 0 的 log_retention_days（缺省 30 天）对照：窗口明显短于 N 天，
--       说明日志不足以支撑「从未关闭过」这类反证。
--     · 没有 account-off 行 = 反转后从没有任何账号被按账号级拦下，
--       这时查询 2 的旁证列全为 0，等价于「完全没有运行期证据」，不要读成「没有关闭过」。
--     · 出现清单之外的 reason（既不是 taken / no-intent / negative / extra-negative /
--       item-sync-requested / item-sync-failed / master-off / account-off）= 词表漂移，
--       需对齐 internal/engine/ai_reason.go。
-- -----------------------------------------------------------------------------
SELECT reason,
       COUNT(*) AS rows_count,
       MIN(created_at) AS first_seen_at,
       MAX(created_at) AS last_seen_at
  FROM ai_decision_log
 GROUP BY reason
 ORDER BY rows_count DESC;


-- -----------------------------------------------------------------------------
-- 查询 4：隐性状态 —— ai_enabled 为 NULL 的行
--   回答：是否还有「列值为 NULL」的行，它们不在 `= 0` 的筛选范围内。
--   读法：
--     · 代码口径是 COALESCE(ai_enabled, 1)，NULL 等于「开」，与「无行」等价。
--       这类行不是本次处置对象，但会让「有行 = 用户设置过」的直觉失效，
--       排查 source=account / source=global 时容易看错。
--     · 结果集为空是正常状态，说明所有行都带明确 0/1。
-- -----------------------------------------------------------------------------
SELECT s.cookie_id,
       COALESCE(NULLIF(c.nickname, ''), NULLIF(c.remark, ''), '') AS account_label,
       s.ai_enabled,
       s.agent_preset,
       s.created_at,
       s.updated_at
  FROM ai_reply_settings s
  LEFT JOIN cookies c ON c.id = s.cookie_id
 WHERE s.ai_enabled IS NULL
 ORDER BY s.cookie_id;


-- =============================================================================
-- 【实跑记录】2026-11-01
--
--   1) 本机数据目录 ~/Library/Application Support/YdisksXianyuHelper 下只有 data-key，
--      没有 data/xianyu_data.db；find $HOME -maxdepth 5 -name xianyu_data.db 也为空。
--      因此本次**没有可用的真实样本**，未取得任何真实分布数字。
--
--   2) 语法与分类逻辑用一个临时 SQLite 库验证：表结构照抄 00001 / 00034 / 00059 / 00064
--      的 sqlite 迁移，数据全部虚构，覆盖六种行形状（无行 / 显式关闭 / 档位误伤 /
--      二次写入 / 启用 / NULL）与五种 reason（含一个词表外取值用于验证漂移检测）。
--
--   3) 5 条查询全部执行成功（sqlite3 -readonly，stderr 为空，退出码 0），结果与设计一致：
--      · 查询 0：无 system_settings 行时返回空集；写入 ai_master_enabled='FALSE'（大写）
--        与 log_retention_days='7' 后正常返回两行。
--      · 查询 1：6 个账号全部输出；无行账号 effective_enabled=1，NULL 行按 COALESCE 也算 1，
--        三条 ai_enabled=0 的行 effective_enabled=0。
--      · 查询 2：A / B / C 各命中 1 行，且旁证列计数与实际插入的日志行数一致。
--      · 查询 3：五种 reason 全部列出，词表外取值 legacy-label 可见（漂移检测有效）。
--      · 查询 4：NULL 行被单独列出，未被 `= 0` 漏掉。
--      · 总闸置为 'FALSE' 后，查询 1 的全部 effective_enabled 立刻变为 0，
--        与 db.aiMasterEnabledFromValue 的归一（大小写、首尾空白）一致。
--
--   4) 该记录只能证明「SQL 在本方言可执行 + 分类逻辑自洽」，**不代表任何真实分布**，
--      也不能替代在真实库上的一次执行。
-- =============================================================================
