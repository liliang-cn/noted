# MCP structured output

Every noted MCP tool returns two things:

- `content`: one text block, the same text as before. For most tools it is the protojson of the gRPC
  reply; deletes and a few others return a sentence. Clients that only read text keep working.
- `structuredContent`: a JSON object for programs (the SuperAI app's cards). Its shape is declared in
  the tool's `outputSchema` (from `tools/list`), and the server checks each result against that schema
  before it sends it.

A failed call has `isError: true`, an error message as text, and no `structuredContent`.

The `outputSchema` is the exact contract. This page explains it.

## Conventions

- Field names are camelCase and match the protojson text where both have the field (`dueTime`,
  `projectId`, `workedMinutes`).
- Times are RFC 3339 with an offset in the MCP server's zone: `noted mcp -tz` (default: the system
  zone) for stdio, the server's `time_zone` for remote MCP. Example: `2030-03-05T09:00:00+08:00`.
- Optional strings, times and lists are left out when empty. Booleans and numbers are always present.
  A top-level collection (`items`, `goals`, `tasks`, ...) is always an array, possibly empty.
- Percent fields are fractions: `0.5` means 50%. A goal's `progress.percent` can go above 1.
- Money is `{"amount": 520, "currency": "USD"}`.
- Enums are short lowercase words: `space` is `work` or `life`, a goal's `period` is `day`, `week`
  or `month`, a trade's `side` is `buy` or `sell`. `priority` is a number: 0 none, 1 low, 2 medium,
  3 high (the same values `create_task` takes).
- Every item has `kind`, `id` and `link`. Clients should ignore fields they do not know. New fields
  may be added later; existing ones will not be renamed or removed.

## Kinds and links

`link` is `noted://<kind>/<id>`. The main kinds open their own screen:

| kind | link | made by |
|---|---|---|
| `task` | `noted://task/<id>` | to-dos |
| `event` | `noted://event/<id>` | calendar events (one per occurrence in lists; occurrences of a recurring event share the id) |
| `note` | `noted://note/<id>` | notes |
| `project` | `noted://project/<id>` | projects |
| `goal` | `noted://goal/<id>` | recurring goals |
| `objective` | `noted://objective/<id>` | objectives |
| `holding` | `noted://holding/<id>` | investments |
| `time` | `noted://time/<id>` | time entries and timers |
| `alarm` | `noted://alarm/<id>` | alarms and reminders the phone rings |

Records that belong to one of those link to their parent:

| kind | link | made by |
|---|---|---|
| `checkIn` | `noted://goal/<goalId>` | `list_check_ins`; `check_in`'s `checkIn` |
| `reading` | `noted://objective/<objectiveId>` | `list_readings` |
| `trade` | `noted://holding/<holdingId>` | `list_trades` |
| `taskWork` | `noted://task/<taskId>` | `list_task_work` |
| `reminder` | `noted://task/<refId>` or `noted://event/<refId>` | `list_reminders` |
| `timeGroup` | `noted://project/<id>` or `noted://goal/<id>`; no link when grouped by activity or day | `time_summary` rows, `weekly_review`'s `topActivities` |

`suggestion` and `change` items have `kind` and `id` but no `link`. Act on them with
`accept_suggestion`, `dismiss_suggestion` and `undo_change`.

## Items

Below, `?` marks a field that is left out when empty.

**task**: `title`, `notes?`, `dueTime?`, `remindTime?`, `priority`, `completed`, `completeTime?`,
`overdue` (open and due before now; in `get_overview`, due before the window), `tags?`, `space`,
`projectId?`, `projectTitle?` (`get_overview` only), `rrule?`, `progress` (0-100), `workedMinutes`,
`workSessions`, `lastWorkedTime?`, `createTime`, `updateTime`.

**event**: `title`, `description?`, `location?`, `startTime`, `endTime?`, `allDay`, `timeZone?`,
`rrule?`, `remindBeforeMinutes?`, `space`, `projectId?`, `projectTitle?` (`get_overview` only),
`createTime?`, `updateTime?`. In `list_events` and `get_overview`, `startTime` and `endTime` are those
of the occurrence.

**note**: `title`, `content` (Markdown), `tags?`, `pinned`, `archived`, `space`, `projectId?`,
`createTime`, `updateTime`; `snippet?` and `score?` in `search_notes`.

**project**: `title`, `notes?`, `pinned`, `archived`, `startTime?`, `dueTime?`, `space`, `createTime`,
`updateTime`, `progress` {`tasksTotal`, `tasksDone`, `percent`, `eventsUpcoming`, `notesCount`,
`nextTime?`, `nextTitle?`, `daysLeft?`, `tasksOverdue`, `timeMinutes`}. `daysLeft` counts to the
start date, or to the due date when there is no start; it is negative once past.

**goal**: `title`, `notes?`, `period`, `target`, `unit`, `timeZone?`, `eventId?`, `milestones?`
[{`id`, `title`, `done`, `doneTime?`}], `archived`, `space`, `counterUnit?`, `counterTarget?`,
`objectiveId?`, `projectId?`, `createTime`, `updateTime`, `progress` {`periodStart`, `periodEnd`,
`done`, `target`, `remaining`, `percent`, `achieved`, `behind`, `streak`, `days?` and `recent?`
[{`date` (YYYY-MM-DD), `amount`}], `milestonesDone`, `milestonesTotal`, `milestonePercent`,
`counterDone`, `counterPercent`}.

**objective**: `title`, `notes?`, `space`, `startTime?`, `dueTime?`, `metricName?`, `metricUnit?`
(none: no metric), `metricStart`, `metricTarget`, `archived`, `projectId?`, `createTime`,
`updateTime`, `progress` {`goalsTotal`, `goalsAchieved`, `goalsPercent`, `behind`, `hasMetric`,
`hasReading`, `metricCurrent`, `metricChange`, `metricPercent`, `lastReadingTime?`, `percent`,
`daysLeft?`, `onTrack`}, `goals?` (its dimensions, as goal items).

**holding**: `symbol`, `name?`, `currency`, `notes?`, `lastPrice?` (money, entered by the user),
`lastPriceTime?`, `archived`, `createTime`, `updateTime`,
`plan?` (only when there is a monthly buying plan) {`amount` (money), `day` (1-28), `time` (HH:MM),
`timeZone?`, `eventId?`, `active`, `nextTime?`, `doneThisMonth`, `investedThisMonth` (money),
`streakMonths`},
`position` {`shares`, `avgCost`, `costBasis`, `realizedPnl`, `totalBought` (all money), `trades`,
`hasPrice`, and only when `hasPrice`: `marketValue`, `unrealizedPnl` (money), `unrealizedPercent`}.

**alarm**: `label`, `mode` (`alarm` or `reminder`), `enabled`, `schedule` {`kind` (`oneShot` or
`recurring`), `at?` (oneShot), `anchor?` (recurring, YYYY-MM-DD), `time?` (HH:MM), `timeZone`,
`recurrence?` {`freq` (`minutely`, `hourly`, `daily`, `weekly`, `monthly`), `interval`, `byDay?`
(MON..SUN), `byMonthDay?`, `byTime?` (HH:MM list), `until?` (YYYY-MM-DD), `count?`, `endTime?`
(HH:MM; absent means 22:00)}}, `holidayPolicy` (`none`, `skipHoliday`, `workdayOnly`,
`followStatutory`), `region` (`CN` or empty), `note?`, `projectId?`, `space`, `originalText?`,
`createTime`, `updateTime`, `nextFires` (up to 5 upcoming rings, holidays applied; empty when off or
finished). [alarms.md](alarms.md) is the full contract, with the rules that expand a schedule.

**time**: `title` (the activity), `minutes` (while running: the minutes so far), `day` (YYYY-MM-DD:
the day `startTime` falls on in the server's zone), `startTime`, `endTime?` (not set while running),
`timed`, `running`, `note?`, `projectId?`, `goalId?`, `checkInId?`, `space`, `createTime`, `updateTime`.
`timed` is true when `startTime` and `endTime` are what the person gave or a timer measured, false when
only a duration was logged and the start was defaulted to "when it was logged minus the minutes".
Show the range (13:00–13:30) only when `timed`; otherwise show the day and the minutes. Passing a start
or an end (`log_time`'s `time`, `update_time_entry`'s `time` or `end`) makes an entry timed. Entries
made before the field existed were guessed once: one whose end is within 5 seconds of its
`createTime` is not timed.

**checkIn**: `goalId`, `time`, `amount`, `count`, `note?`, `timeEntryId?`.
**reading**: `objectiveId`, `time`, `value`, `note?`.
**trade**: `holdingId`, `time`, `side`, `shares`, `price` (money, per share), `fee` (money), `note?`.
**taskWork**: `taskId`, `time`, `minutes`, `progress?` (0-100), `note?`.
**reminder**: `refKind` (`task` or `event`), `refId`, `title`, `dueTime?`, `fireTime`, `space`.
**timeGroup**: `id` (the key: the activity in lower case, a project or goal id, empty for entries
with none, or YYYY-MM-DD), `label`, `date?` (YYYY-MM-DD, only when grouped by day; the same as `id`),
`minutes`, `count`.
**suggestion**: `type` (goal_slot, project_dates, conflict, overdue, schedule, weekly, plan, extract,
ask), `status`, `title`, `reason?`, `space`, `source` (`rules` or `ai`), `operations`
[{`type`, `label`, `args?`}], `inputs?` [{`name`, `label`, `type`, `required`}], `createTime`,
`decideTime?`.
**change**: `suggestionId?`, `summary`, `entries`, `createTime`, `undone`, `undoTime?`.

## Results by tool

| tool | structuredContent |
|---|---|
| `get_overview` | `{horizon, from, to, space?, items: [task \| event], goals: [goal], pinnedProjects: [project], runningTimer?: time}` |
| `list_tasks` | `{items: [task], nextPageToken?}` |
| `get_task`, `create_task`, `update_task`, `complete_task`, `log_task_work` | task |
| `list_task_work` | `{items: [taskWork]}` |
| `list_events` | `{items: [event], from, to}` |
| `create_event`, `update_event` | event |
| `list_notes` | `{items: [note], nextPageToken?}` |
| `search_notes` | `{items: [note], mode}` (`fulltext` or `semantic`) |
| `get_note`, `create_note`, `update_note` | note |
| `list_projects` | `{items: [project]}` |
| `get_project` | `{project, tasks: [task], events: [event], notes: [note], goals: [goal], objectives: [objective], timeEntries: [time]}` |
| `create_project`, `update_project` | project |
| `list_goals` | `{items: [goal]}` |
| `get_goal`, `create_goal`, `update_goal`, `set_milestone_done`, `undo_check_in` | goal |
| `check_in` | goal, plus `checkIn` (the check-in just recorded) |
| `list_check_ins` | `{items: [checkIn]}` |
| `list_objectives` | `{items: [objective]}` |
| `get_objective`, `create_objective`, `update_objective`, `record_reading` | objective |
| `list_readings` | `{items: [reading]}` |
| `log_time`, `start_timer`, `stop_timer`, `update_time_entry` | time |
| `get_running_timer` | `{running: false}` or `{running: true, entry: time}` |
| `list_time_entries` | `{items: [time], nextPageToken?}` |
| `time_summary` | `{from, to, groupBy, totalMinutes, totalCount, items: [timeGroup]}` |
| `list_alarms` | `{items: [alarm], version}`; `version` is opaque and changes whenever any of the user's alarms changes, deletes included |
| `get_alarm`, `create_alarm`, `update_alarm` | alarm |
| `get_holidays` | `{year, region, days: [{date, kind, name}], availableYears, version}`; a day's `kind` is `holiday` or `workday` (调休补班); days are not items |
| `list_holdings` | `{items: [holding]}` |
| `get_holding`, `create_holding`, `update_holding`, `record_trade` | holding |
| `list_trades` | `{items: [trade]}` |
| `list_reminders` | `{items: [reminder]}` |
| `weekly_review` | `{from, to, tasksDone, tasksTotal, timeMinutes, goals: [goal], carriedTasks: [task], topActivities: [timeGroup], nextWeek?: suggestion}` |
| `list_suggestions` | `{items: [suggestion]}` |
| `dismiss_suggestion`, `propose_schedule`, `plan_from_text`, `extract_tasks` | suggestion |
| `accept_suggestion` | `{suggestion, change?}` |
| `list_changes` | `{items: [change]}` |
| `undo_change` | change |
| `delete_task`, `delete_event`, `delete_note`, `delete_project`, `delete_goal`, `delete_objective`, `delete_holding`, `delete_time_entry`, `delete_trade`, `delete_reading`, `delete_alarm` | `{deleted: <id>, kind}` |
| `delete_task_work` | `{deleted: <id>, kind: "taskWork", task}` (the task with its totals recomputed) |
| `ask_assistant` | `{reply, sessionId, toolsUsed?, suggestion?, change?, references?: [{kind, id, link, title, time?}]}` |
| `summarize_note` | `{noteId, summary}` |
| `suggest_tags` | `{noteId, tags, applied}` |
| `daily_briefing` | `{briefing}` |

In `get_overview`, an item's `kind` (`task` or `event`) says which shape it has. The schema lists the
two as `anyOf`.
