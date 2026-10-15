# noted

自托管的笔记 + 日程 gRPC 服务,单个二进制,一个 SQLite 文件。AI 是可选增强:不配也是完整可用的笔记日程后端,配了才多出助手、摘要、语义搜索。还自带 MCP server,Claude 等客户端可以直接读写你的笔记和日程。

- **笔记**:Markdown、标签、置顶、归档、全文搜索(中文可搜)
- **日程**:事件(时区、全天、周期规则、提前提醒)、待办(优先级、截止、提醒)
- **项目**:一件有终点的事(比如一次旅行),下面挂待办、日程和笔记,有进度、倒计时和置顶
- **目标**:周期性的目标(每周运动 3 次、每周学 140 分钟德语),打卡、连续周期数、是否落后于节奏、可选的里程碑路径,还可以带一个不限周期的累计计数器(课时 19/40),打卡时用 `amount` 记分钟、用 `count` 记课时
- **工作 / 个人**:每条内容属于其一,所有列表、搜索、概览都能按空间过滤,不指定就是全部
- **概览**:`GetFocus` 一次返回 今天 / 接下来 / 这周 / 这个月 的日程、待办、目标和置顶项目
- **建议**:目标落后、项目缺日期、日程冲突、任务逾期、排进空档、每周回顾,**用固定规则算,不需要大模型**;你采纳了才写入,每次写入都能撤销
- **提醒**:gRPC 流式推送、历史查询、可选 webhook
- **多用户**:Bearer token,数据按用户隔离
- **偏好**:每用户的小型 JSON(主题、页面布局),多设备同步
- **AI(可选)**:自然语言助手、一句话拆成项目、笔记提取待办、摘要、自动标签、每日简报、语义搜索,基于 [Agent-Go](https://github.com/liliang-cn/agent-go) 和 [CortexDB](https://github.com/liliang-cn/cortexdb),接任意 OpenAI 兼容接口
- **MCP**:把以上能力暴露为 MCP 工具,可以远程(Streamable HTTP,URL + token)或本地 stdio(`noted mcp`)接入

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
| `NOTED_MCP_LISTEN` | 远程 MCP(Streamable HTTP)监听地址,如 `127.0.0.1:43638`;不设就不开,见 [MCP](#mcp) |
| `NOTED_PUBLIC_URL` | 外部访问 `mcp_listen` 的地址,如 `https://noted.example.com`;MCP 登录的 OAuth 元数据和配对二维码用它。不设就取请求的 `X-Forwarded-Proto` 和 Host |
| `NOTED_DATA_DIR` | 数据目录,默认 `./data` |
| `NOTED_TIME_ZONE` | 简报和助手日期计算用的默认时区 |
| `NOTED_LANGUAGE` | 服务器自己写的句子(建议、回顾)用 `zh` 还是 `en`,默认 `zh` |
| `NOTED_AUTH_DISABLED` | `true` 时免 token,所有调用者是用户 `default`,只适合本机 |
| `NOTED_AUTH_SIGNUP` | `true` 时任何人都能用邮箱 + 密码注册(gRPC `SignUp` 和网页);默认关闭 |
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

已登录的用户也可以自己管理 token(比如给每个 AI 助手单独发一个,用完吊销):`AccountService.CreateToken`(`label` 去掉首尾空白后最多 60 个字符,不填就是"AI 助手";明文 token 只在这次返回)、`ListTokens`(只列自己的,带 id、标签、创建时间、最近使用时间,`current` 标出发起这次调用的 token)、`RevokeToken`(按 id 吊销,立即失效,可以吊销正在用的这个;别人的 id 一律 NOT_FOUND)。最近使用时间每个 token 最多每分钟写一次。

### 邮箱 + 密码

`AccountService.SignUp` / `SignIn`(无需 token)用邮箱和密码注册、登录,返回一个新 token;`SetEmailPassword` 给当前账号(比如用 Apple 登录建的账号)加上或修改邮箱和密码,已有密码时必须带 `current_password`。邮箱不区分大小写,密码 8 到 128 个字符,只存 bcrypt(cost 12)哈希。注册默认关闭(`[auth] signup = true` 打开,关闭时返回 FAILED_PRECONDITION);邮箱已被注册返回 ALREADY_EXISTS;邮箱或密码错误统一返回同一条 UNAUTHENTICATED,不透露是哪个错了。同一个邮箱或同一个来源地址 10 分钟内失败 5 次后锁 10 分钟(RESOURCE_EXHAUSTED,内存里计数,重启清零);来源地址在经过本机或内网反向代理时取 `X-Forwarded-For` 的最后一项。

### 网页

开了 `mcp_listen` 时,同一个端口的 `/` 是一个网页:用邮箱密码注册或登录,生成令牌(只显示一次,附带 Claude Code 和 Codex 的配置,地址取自访问的域名),查看和吊销自己的令牌,修改密码。网页登录本身就是一个标签为"网页"的 token,放在 httpOnly、SameSite=Strict 的 cookie 里(HTTPS 下带 Secure),退出登录即吊销它。`/api/` 下的 JSON 接口直接调用 `AccountService` 的实现,规则和 gRPC 完全一致;所有非 GET 请求必须带 `X-Noted: 1` 头,防跨站请求。

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

接口定义在 [`api/noted/v1/noted.proto`](api/noted/v1/noted.proto),主要的 service:`NoteService`、`CalendarService`、`GoalService`、`ObjectiveService`、`HoldingService`、`AlarmService`、`HolidayService`、`ProjectService`、`ExportService`、`AIService`。更新类接口用 `update_mask` 指定要改的字段。周期规则支持 RRULE 子集:`FREQ`(DAILY/WEEKLY/MONTHLY/YEARLY)、`INTERVAL`、`COUNT`、`UNTIL`、`BYDAY`(仅 WEEKLY)。

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

### 闹钟

`AlarmService` 存闹钟的定义(一次性或循环:每 N 分钟/小时、每天、每周几、每隔一周、每月几号、一天几个时间点,可设截止日期或次数),手机端(SuperAI 的 AlarmKit)照同一套规则在本地响铃。`mode` 是 `alarm`(穿透静音的真闹钟)或 `reminder`(只发通知)。`holidayPolicy` 可以跳过法定节假日,或只在工作日响(按国务院的放假和调休补班安排,`region` 为 `CN` 时生效;中国大陆时区默认 `CN`)。每个闹钟返回服务端算出的接下来 5 次响铃 `next_fires`;`ListAlarms` / `GetAlarmsVersion` 带一个版本号,任何闹钟有增删改它就变,客户端据此决定要不要重新同步。`HolidayService/GetHolidays` 返回某年的放假和补班日,数据来自 [holiday-cn](https://github.com/NateScarlet/holiday-cn),目前内置 2025、2026 年。完整规则和共享测试用例见 [`docs/alarms.md`](docs/alarms.md)、[`docs/alarm-fixtures.json`](docs/alarm-fixtures.json)。

```sh
grpcurl -plaintext -H "$T" -d '{"alarm":{"label":"接三宝","holiday_policy":"workdayOnly","schedule":{"kind":"recurring","anchor":"2026-10-12","time":"15:15","time_zone":"Asia/Shanghai","recurrence":{"freq":"weekly","interval":2,"by_day":["MON"]}}}}' $A noted.v1.AlarmService/CreateAlarm
grpcurl -plaintext -H "$T" -d '{"year":2026}' $A noted.v1.HolidayService/GetHolidays
```

### 导出

你的数据随时能带走。服务端导出有两种方式:

```sh
noted export alice -o alice.zip                      # 在服务器上,直接读数据库
grpcurl -plaintext -H "$T" $A noted.v1.ExportService/Export   # 远程,流式返回同一个 zip
```

zip 里有 `noted.json`(全部记录,包括已归档和已完成的、目标和读数、标的和买卖记录、闹钟、设置)、`notes/` 下每条笔记一个带元数据的 Markdown、`calendar.ics`(日程和有日期的待办,任何日历 App 都能导入)和 `trades.csv`(买卖记录,可直接用表格打开)。iOS App 在 设置 → 导出我的数据 里直接生成并分享同一个文件。

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
- **按空间控制 AI 能读什么。** `AIService.SetAIAccess` 可以分别关掉「工作」或「个人」,默认两边都开。关掉的空间:助手看不到它的任何内容(列表、搜索、按 id 读取都一样);它的笔记不会发给 embedding 服务,已建的索引会被清掉;摘要、标签、提取待办、简报都不处理它;关闭前的旧对话不会再被带给模型。重新打开后会自动补建索引。这些在服务端强制执行,不依赖客户端。该设置只能通过 `SetAIAccess` 修改,普通偏好接口改不了。
- **助手不会直接改你的数据。** 它调用的写工具只是"暂存",结果作为一个待确认的提案(`Proposal`)随回复返回;你调用 `SuggestionService.AcceptProposal` 才真正写入,并产生一条可 `UndoChange` 撤销的改动记录。`Ask` 的 `auto_apply` 用于自己另有确认步骤的调用方(MCP 就是这样)。
- `PlanFromText` 只保留你明确说出的日期。"下个月去"不是确切日期,出发日期会作为必填项返回让你填,而不是猜一个。

## 同步到 CortexDB(可选)

把指定用户的数据单向同步进 CortexDB「大脑」,让它知道**有什么、彼此怎么关联**;精确的数字、时间和状态仍以 noted 为准。默认关闭,只有 `sync_users` 里列出的用户会被读取和发送。

```toml
[cortexdb]
remote     = "10.0.0.5:46017"     # CortexDB gRPC 地址
sync_users = ["apple-b0d825f8"]
interval   = "15m"
```

token 只从环境变量 `CORTEXDB_GRPC_TOKEN` 读,不写进配置文件。也可以用 `NOTED_CORTEXDB_REMOTE`、`NOTED_CORTEXDB_SYNC_USERS`(逗号分隔)、`NOTED_CORTEXDB_INTERVAL` 配置。

- 每一项是集合 `noted` 里的一条知识记录(`knowledge_save`),同时是一个图实体,id 固定:记录 `noted:<用户>:<kind>:<id>`,实体 `entity:noted:<用户>:<kind>:<id>`,来源 `noted://<kind>/<id>`。kind 有 project、task、event、note、goal、objective、holding、alarm、time。
- 笔记存完整 Markdown;关系有 `part_of`(待办、日程、笔记、目标、闹钟 → 项目)、`references`(笔记里的 `[@标题](noted://kind/id)`)、`dimension_of`(目标 → 大目标)、`count_toward` / `spent_on`(每周时间汇总 → 目标 / 项目)、`held_in`(标的 → 币种)。
- 持仓、指标读数、时长、闹钟接下来的响铃只以带日期、标明「推算快照,以 noted 为准」的形式出现;时间记录不逐条同步,按 ISO 周各写一条汇总。
- 增量:服务器记下每项发送时的哈希,只发有变化的;noted 里删除的会从大脑里删掉(记录和实体一起)。失败的项下次重试,不影响服务。日志只记数量和 id。
- 在 AI 设置里关掉的空间不会同步,已同步的会被撤回。

第一次建议手动跑:

```sh
noted cortex sync apple-b0d825f8 --dry-run   # 只列出会发送/删除的 id,不发送
noted cortex sync apple-b0d825f8             # 立刻同步一次;--full 全部重发
```

## MCP

两种接法,工具完全相同:

- **远程(推荐给别人用)**:`noted serve` 在 `mcp_listen` 上用 Streamable HTTP 提供 MCP,客户端只需要一个 URL 和一个 token。
- **本地 stdio**:`noted mcp` 作为 gRPC 客户端连到运行中的 noted 服务。

两种都走 gRPC 的鉴权和用户隔离。

### 远程 MCP(Streamable HTTP)

在配置里打开:

```toml
[server]
mcp_listen = "127.0.0.1:43638"   # 或环境变量 NOTED_MCP_LISTEN;不设就不开
```

端点是 `http://<mcp_listen>/mcp`,另有 `GET /healthz` 返回 200,`/` 是管理令牌的[网页](#网页)。每个请求都要带 `Authorization: Bearer <token>`,token 和 gRPC 用的是同一种(`noted token new` 或 `AccountService.CreateToken`),工具以该 token 的用户身份运行;没带或无效返回 401(`WWW-Authenticate: Bearer resource_metadata="…"`,指向下面的登录),吊销后正在进行的会话也会立刻 401。每个 MCP 会话绑定开它的 token,换个 token 拿同一个会话 id 会被拒绝。不带偏移的时间按服务端的 `time_zone` 解读。服务本身是明文 HTTP,对外请放在 TLS 反向代理后面,例如 Caddy:

```caddy
noted.example.com {
    @grpc header Content-Type application/grpc*
    handle @grpc {
        reverse_proxy h2c://127.0.0.1:43872   # gRPC,不要加 flush_interval(会弄坏错误 trailer)
    }
    handle {
        reverse_proxy http://127.0.0.1:43638 {   # MCP、网页、/healthz
            flush_interval -1                     # 流式响应(SSE)不要缓冲
        }
    }
}
```

客户端:

```sh
# Claude Code:不用 token,加上以后在 Claude Code 里输入 /mcp,选 noted 登录
claude mcp add --scope user --transport http noted https://noted.example.com/mcp

# Claude Code:用 token
claude mcp add --scope user --transport http noted https://noted.example.com/mcp --header "Authorization: Bearer <token>"

# Codex(~/.codex/config.toml)
[mcp_servers.noted]
url = "https://noted.example.com/mcp"
bearer_token_env_var = "NOTED_TOKEN"   # token 放在这个环境变量里
```

不用 token 的登录走 MCP 标准授权(OAuth 2.1 + PKCE,`/.well-known/oauth-protected-resource`、`/.well-known/oauth-authorization-server`、`/oauth/register`、`/oauth/authorize`、`/oauth/token`)。浏览器打开的页面显示一个配对码和二维码,在 App 的「设置 → 接入 AI 助手」扫码或输入配对码允许(`AccountService.ApprovePairing`);这个浏览器已经登录网页的话,页面上也可以直接允许。配对码 10 分钟有效。允许后客户端拿到的就是一个普通 token,标签是客户端的名字,在令牌列表里可以吊销。反向代理没传 `X-Forwarded-Proto` 的话,设 `public_url`。

### 本地 stdio(`noted mcp`)

`noted mcp` 是一个 stdio MCP server,作为客户端连到运行中的 noted 服务,所以鉴权和用户隔离与 gRPC 完全一致,也可以连远程服务。

| 参数 | 环境变量 | 说明 |
| --- | --- | --- |
| `-addr` | `NOTED_ADDR` | 服务地址,默认 `127.0.0.1:43872` |
| `-token` | `NOTED_TOKEN` | 用户 token(服务端关了鉴权可省略) |
| `-tls` | | 用 TLS 连接服务 |
| `-tz` | `NOTED_TIME_ZONE` | 不带时区偏移的时间按这个时区解读,默认本机时区 |

#### Claude Code

```sh
claude mcp add noted -e NOTED_ADDR=127.0.0.1:43872 -e NOTED_TOKEN=<token> -- /path/to/noted mcp
```

#### Claude Desktop 等

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
| 待办 | `list_tasks` `get_task` `create_task` `update_task` `complete_task` `delete_task` |
| 待办投入 | `log_task_work` `list_task_work` `delete_task_work`(每次做了多少分钟、进度) |
| 提醒 | `list_reminders` |
| 概览 | `get_overview`(今天 / 接下来 / 这周 / 这个月) |
| 项目 | `list_projects` `get_project` `create_project` `update_project` `delete_project` |
| 目标 | `list_goals` `get_goal` `create_goal` `update_goal` `delete_goal` `check_in` `list_check_ins` `undo_check_in` `set_milestone_done` |
| 大目标 | `list_objectives` `get_objective` `create_objective` `update_objective` `delete_objective` `record_reading` `list_readings` `delete_reading` |
| 时间记录 | `log_time` `start_timer` `stop_timer` `get_running_timer` `list_time_entries` `update_time_entry` `delete_time_entry` `time_summary`(游泳 30 分钟;可挂项目或目标,挂目标时自动打卡,按活动/项目/目标/天汇总) |
| 投资 | `list_holdings` `get_holding` `create_holding` `update_holding` `delete_holding` `record_trade` `list_trades` `delete_trade` |
| 闹钟 | `create_alarm` `list_alarms` `get_alarm` `update_alarm` `delete_alarm` `get_holidays`(叫我/闹钟用 `mode: alarm`,只要通知用 `reminder`;可跳过节假日或只在工作日响) |
| 建议 | `list_suggestions` `accept_suggestion` `dismiss_suggestion` `propose_schedule` `weekly_review` `list_changes` `undo_change` |
| AI | `ask_assistant` `summarize_note` `suggest_tags` `daily_briefing` `plan_from_text` `extract_tasks`(仅服务端开了 AI 时才出现) |

带 `space` 参数的工具可传 `work` 或 `life`,不传就是两边都看;`create_task` 等传 `project_id` 就加进项目,已有的待办、日程、笔记、目标、大目标用 `update_*` 的 `project_id` 挪进项目。`record_trade` 可以只给代码(如 `VOO`),第一次买入时自动建持仓。stdio 模式不设 `-tz` / `NOTED_TIME_ZONE` 时用本机时区的 IANA 名字,远程 MCP 用服务端的 `time_zone`。时间一律用 RFC 3339,例如 `2026-03-05T15:00:00+08:00`;"下周五"这类相对日期由调用方的模型先换算。删除类工具带 destructive 标注,客户端通常会弹确认。出错时作为工具错误返回,模型可以读到原因并重试。

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

代码结构:`internal/store` 是 SQLite 存储(不依赖 AI),`internal/service` 是 gRPC 实现,`internal/ai` 是可选的 AI 层(通过 `Engine` 接口接入,关闭时为 nil),`internal/mcpserver` 是 MCP 工具,`internal/mcphttp` 把它们通过 Streamable HTTP 提供给远程客户端,`internal/portal` 是注册登录和管理令牌的网页,`internal/oauth` 是 MCP 客户端不用令牌的登录(OAuth + App 配对),`internal/reminder` 是提醒调度。
