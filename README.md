# noted

自托管的笔记 + 日程 gRPC 服务,单个二进制,一个 SQLite 文件。AI 是可选增强:不配也是完整可用的笔记日程后端,配了才多出助手、摘要、语义搜索。还自带 MCP server,Claude 等客户端可以直接读写你的笔记和日程。

- **笔记**:Markdown、标签、置顶、归档、全文搜索(中文可搜)
- **日程**:事件(时区、全天、周期规则、提前提醒)、待办(优先级、截止、提醒)
- **项目**:一件有终点的事(比如一次旅行),下面挂待办、日程和笔记,有进度、倒计时和置顶
- **目标**:周期性的目标(每周运动 3 次、每周学 140 分钟德语),打卡、连续周期数、是否落后于节奏、可选的里程碑路径,还可以带一个不限周期的累计计数器(课时 19/40),打卡时用 `amount` 记分钟、用 `count` 记课时
- **工作 / 生活**:每条内容属于其一,所有列表、搜索、概览都能按空间过滤,不指定就是全部
- **概览**:`GetFocus` 一次返回 今天 / 接下来 / 这周 / 这个月 的日程、待办、目标和置顶项目
- **建议**:目标落后、项目缺日期、日程冲突、任务逾期、排进空档、每周回顾,**用固定规则算,不需要大模型**;你采纳了才写入,每次写入都能撤销
- **提醒**:gRPC 流式推送、历史查询、可选 webhook
- **多用户**:Bearer token,数据按用户隔离
- **偏好**:每用户的小型 JSON(主题、页面布局),多设备同步
- **AI(可选)**:自然语言助手、一句话拆成项目、笔记提取待办、摘要、自动标签、每日简报、语义搜索,基于 [Agent-Go](https://github.com/liliang-cn/agent-go) 和 [CortexDB](https://github.com/liliang-cn/cortexdb),接任意 OpenAI 兼容接口
- **MCP**:`noted mcp` 把以上能力暴露为 MCP 工具

## 部署

### Docker

```sh
git clone https://github.com/liliang-cn/noted && cd noted
docker compose up -d --build
docker compose run --rm noted user add alice   # 打印 token,只显示一次
```

数据在命名卷 `noted-data`(容器内 `/data`),服务监听 `43872`。如果改用 bind mount,宿主目录要对 uid 65532 可写。

### 二进制

```sh
make build                       # 纯 Go,无需 CGO,产物 bin/noted
./bin/noted user add alice       # 创建用户并打印 token
./bin/noted serve                # 默认 127.0.0.1:43872,数据在 ./data
```

不需要任何配置文件即可运行。要对外提供服务,监听地址改成 `0.0.0.0:43872`(`NOTED_LISTEN` 或配置文件),并建议启用 TLS 或放在反向代理后面。

### 配置

复制 [`noted.example.toml`](noted.example.toml) 为 `noted.toml`(当前目录会自动读取,也可用 `-config` 或 `NOTED_CONFIG` 指定)。环境变量优先于文件:

| 变量 | 含义 |
| --- | --- |
| `NOTED_LISTEN` | 监听地址,默认 `127.0.0.1:43872` |
| `NOTED_DATA_DIR` | 数据目录,默认 `./data` |
| `NOTED_TIME_ZONE` | 简报和助手日期计算用的默认时区 |
| `NOTED_LANGUAGE` | 服务器自己写的句子(建议、回顾)用 `zh` 还是 `en`,默认 `zh` |
| `NOTED_AUTH_DISABLED` | `true` 时免 token,所有调用者是用户 `default`,只适合本机 |
| `NOTED_WEBHOOK_URL` | 每条触发的提醒会 POST 一份 JSON 到这里(失败重试 3 次) |
| `NOTED_AI_ENABLED` | `true` 开启 AI |
| `NOTED_AI_LLM_BASE_URL` / `_API_KEY` / `_MODEL` | 助手、摘要、标签、简报用的模型 |
| `NOTED_AI_EMBEDDING_BASE_URL` / `_API_KEY` / `_MODEL` | 可选,配了才有语义搜索 |

TLS 在配置文件里设置 `server.tls_cert` 和 `server.tls_key`。

### 用户和 token

```sh
noted user add alice          # 新建用户,打印第一个 token
noted token new alice -label laptop   # 再签发一个
noted user list
```

客户端把 token 放在 gRPC metadata:`authorization: Bearer <token>`。数据库只存 token 的哈希。

### 备份

停服后复制整个数据目录即可。运行中备份请用 `sqlite3 data/noted.db ".backup 'noted-backup.db'"`(数据库是 WAL 模式,直接拷文件可能不一致)。`data/ai/` 是可重建的索引,可以不备份。

### 健康检查

标准 gRPC health 协议。容器内可用 `noted health -addr 127.0.0.1:43872`,镜像已内置 `HEALTHCHECK`。服务启用了 TLS 时探针要加 `-tls`(`noted health -tls`),并在 compose 里覆盖 `healthcheck`,否则默认的明文探针会判为不健康。

## 使用

服务默认开启 gRPC reflection,可以直接用 [grpcurl](https://github.com/fullstorydev/grpcurl):

```sh
T="authorization: Bearer <token>"
A=127.0.0.1:43872

grpcurl -plaintext $A list

# 笔记
grpcurl -plaintext -H "$T" -d '{"note":{"title":"周会纪要","content":"讨论了季度报告","tags":["work"]}}' $A noted.v1.NoteService/CreateNote
grpcurl -plaintext -H "$T" -d '{"query":"季度报告"}' $A noted.v1.NoteService/SearchNotes

# 每周一三五 9:00 的站会,提前 10 分钟提醒
grpcurl -plaintext -H "$T" -d '{"event":{"title":"站会","start_time":"2026-10-12T01:00:00Z","time_zone":"Asia/Shanghai","rrule":"FREQ=WEEKLY;BYDAY=MO,WE,FR","remind_before_minutes":10}}' $A noted.v1.CalendarService/CreateEvent

# 一段时间内的日程(周期事件会展开)
grpcurl -plaintext -H "$T" -d '{"from":"2026-10-12T00:00:00Z","to":"2026-10-19T00:00:00Z"}' $A noted.v1.CalendarService/ListEvents

# 实时接收提醒
grpcurl -plaintext -H "$T" $A noted.v1.CalendarService/WatchReminders
```

接口定义在 [`api/noted/v1/noted.proto`](api/noted/v1/noted.proto),主要的 service:`NoteService`、`CalendarService`、`GoalService`、`ObjectiveService`、`HoldingService`、`ProjectService`、`ExportService`、`AIService`。更新类接口用 `update_mask` 指定要改的字段。周期规则支持 RRULE 子集:`FREQ`(DAILY/WEEKLY/MONTHLY/YEARLY)、`INTERVAL`、`COUNT`、`UNTIL`、`BYDAY`(仅 WEEKLY)。

### 目标计划

`ObjectiveService` 管"大目标",比如减肥:它由几个维度(普通的 `Goal`,例如游泳每周 2 次、跑步每周 3 次、轻食每周 10 餐、控糖每周 7 天,各自有周期和数量)加一个可选的结果指标(体重 75 → 68 kg,随时记录读数)组成。创建维度时把 `goal.objective_id` 设成计划的 id。进度同时给两个数:`goals_percent` 是各维度本期完成度的平均,`metric_percent` 是指标从起点走向目标的比例;有读数后 `percent` 取指标,没有就取维度平均。设了起止日期时,`on_track` 表示指标没有落后于已用掉的时间。指标可以向下(体重)也可以向上(肌肉)。删除计划会保留里面的维度,变成单独的目标。

```sh
grpcurl -plaintext -H "$T" -d '{"objective":{"title":"减肥","metric_name":"体重","metric_unit":"kg","metric_start":75,"metric_target":68,"due_time":"2027-01-09T00:00:00Z"}}' $A noted.v1.ObjectiveService/CreateObjective
grpcurl -plaintext -H "$T" -d '{"objective_id":"<id>","value":73.8}' $A noted.v1.ObjectiveService/RecordMeasurement
```

### 标的和定投

`HoldingService` 记录你投资的标的(QQQM、VOO、OKLO 这样的代码)、买卖记录和每月定投计划。持仓按平均成本法算:股数、均价、成本、已实现盈亏;你自己填一个现价,就有市值和浮动盈亏。卖出超过当时持有的股数会被拒绝,补记以前的交易也会按时间顺序重新校验。设了 `dca_day`(1-28)后,服务端会建一条每月同一天的周期日程"定投 QQQM",准点提醒,所以本地通知、Apple Watch 和日历里都能看到;改日期或金额会移动同一条日程,停掉计划、归档或删除标的会一起删掉它。`position.dca` 给出这个月是否已经买过、本月投入和连续几个月都买了。

noted 只记录和提醒:不联网取行情,不下单,不碰钱。现价要自己填。

```sh
grpcurl -plaintext -H "$T" -d '{"holding":{"symbol":"QQQM","dca_day":15,"dca_amount":500,"time_zone":"Asia/Shanghai"}}' $A noted.v1.HoldingService/CreateHolding
grpcurl -plaintext -H "$T" -d '{"holding_id":"<id>","trade":{"side":"buy","shares":3,"price":160}}' $A noted.v1.HoldingService/RecordTrade
```

### 导出

你的数据随时能带走。服务端导出有两种方式:

```sh
noted export alice -o alice.zip                      # 在服务器上,直接读数据库
grpcurl -plaintext -H "$T" $A noted.v1.ExportService/Export   # 远程,流式返回同一个 zip
```

zip 里有 `noted.json`(全部记录,包括已归档和已完成的、目标和读数、标的和买卖记录、设置)、`notes/` 下每条笔记一个带元数据的 Markdown、`calendar.ics`(日程和有日期的待办,任何日历 App 都能导入)和 `trades.csv`(买卖记录,可直接用表格打开)。iOS App 在 设置 → 导出我的数据 里直接生成并分享同一个文件。

## AI(可选)

默认关闭。开启:

```toml
[ai]
enabled = true

[ai.llm]
base_url = "https://api.openai.com/v1"
api_key  = "${OPENAI_API_KEY}"
model    = "gpt-5.4-mini"

[ai.embedding]            # 可选
base_url = "https://api.openai.com/v1"
api_key  = "${OPENAI_API_KEY}"
model    = "text-embedding-3-small"
```

| 能力 | 需要 |
| --- | --- |
| `Ask` 助手(能查,能**准备**笔记、日程、待办、项目、打卡,不能删除)、`PlanFromText`(一句话拆成项目)、`ExtractTasks`(笔记提取待办)、`SummarizeNote`、`SuggestTags`、`DailyBriefing` | LLM |
| `SearchNotes` 的 `semantic: true` | embedding |

- 没开 AI 时,`AIService` 除 `GetStatus` 外都返回 `FAILED_PRECONDITION`;请求语义搜索会自动退回全文搜索,响应里的 `mode` 会写明。
- 启动时会检查 LLM/embedding 地址,key 写错立刻报错,不会等到第一次请求。
- 开 embedding 后,后台会自动给已有笔记建索引。**换了 embedding 模型**需要删除 `<data_dir>/ai/index.db` 后重启,会自动重建。
- 助手每条消息最多调用 8 轮工具,用来限制成本。
- **按空间控制 AI 能读什么。** `AIService.SetAIAccess` 可以分别关掉「工作」或「生活」,默认两边都开。关掉的空间:助手看不到它的任何内容(列表、搜索、按 id 读取都一样);它的笔记不会发给 embedding 服务,已建的索引会被清掉;摘要、标签、提取待办、简报都不处理它;关闭前的旧对话不会再被带给模型。重新打开后会自动补建索引。这些在服务端强制执行,不依赖客户端。该设置只能通过 `SetAIAccess` 修改,普通偏好接口改不了。
- **助手不会直接改你的数据。** 它调用的写工具只是"暂存",结果作为一个待确认的提案(`Proposal`)随回复返回;你调用 `SuggestionService.AcceptProposal` 才真正写入,并产生一条可 `UndoChange` 撤销的改动记录。`Ask` 的 `auto_apply` 用于自己另有确认步骤的调用方(MCP 就是这样)。
- `PlanFromText` 只保留你明确说出的日期。"下个月去"不是确切日期,出发日期会作为必填项返回让你填,而不是猜一个。

## MCP

`noted mcp` 是一个 stdio MCP server,作为客户端连到运行中的 noted 服务,所以鉴权和用户隔离与 gRPC 完全一致,也可以连远程服务。

| 参数 | 环境变量 | 说明 |
| --- | --- | --- |
| `-addr` | `NOTED_ADDR` | 服务地址,默认 `127.0.0.1:43872` |
| `-token` | `NOTED_TOKEN` | 用户 token(服务端关了鉴权可省略) |
| `-tls` | | 用 TLS 连接服务 |
| `-tz` | `NOTED_TIME_ZONE` | 不带时区偏移的时间按这个时区解读,默认本机时区 |

### Claude Code

```sh
claude mcp add noted -e NOTED_ADDR=127.0.0.1:43872 -e NOTED_TOKEN=<token> -- /path/to/noted mcp
```

### Claude Desktop 等

```json
{
  "mcpServers": {
    "noted": {
      "command": "/path/to/noted",
      "args": ["mcp"],
      "env": { "NOTED_ADDR": "127.0.0.1:43872", "NOTED_TOKEN": "<token>" }
    }
  }
}
```

### 工具

| 类别 | 工具 |
| --- | --- |
| 笔记 | `search_notes` `list_notes` `get_note` `create_note` `update_note` `delete_note` |
| 日程 | `list_events` `create_event` `update_event` `delete_event` |
| 待办 | `list_tasks` `create_task` `complete_task` `delete_task` |
| 提醒 | `list_reminders` |
| 概览 | `get_overview`(今天 / 接下来 / 这周 / 这个月) |
| 项目 | `list_projects` `get_project` `create_project` `update_project` |
| 目标 | `list_goals` `create_goal` `check_in` |
| 建议 | `list_suggestions` `accept_suggestion` `dismiss_suggestion` `weekly_review` `list_changes` `undo_change` |
| AI | `ask_assistant` `summarize_note` `daily_briefing` `plan_from_text` `extract_tasks`(仅服务端开了 AI 时才出现) |

带 `space` 参数的工具可传 `work` 或 `life`,不传就是两边都看;`create_task` 等传 `project_id` 就加进项目。时间一律用 RFC 3339,例如 `2026-03-05T15:00:00+08:00`;"下周五"这类相对日期由调用方的模型先换算。删除类工具带 destructive 标注,客户端通常会弹确认。出错时作为工具错误返回,模型可以读到原因并重试。

## iOS

`ios/` 是 SwiftUI 客户端(iOS 18+),直接连这个服务的 gRPC 端口。

```sh
cd ios && xcodegen generate && open Noted.xcodeproj
```

首次打开填服务器地址、端口和 `noted user add` 给的 token。Swift 的 gRPC 桩代码在 `ios/Noted/Generated`,改了 proto 之后用 `protoc-gen-swift` 和 `protoc-gen-grpc-swift-2` 重新生成。主题和概览布局保存在服务端的偏好里,多台设备共用。

提醒:App 把接下来 14 天里设了提醒的日程和待办预约成本地通知(最多 60 条),App 在前台时服务端触发的提醒通过 `WatchReminders` 流显示成提示条。小组件和 Apple Watch 显示的是 App 写下的当天快照,不直连服务器,也拿不到令牌。Pro 订阅的商品 ID 是 `cn.superleo.noted.pro.yearly` 和 `cn.superleo.noted.pro.monthly`,`ios/Noted.storekit` 里的价格只是本地测试占位。

改了 proto 后重新生成 Swift 桩:`protoc -I api --swift_out=ios/Noted/Generated --swift_opt=Visibility=Public --grpc-swift-2_out=ios/Noted/Generated --grpc-swift-2_opt=Server=false,Visibility=Public noted/v1/noted.proto`(需要 `protoc-gen-swift` 和 `protoc-gen-grpc-swift-2` 在 PATH 里)。`ios/Tests/shots.sh <目录>` 起一个有示例数据的服务,把各页面截图存下来,用来和设计稿对照。

测试:`ios/Tests/run-ui-tests.sh` 起一个真实的 noted 和一个假模型,跑单元测试和 UI 测试,最后核对服务端数据;`ios/Tests/run-watch-roundtrip.sh` 用一对配对的模拟器测手机和手表之间的同步。两者都需要 grpcurl 和已安装的模拟器。

已知限制:`ListProposals` 不像 `GetFocus` 那样接收客户端时区,建议里的空档按服务器的 `NOTED_TIME_ZONE` 计算。

## 开发

```sh
make test     # go vet + go test -race
make proto    # 修改 proto 后重新生成,需要 buf、protoc-gen-go、protoc-gen-go-grpc
```

代码结构:`internal/store` 是 SQLite 存储(不依赖 AI),`internal/service` 是 gRPC 实现,`internal/ai` 是可选的 AI 层(通过 `Engine` 接口接入,关闭时为 nil),`internal/mcpserver` 是 MCP 工具,`internal/reminder` 是提醒调度。
