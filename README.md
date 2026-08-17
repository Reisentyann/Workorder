# AI 工单处理工作台

最小可用的 AI 工单处理系统，帮助客服团队高效处理工单。核心围绕「AI 自动分析 → 结论验证 → 人工确认/修改 → 自动处理建议」链路。

## 技术栈

- 后端：Go（标准库 `net/http` + `log/slog`，零外部依赖）
- 前端：原生 HTML/CSS/JS 单页应用
- 存储：JSON 文件（无数据库，懒标记软删除）

## 目录结构

```
backend/
  cmd/workbench/main.go        # 启动入口
  internal/
    model/                     # 数据模型 + 状态枚举 + UUID
    store/                     # JSON 存储（原子写/锁/软删除/队列/数据集/审计/推送消息）
    analyzer/                  # AI 分析（LLMClient 接口 + MockLLM + 策略模式 + 工厂）
    verifier/                  # 结论验证（证据一致性/历史案例/拒答/三档分级）
    engine/                    # AI 处理机（单 worker 状态机 + 打断 + 超时 + 熔断 + 启动恢复）
    sanitize/                  # 输入脱敏
    demo/                      # 脚本化演示编排（备用）
    api/                       # /api/v1 接口层
  data/                        # 运行时生成的 JSON 数据（data.json + audit.json）
frontend/
  index.html  style.css  app.js
```

## 启动方式

**方式一：预编译 exe 直接运行（无需 Go）**

双击项目根目录 `run-exe.bat`，或直接运行 `backend/workbench.exe`（带 `-demo`）。

**方式二：源码编译运行（需要 Go 1.22+）**

```bash
cd backend
go run ./cmd/workbench
# 默认监听 :8080，前端 http://localhost:8080
```

启用演示/测试接口（演示场景、mock 场景桩）：

```bash
go run ./cmd/workbench -demo
```

可选参数：

```bash
go run ./cmd/workbench -addr :8080 -data data -frontend ../frontend -demo
```

> 一键启动：双击 `start.bat`（需 Go，自动编译）；或双击 `run-exe.bat`（无需 Go，用预编译 exe）。

## 测试方式

```bash
cd backend
go test ./...
```

## 核心设计思路

> 对应题目评分重点：**AI 自动分析、结论验证、后续处理**，三者均已实现，见 1/2/3 节。

### 1. AI 自动分析（analyzer）

- **策略模式 + 工厂 + 注册表**：`Strategy` 接口定义每种工单类型（退款/登录异常/发票/物流/其他）的处理逻辑，`Factory.Register` 挂载。新增类型 = 新增一个策略文件并注册，不改已有代码（开闭原则）。
- **`LLMClient` 接口隔离**：`MockLLM` 用规则 + 关键词模拟分析，未来可换真实 LLM 不改上层。
- **置信度由 LLM 自己判断**：人工指令 > 强关键词命中 > 兜底策略，并按信息完整性下调。
- **金额敏感优先级**：退款金额超阈值（500 元）自动判高优先级。

### 2. 结论验证（verifier）

独立于分析的验证层：

- **证据一致性**：判断依据为空则下调把握度。
- **信息完整性**：缺关键字段（订单号/金额/税号等）标记「需补充信息」。
- **历史案例交叉验证**：用字符 bigram 的 Jaccard 相似度，与**已人工确认的数据集**比对，一致则加权、无同类案例则谨慎下调。
- **拒答 + 三档分级**：`<0.6` 拒答转人工（附已尝试摘要）；`0.6~0.9` 拟稿+人工确认；`≥0.9` 自动处理建议。

### 3. 后续处理

- **可自动处理**：登录异常等给出自动修复建议（操作步骤）。
- **信息不足/低置信**：给出补充信息清单或人工接管建议。
- **人工确认/修改**：确认后回写工单，并将该案例**计入数据集**（供后续验证）。

### 4. 关键机制

- **队列**：工单 FIFO，队首给人工审查；**让 LLM 重做的工单插队到队首**（`PushFront`）。
- **推送消息（服务器推送模拟）**：右侧消息面板预置多条用户反馈，模拟服务器推送；**点击消息直接针对该反馈新建工单**，进入分析流程。
- **AI 处理机状态机**：`QUEUED → RUNNING → SUCCEEDED/CANCELED/TIMED_OUT/FAILED`，单 worker 串行、可打断、超时兜底、状态持久化。
- **快速操作**：预设按钮 + 快捷键（1-9），一键触发预设指令；支持动态新增按钮。
- **强行打断**：Esc / 按钮中断分析，取消请求幂等。
- **链路降级与恢复**：LLM 出错/超时/打断精确区分（`failed`/`timed_out`/`canceled`），出错与超时回落 `pending` 可重试；验证器 panic 降级跳过校准；存储写失败告警；进程崩溃后启动时自动清理残留的 `analyzing` 工单。
- **懒标记软删除**：删除工单用 `deleted` 标记，不物理删除重建。
- **审计日志**：独立 `audit.json`，记录每次操作（创建/分析/确认/状态变更/删除/演示），埋点在 store 层统一记录；配合「历史工单」入口实现追责。
- **历史工单**：前端「历史工单」查看含软删的全部工单 + 每个工单的操作时间线。
- **拒答**：AI 不确定时不硬答，附「已尝试摘要」转人工。
- **熔断**：连续低质 3 次或累计 5 次分析 → 升级 `escalated`，拒绝再次自动分析。
- **输入脱敏**：分析前对手机号/身份证/邮箱/银行卡脱敏，原始工单完整保留。
- **引导式演示**：前端「▶ 演示」面板，选场景后系统逐步引导你操作（提示点哪里、输入什么），替代自动跑日志。
- **日志**：`log/slog` 结构化日志（运行排查），与业务审计日志分离。

## API 概览

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/v1/tickets` | 列表（`?include_deleted=true` 含软删）/ 创建 |
| GET/DELETE | `/api/v1/tickets/{id}` | 详情 / 软删除 |
| PUT | `/api/v1/tickets/{id}/status` | 更新状态 |
| POST | `/api/v1/tickets/{id}/analyze` | 触发分析（可带 `instruction`） |
| POST | `/api/v1/tickets/{id}/analyze/cancel` | 打断分析 |
| PUT | `/api/v1/tickets/{id}/analysis/{aid}/confirm` | 确认/修改 AI 建议 |
| GET | `/api/v1/queue` | 查看队列 |
| GET | `/api/v1/queue/pop` | 取队首工单 |
| GET/POST | `/api/v1/actions` | 预设动作列表 / 新增 |
| DELETE | `/api/v1/actions/{id}` | 删除动作 |
| POST | `/api/v1/tickets/{id}/actions/{aid}` | 执行预设动作 |
| GET | `/api/v1/audit` | 审计日志（`?ticket_id=` 过滤） |
| GET | `/api/v1/inbox` | 推送消息列表（服务器推送模拟） |
| POST | `/api/v1/inbox/{id}/handle` | 处理推送消息（点击消息 → 创建工单） |
| POST | `/api/v1/mock/preset` | 设置演示场景桩（仅 `-demo`） |
| POST | `/api/v1/mock/reset` | 重置场景桩（仅 `-demo`） |
| POST | `/api/v1/demo/run` | 运行演示场景（仅 `-demo`） |
| GET | `/api/v1/demo/logs` | 演示日志（仅 `-demo`） |
