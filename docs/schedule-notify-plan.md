# 日程统一（notify）实施计划

分支 `noted-schedule-notify`，四个仓库：noted（服务端 + MCP）、superai（中转后端）、superai-ios、superai-android。

## 现状（读代码得出）

- **event（日程）**：`events` 表（store/events.go），RRULE 子集重复，`remind_before` 分钟（NULL = 不提醒），服务端 scheduler（internal/reminder + store/reminders.go）按 `remind_before` 产生 Reminder → Hub/WatchReminders/webhook。手机端没有任何日程通知路径。
- **task（待办）**：`tasks` 表（store/tasks.go），`due_ms` + 绝对 `remind_ms`，完成时 spawnNext 生成下一条重复任务。scheduler 按 `remind_ms` 触发。手机端只显示提醒时间，不排任何通知。
- **alarm（闹钟）**：独立 `alarms` 表（store/alarms.go），`mode` alarm/reminder、`schedule` JSON（kind oneShot/recurring，minutely…monthly，byDay/byTime/until/count）、`holiday_policy`、`region`（internal/alarm 引擎 + internal/holiday 中国节假日/调休）。服务端从不触发闹钟；手机端通过 SuperAI 的 `NotedAlarms` RPC → noted `list_alarms` 拿到定义，用同一套引擎在本地展开，iOS 用 AlarmKit（mode alarm）/本地通知（mode reminder），Android 用 setAlarmClock+RingService / 普通通知。版本号变化时 SuperAI 推静默 push 让手机重新同步。
- SuperAI 后端（app_alarms.go、backend/noted.go、alarmplan）不解析字段，只转发 MCP JSON；system prompt 里描述闹钟参数。

## 目标模型

`events` 表就是「日程安排」表，每条有：

| 字段 | 含义 |
|---|---|
| `notify` | `alarm` 手机响铃必须手动关 / `reminder` 系统通知可点掉 / `app` 仅 App 内提示 / `none`。默认 `reminder` |
| `remind_before`（已有） | 提前多少分钟。**NULL = 不排任何通知**（与原有日程语义一致）；0 = 准点（iOS 教训：0 不等于未设置） |
| `rule` | 闹钟式规则（alarm.Schedule JSON：kind、at/anchor/time、recurrence），空 = 普通日程（start + rrule） |
| `holiday_policy`/`region` | 节假日策略，对闹钟式规则和 RRULE 日程都生效 |
| `enabled` | 关闭即暂停，不删除 |
| `original_text` | 用户原话 |
| `source` | `alarm`（闹钟 API 建的/迁移来的）/ `task`（带截止时间的待办镜像）/ `event` |
| `task_id`、`completed` | source=task 时指向待办，镜像完成状态 |

- **闹钟 API 兼容**：`create_alarm`/`update_alarm`/`list_alarms`/`delete_alarm`/`get_alarm`/gRPC AlarmService 保留，内部读写 `events`（source=alarm，mode→notify，remind_before=0）。`list_alarms` 默认筛 `notify=alarm` 或 source=alarm（这样原来 mode=reminder 的闹钟还列得到）；新参数 `notify` 可筛 `alarm|reminder|app|all`，`all` 是手机端要排的全部通知项，每项带 `notify`、`remindBeforeMinutes`、`source`，普通日程的 RRULE 转成闹钟式 schedule（DAILY/WEEKLY/MONTHLY 直接映射，YEARLY→monthly interval 12）。版本号覆盖全部日程。
- **待办**：`tasks` 表保留（完成/标签/工时/重复生成都在那），加 `notify` 列。有截止时间的待办在同一事务里写一条 `events` 镜像（id 同待办 id，source=task，start=due，remind_before=due−remind），所以 `list_events`、通知 feed、服务端 scheduler 都把它当日程处理；`update_event`/`delete_event` 落到镜像上时转给待办。没时间的待办不进 `events`，`list_events(include_tasks)` 以 `unscheduled` 一栏返回；排期（设 due）后镜像自动出现。规则「待办要有时间才能提醒」服务端强制：有 remind 无 due 报错。
- **scheduler**：统一走 events 路径（含闹钟式规则、镜像），`notify=none`/disabled/completed 不触发；Reminder 和 webhook 带 `notify`。
- **迁移**（store.migrate 启动时自动，`noted schedule status|migrate|rollback` 手动）：
  1. `events`、`tasks` 加列（ADD COLUMN，旧二进制忽略新列）。
  2. `alarms` 表每行 INSERT OR IGNORE 到 `events`（同 id），`alarms` 表原样保留 → 直接换回旧二进制就是回滚（数据截止迁移时刻）。
  3. 每个带 due 的待办 upsert 镜像（幂等，每次启动自愈）。
  4. `rollback`：把 source=alarm 的 events 回写 `alarms`（含迁移后新建/改动的），删掉镜像行和迁移标记。
- **手机端**（iOS/Android）：feed 换成 `list_alarms{notify:"all"}`；展开后每次响铃减 `remindBeforeMinutes`；按 `notify` 分流：alarm→AlarmKit/setAlarmClock，reminder→本地通知，app→不排系统通知，只写入 App 内的日程/提示；闹钟页只显示 notify=alarm 或 source=alarm 的项。
- **SuperAI**：`NotedAlarms` 传 `notify:"all"`；日程/待办工具调用也触发版本检查；alarmplan 认识 notify/remindBeforeMinutes；prompt 改为描述 notify。

## 步骤

1. noted：proto → store（events/alarms 门面/tasks 镜像/迁移）→ service → mcpserver → reminder/export/cortexsync → 测试 → `make test`。
2. superai：app_alarms.go、noted.go、alarmplan、测试。
3. superai-ios / superai-android 并行：模型、planner 分流、App 内提示、闹钟页筛选、测试。
4. 回填本文件末尾的「结果」。

## 结果

- noted（服务端 + MCP）：完成。events 表为日程表，`notify` 四档，闹钟 API 门面，待办镜像，`list_alarms{notify:"all"}`，`list_fires`，webhook/Reminder 带 `notify`，导出与 cortex 同步带 `notify`；启动时自动迁移，新增 `noted schedule status|migrate|rollback`。
- 迁移在每次启动自动执行（幂等）；`alarms` 表原样保留。回滚：停服务 → `noted schedule rollback` → 换回旧二进制。
- superai：`NotedAlarms` 传 `notify:"all"`，alarmplan 认识 `notify`/`remindBeforeMinutes`，日程/待办工具调用触发版本检查，prompt 描述 notify。
- iOS / Android：feed 与分流见各仓库提交。
