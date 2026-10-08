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

接口定义在 [`api/noted/v1/noted.proto`](api/noted/v1/noted.proto),分三个 service:`NoteService`、`CalendarService`、`AIService`。更新类接口用 `update_mask` 指定要改的字段。周期规则支持 RRULE 子集:`FREQ`(DAILY/WEEKLY/MONTHLY/YEARLY)、`INTERVAL`、`COUNT`、`UNTIL`、`BYDAY`(仅 WEEKLY)。

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

## 开发

```sh
make test     # go vet + go test -race
make proto    # 修改 proto 后重新生成,需要 buf、protoc-gen-go、protoc-gen-go-grpc
```

代码结构:`internal/store` 是 SQLite 存储(不依赖 AI),`internal/service` 是 gRPC 实现,`internal/ai` 是可选的 AI 层(通过 `Engine` 接口接入,关闭时为 nil),`internal/mcpserver` 是 MCP 工具,`internal/reminder` 是提醒调度。
