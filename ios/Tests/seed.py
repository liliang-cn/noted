#!/usr/bin/env python3
"""Seeds a fresh noted server with the prototype's sample data (design/prototype), plus a few
items each UI test owns (titles starting with "UIT "), so tests can change them without stepping
on each other. Dates are relative to today, in this machine's time zone.
Usage: seed.py   (needs NOTED_ADDR and NOTED_TOKEN in the environment, and grpcurl)"""
import datetime as dt, json, os, subprocess

ADDR, TOKEN = os.environ["NOTED_ADDR"], os.environ["NOTED_TOKEN"]
ZONE = os.environ.get("NOTED_TIME_ZONE", "UTC")
TODAY = dt.datetime.now().astimezone().replace(hour=0, minute=0, second=0, microsecond=0)
LIFE, WORK = "SPACE_LIFE", "SPACE_WORK"


def rpc(method, body):
    out = subprocess.run(["grpcurl", "-plaintext", "-H", f"authorization: Bearer {TOKEN}", "-d", json.dumps(body, ensure_ascii=False),
                          ADDR, f"noted.v1.{method}"], capture_output=True, text=True)
    if out.returncode != 0:
        raise SystemExit(f"{method} failed: {out.stderr.strip()}\n{json.dumps(body, ensure_ascii=False)}")
    return json.loads(out.stdout) if out.stdout.strip() else {}


def at(days=0, hm="00:00"):
    """RFC 3339 (UTC) for a local day `days` from today at HH:MM."""
    h, m = map(int, hm.split(":"))
    t = TODAY + dt.timedelta(days=days)
    t = t.replace(hour=h, minute=m)
    return t.astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def ago(minutes):
    return (dt.datetime.now(dt.timezone.utc) - dt.timedelta(minutes=minutes)).strftime("%Y-%m-%dT%H:%M:%SZ")


def project(title, space, **kw):
    return rpc("ProjectService/CreateProject", {"project": {"title": title, "space": space, **kw}})["id"]


def task(title, space=LIFE, days=None, hm="00:00", remind_before=None, **kw):
    t = {"title": title, "space": space, **kw}
    if days is not None:
        t["due_time"] = at(days, hm)
        if remind_before is not None:
            h, m = map(int, hm.split(":"))
            due = TODAY + dt.timedelta(days=days, hours=h, minutes=m) - dt.timedelta(minutes=remind_before)
            t["remind_time"] = due.astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return rpc("CalendarService/CreateTask", {"task": t})["id"]


def event(title, space, days, start, end, **kw):
    return rpc("CalendarService/CreateEvent", {"event": {"title": title, "space": space, "start_time": at(days, start),
                                                          "end_time": at(days, end), "time_zone": ZONE, **kw}})["id"]


def note(title, content, space, **kw):
    return rpc("NoteService/CreateNote", {"note": {"title": title, "content": content, "space": space, **kw}})["id"]


def goal(title, period, target, unit="times", space=LIFE, **kw):
    return rpc("GoalService/CreateGoal", {"goal": {"title": title, "period": period, "target": target, "unit": unit,
                                                    "space": space, "time_zone": ZONE, **kw}})["id"]


def work(task_id, minutes, progress, note_text, minutes_ago):
    rpc("CalendarService/LogTaskWork", {"task_id": task_id, "minutes": minutes, "progress": progress, "note": note_text, "time": ago(minutes_ago)})


# ---- projects
trip = project("菲律宾旅行", LIFE, pinned=True, notes="宿务 + 薄荷岛", start_time=at(39), due_time=at(45))
launch = project("Q4 产品发布", WORK, pinned=True, start_time=at(-27), due_time=at(19))

task("办理签证", LIFE, 6, "10:00", remind_before=1440, project_id=trip, priority="PRIORITY_MEDIUM")
task("订酒店", LIFE, project_id=trip)
task("订机票", LIFE, project_id=trip, completed=True)
event("航班 CTU→MNL", LIFE, 39, "09:00", "13:30", project_id=trip, location="天府国际机场 T1")
note("行程想法", f"关于 [@菲律宾旅行](noted://project/{trip}) 的安排。\n\n## 宿务\n- [ ] 订浮潜\n- [x] 查鲸鲨季节\n\n## 薄荷岛\n- 巧克力山\n- 眼镜猴保护区",
     LIFE, project_id=trip, tags=["travel"])

for title, d in [("需求冻结", -17), ("设计评审", -12), ("接口联调", -4)]:
    task(title, WORK, d, project_id=launch, completed=True)
task("回归测试", WORK, 8, project_id=launch, priority="PRIORITY_MEDIUM")
task("正式上线", WORK, 19, project_id=launch, priority="PRIORITY_HIGH")

# ---- today
event("团队站会", WORK, 0, "09:00", "09:30", location="线上会议")
event("牙医复诊", LIFE, 0, "14:00", "15:00", location="人民南路口腔", remind_before_minutes=60)
event("季度报告评审", WORK, 0, "16:00", "17:30", location="3F 大会议室", project_id=launch)
task("交房租", LIFE, 0, "10:00", remind_before=0, priority="PRIORITY_HIGH", rrule="FREQ=MONTHLY")
DAY = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"][TODAY.weekday()]
weekly = task("写周报", WORK, 0, "18:00", remind_before=30, rrule=f"FREQ=WEEKLY;BYDAY={DAY}")
work(weekly, 45, 20, "列提纲", 60 * 26)
work(weekly, 50, 60, "补齐数据和图表", 60 * 3)

# ---- goals
goal("健身", "GOAL_PERIOD_WEEK", 3)
german = goal("德语", "GOAL_PERIOD_DAY", 30, unit="minutes", counter_unit="课时", counter_target=40)
rpc("GoalService/RecordCheckIn", {"goal_id": german, "amount": 0, "count": 19, "time": ago(60 * 30)})

lose = rpc("ObjectiveService/CreateObjective", {"objective": {"title": "减肥", "space": LIFE, "start_time": at(-30),
            "metric_name": "体重", "metric_unit": "kg", "metric_start": 80, "metric_target": 75}})["id"]
swim = goal("游泳", "GOAL_PERIOD_WEEK", 2, objective_id=lose)
run = goal("跑步", "GOAL_PERIOD_WEEK", 3, objective_id=lose)
# Checked in earlier today, so they count in this week whatever day it is.
for g, n in [(swim, 1), (run, 2)]:
    for i in range(n):
        rpc("GoalService/RecordCheckIn", {"goal_id": g, "amount": 1, "time": ago(2 + i)})
for v, d in [(80, 30), (79.4, 22), (79.1, 14), (78.6, 6)]:
    rpc("ObjectiveService/RecordMeasurement", {"objective_id": lose, "value": v, "time": ago(60 * 24 * d)})

# ---- holdings
for sym, name, last, trades in [
    ("QQQM", "Invesco 纳斯达克100 ETF", 221, [(118, 5, 200), (58, 4, 207), (27, 3, 210.67)]),
    ("VOO", "Vanguard 标普500 ETF", 540, [(118, 4, 512), (58, 3, 522), (27, 3, 528.67)]),
    ("OKLO", "Oklo Inc.", 27.5, [(89, 20, 33), (27, 10, 27)]),
]:
    h = rpc("HoldingService/CreateHolding", {"holding": {"symbol": sym, "name": name, "currency": "USD", "last_price": last,
                                                         "dca_amount": 500, "dca_day": 15, "time_zone": ZONE}})["id"]
    for days, q, p in trades:
        rpc("HoldingService/RecordTrade", {"holding_id": h, "trade": {"side": "buy", "shares": q, "price": p, "time": ago(60 * 24 * days)}})

# ---- notes
note("周会纪要", "## 周会\n- 回归测试下周开始\n- [x] 确认发布时间\n- [ ] 更新隐私政策文案\n\n> 灰度期间盯崩溃率", WORK, pinned=True, tags=["工作"])
note("旅行清单", "## 证件\n- [x] 护照\n- [ ] 签证打印件\n\n## 随身\n- 转换插头\n- 防晒霜", LIFE, tags=["travel"])
note("德语单词", "## 第 19 课\n- die Rechnung — 账单\n- bestellen — 点餐", LIFE, tags=["德语"])

# ---- items the UI tests own (each used by one test only)
for t in ["UIT 滑动完成", "UIT 滑动删除", "UIT 推迟", "UIT 打开"]:
    task(t, LIFE, 0)
task("UIT 记录", LIFE, 0)
task("UIT 编辑", LIFE, 0)
task("UIT 页删除", LIFE, 0)
sitting = task("UIT 删记录", LIFE, 0)
work(sitting, 25, 30, "UIT 第一次", 60 * 5)
event("UIT 日程编辑", LIFE, 0, "12:00", "13:00")
event("UIT 日程删除", LIFE, 0, "20:00", "21:00")
linkp = project("UIT 关联", LIFE)
task("UIT 待关联", LIFE)
task("UIT 移出", LIFE, project_id=linkp)
gone = project("UIT 删项目", LIFE)
task("UIT 留下", LIFE, project_id=gone)
project("UIT 改名", WORK)
note("UIT 置顶我", "滑动置顶", LIFE)
note("UIT 删除我", "滑动删除", LIFE)
note("UIT 编辑笔记", "第一行", LIFE)
note("UIT 清单", "- [ ] 买水\n- [ ] 买面包", LIFE)
note("UIT 引用", "引用一个项目", LIFE)
