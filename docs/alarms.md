# Alarms

Alarms (闹钟) are rings at set times: one-off or repeating, optionally skipping statutory holidays or
ringing on working days only (mainland China's 法定节假日 and 调休 included). noted keeps the
definitions and is their source of truth; an AI manages them through MCP; the SuperAI iOS app
schedules the rings on the phone with AlarmKit. Both sides expand a schedule with the same rules,
written below and pinned by the shared fixtures in [`alarm-fixtures.json`](alarm-fixtures.json).

The rules are a port of SmartAlarm's `ScheduleEngine.swift`; [the differences](#changes-from-smartalarm)
are listed at the end.

## The alarm item

What the MCP tools return (`structuredContent`) and what the app syncs. Field names and values are
exactly these; `?` marks a field left out when empty.

```jsonc
{
  "kind": "alarm",
  "id": "8b0c…",                      // a UUID
  "link": "noted://alarm/8b0c…",
  "label": "接三宝",
  "mode": "alarm",                    // "alarm" | "reminder"
  "enabled": true,
  "schedule": {
    "kind": "recurring",              // "oneShot" | "recurring"
    "at": "2026-10-14T07:00:00+08:00", // ? oneShot only
    "anchor": "2026-10-12",           // ? recurring only: YYYY-MM-DD, the first day it may ring
    "time": "15:15",                  // ? recurring: HH:MM; absent when byTime is set
    "timeZone": "Asia/Shanghai",      // IANA, always present (default: the server's zone)
    "recurrence": {                   // ? recurring only
      "freq": "weekly",               // "minutely" | "hourly" | "daily" | "weekly" | "monthly"
      "interval": 2,                  // >= 1, always present
      "byDay": ["MON"],               // ? weekly only; always present for weekly
      "byMonthDay": 15,               // ? monthly only; always present for monthly
      "byTime": ["08:00", "18:00"],   // ? daily only
      "until": "2026-12-31",          // ? YYYY-MM-DD, inclusive
      "count": 10,                    // ? rings in all
      "endTime": "18:00"              // ? minutely/hourly only; absent means 22:00
    }
  },
  "holidayPolicy": "workdayOnly",     // "none" | "skipHoliday" | "workdayOnly" | "followStatutory"
  "region": "CN",                     // "CN" | "" (always present)
  "note": "…",                        // ?
  "projectId": "…",                   // ?
  "space": "life",                    // "work" | "life"
  "originalText": "每隔一周的周一 15:15 接三宝", // ?
  "createTime": "2026-10-13T20:00:00+08:00",
  "updateTime": "2026-10-13T20:00:00+08:00",
  "nextFires": ["2026-10-26T15:15:00+08:00", "…"] // up to 5, server-computed, holidays applied; [] if none
}
```

`list_alarms` returns `{ "items": [alarm…], "version": "<opaque>" }`. The version changes whenever
any of the user's alarms is created, changed or deleted (including an alarm leaving a deleted
project); compare it with the one you last synced at. gRPC has the same as `ListAlarmsResponse.version`
and a cheap `GetAlarmsVersion`.

Times in MCP results are RFC 3339 in the MCP server's zone, like every noted tool; `nextFires` are
instants, so read them in `schedule.timeZone` to show wall-clock times. A disabled alarm, a one-shot
whose time has passed and a finished series all have `"nextFires": []`.

## Canonical form

The server validates every write and stores one canonical form, so a client never needs defaults:

- `timeZone` is filled in (the server's zone; MCP: its `-tz` zone).
- `holidayPolicy` is `none` when not given. `region` when not given (gRPC: unset; MCP: omitted) is
  `CN` if the zone is a mainland China zone (`Asia/Shanghai`, `Asia/Chongqing`, `Asia/Harbin`,
  `Asia/Urumqi`, …), else `""`.
- oneShot: `at` is kept in the alarm's zone, whole seconds; `anchor`, `time`, `recurrence` are
  dropped. (Writing `anchor` + `time` instead of `at` is accepted and turned into `at`.)
- recurring: `interval` is at least 1; weekly `byDay` defaults to the anchor's weekday and is written
  MON..SUN in week order without repeats (`MO`, `monday` are accepted on input); monthly
  `byMonthDay` defaults to the anchor's day of the month; `byTime` is sorted, without repeats;
  times are `HH:MM`.

Refused, with a message saying what to change: unknown `kind`/`freq`/`mode`/policy/region; `yearly`;
oneShot without `at`; recurring without `anchor`, `recurrence`, or `time` (unless daily `byTime`);
`byDay` with freq ≠ weekly; `byMonthDay` with freq ≠ monthly; `byTime` with freq ≠ daily; `time`
and `byTime` together; `endTime` with freq other than minutely/hourly; `endTime` before `time` (or,
with no `endTime`, `time` after 22:00); a minutely/hourly step longer than a day; `until` before
`anchor`; interval outside 1-1000; count outside 0-10000; an empty label.

## Engine rules

Input: the rule (`schedule`, `holidayPolicy`, `region`), the holiday calendar of the region, `now`,
and a limit. Output: ring times strictly after `now`, oldest first, at most `limit` (the server uses
5 for `nextFires`; one expansion never returns more than **200**). A disabled alarm returns nothing.

Constants: window **180** days; cap **200**; default endTime **22:00**.

**oneShot**: `[at]` if `at > now`, else `[]`. No window and no holiday policy.

**recurring**: work in civil dates in the alarm's `timeZone`.

1. `today` = now's date in the zone. `last` = `today + 180 days` (so today and the next 180 days).
   If `until` is set and earlier, `last = until`.
2. minutely/hourly with neither `until` nor `count`: the alarm rings on its anchor day only, so
   `last = min(last, anchor)`.
3. `first` = `anchor`; if there is no `count` and `today` is later, `first = today`. (With a `count`,
   rings before today still count, so expansion starts at the anchor.)
4. For each day `d` from `first` to `last`, if `d` matches the rule and passes the holiday policy, for
   each of the day's times `t` in order: the ring is `d` at `t` in the zone (wall clock). Count it;
   keep it if it is after `now`; stop when the limit is reached, or when `count` rings have been
   counted.

**The day's times**: daily with `byTime`: those; minutely/hourly: `time`, `time + step`, … while
`<= endTime` (step = interval minutes or interval×60), so the window repeats every day it runs; else:
`time`.

**A day matches** (never before `anchor`):

- minutely/hourly: every day.
- daily: `(d - anchor) in days % interval == 0`.
- weekly: `d`'s weekday is in `byDay`, and `(monday(d) - monday(anchor)) in weeks % interval == 0`,
  where `monday(x)` is the Monday of x's week (weeks start on Monday, always). Every other week
  (每隔一周) is `interval: 2`, counted from the anchor's week.
- monthly: `d`'s day of month is `byMonthDay`, and `(months from anchor's month to d's) % interval
  == 0`. Months without that day (31st in April) are skipped, not moved.

**Holiday policy** (on the ring's own date):

| policy | region CN | region "" |
|---|---|---|
| `none` | rings | rings |
| `skipHoliday` (节假日不响) | not on a listed `holiday` day | rings (no local data) |
| `workdayOnly` (仅工作日) | listed `workday` days, plus Mon-Fri not listed as `holiday` | Mon-Fri |
| `followStatutory` (按调休) | same as `workdayOnly` | same as `workdayOnly` |

`count` counts rings that happen: a day skipped by the policy does not use one up.

Times are wall-clock: a daily 08:00 alarm in New York rings at 08:00 on both sides of a DST change. A
wall time that does not exist (in a spring-forward gap) is whatever the platform's date-from-components
gives; mainland China has no DST.

## Holidays

`holiday.ForRegion("CN")` (`internal/holiday`) embeds `data/<year>.json`, copied unchanged from
[NateScarlet/holiday-cn](https://github.com/NateScarlet/holiday-cn) (each file names its State
Council notice in `papers`); 2025 and 2026 now. To add a year, drop its file into
`internal/holiday/data/`. A date the data does not list is an ordinary day: Mon-Fri work, weekends
rest, so a year without data (2027 today) treats 国庆 like any week until its file is added.

`HolidayService.GetHolidays(year, region)` (MCP `get_holidays`): year 0 is this year, region empty is
CN, any other region is refused. It returns `days` (`{date, kind: "holiday"|"workday", name}`, by
date; `holiday` is a day off, `workday` a weekend day worked, 调休补班), `availableYears`, and a
`version` that changes with the data, so the app can cache the days and refetch when it moves.

## API

gRPC (`api/noted/v1/noted.proto`): `AlarmService` `CreateAlarm`, `GetAlarm`, `UpdateAlarm`
(`update_mask`: `label`, `mode`, `enabled`, `schedule` (replaced whole), `holiday_policy`, `region`,
`note`, `project_id`, `space`, `original_text`), `DeleteAlarm`, `ListAlarms` (`enabled`, `space`,
`project_id` filters), `GetAlarmsVersion`; `HolidayService` `GetHolidays`. Enum-like fields are the
strings above. `enabled` and `region` are `optional`: unset on create means on, and the default
region. Space is inherited like a task's: given, else the project's, else life. Deleting a project
keeps its alarms and takes them out of it.

MCP: `create_alarm`, `list_alarms` (`enabled` filter), `get_alarm`, `update_alarm` (incl. `enabled`;
`schedule` replaces the whole schedule), `delete_alarm` (`{deleted, kind: "alarm"}`), `get_holidays`.
Top-level arguments are snake_case like the other tools (`holiday_policy`, `project_id`,
`original_text`); `schedule` has exactly the shape above, so a model can copy it from `get_alarm`,
change a field and send it back. The server resolves nothing relative: the model turns 明天 /
下周一 into dates first. The tool descriptions steer 闹钟/叫我/准时响/必须提醒到 to `mode: alarm` and
"只要通知" to `mode: reminder`.

Also: the export's `noted.json` has an `alarms` list (schedule in the shape above); the CortexDB sync
writes each alarm as kind `alarm` (entity type `noted_alarm`, `part_of` its project), its rule in
words and its next rings as a dated snapshot marked derived (`下次：截至 <date>，接下来 …（推算快照，以
noted 为准）`).

## Fixtures

[`alarm-fixtures.json`](alarm-fixtures.json) is self-contained:

```jsonc
{
  "windowDays": 180, "maxOccurrences": 200, "defaultEndTime": "22:00",
  "holidays": { "CN": [ { "date": "2026-10-01", "kind": "holiday", "name": "国庆节" }, … ] }, // all of 2026
  "cases": [ {
    "name": "weekly-biweekly", "description": "每隔一周的周一 15:15 接三宝",
    "now": "2026-10-13T10:00:00+08:00",
    "alarm": { "schedule": {…}, "holidayPolicy": "none", "region": "CN" }, // "enabled": false on some
    "limit": 5,
    "expected": ["2026-10-26T15:15:00+08:00", …], // the first `limit` rings after now
    "total": 12                                    // all rings of one expansion (cap 200)
  }, … ]
}
```

Build the calendar from `holidays.CN` (used only when the case's region is CN), run the engine with
`now` and `limit`, and compare `expected` as instants; run it again without a limit and compare the
count with `total`. Alarms are written in canonical form. The cases cover one-shots (future, past,
exactly now, policy ignored), disabled, daily (interval 2 and 3, future anchor, until, count),
byTime, every holiday policy around 国庆 2026 including the 调休 Sunday 09-20 and Saturday 10-10, no
region, a year beyond the data, biweekly weekly (anchor on and mid-week), weekends, weekly with
holidays, monthly (15th, 31st, every 2 months), minutely with endTime, hourly with the default end,
multi-day hourly with until and workdays, an expired hourly, the 200 cap, count with skipped
holidays, and a DST change. `internal/alarm` runs every case (`go test ./internal/alarm`); the
SuperAI iOS engine runs the same file.

## Changes from SmartAlarm

The engine follows `ScheduleEngine.swift` and `HolidaySnapshot` except where those were
nondeterministic or wrong for a stored definition:

- **until and count apply to every frequency.** The Swift engine ignored both for daily/weekly/monthly
  and counted `count` from "now" for minutely/hourly. Here `count` counts rings from the anchor.
- **Nothing rings before the anchor.** Swift only checked it for daily and interval>1 weekly; weekly
  interval 1 and monthly could ring before a future anchor.
- **Weekly without byDay rings on the anchor's weekday.** Swift matched every day of the week.
- **Monthly honours interval** (Swift ignored it) and defaults `byMonthDay` to the anchor's day
  (Swift never rang without it).
- **Weeks start on Monday** for every-N-weeks, not on the device locale's first weekday.
- **minutely/hourly run in a daily window** from `time` to `endTime` (default 22:00) on the anchor day,
  or every day through `until`/`count`. Swift strode from "now" (ignoring the anchor and time), ended
  at midnight by default (its comment said 22:00), and stretched to at least three rings past the
  end, which could ring at 01:00 and 03:00. A definition kept on a server has to mean the same thing
  on every read.
- **byTime is daily only**; Swift also applied it to weekly/monthly. Several times on given weekdays
  are one alarm per time.
- **`time` is required** for recurring alarms (Swift fell back to the anchor's clock time; our anchor
  is a date).
- Unchanged: the 180-day window and 200 cap (Swift's window also counted days, today through
  today+180), strictly-after-now, one-shots ignoring holiday policies, `followStatutory` behaving like
  `workdayOnly`, and outside CN: `skipHoliday` rings as usual, workdays are Mon-Fri.
