#!/usr/bin/env python3
"""Checks what the full UI test run left on the real server (see Tests/seed.py for the start).
Usage: verify.py export|tasks|sittings|objective|holding|projects|notes|events|tokens
Needs NOTED_ADDR and NOTED_TOKEN in the environment (and grpcurl)."""
import datetime as dt, json, os, subprocess, sys, zipfile

ADDR, TOKEN = os.environ["NOTED_ADDR"], os.environ["NOTED_TOKEN"]


def rpc(method, body="{}"):
    out = subprocess.run(["grpcurl", "-plaintext", "-H", f"authorization: Bearer {TOKEN}", "-d", body, ADDR, f"noted.v1.{method}"],
                         capture_output=True, text=True, check=True).stdout
    return json.loads(out) if out.strip() else {}


def local(ts):
    return dt.datetime.fromisoformat(ts.replace("Z", "+00:00")).astimezone()


def tasks():
    return rpc("CalendarService/ListTasks", json.dumps({"filter": "FILTER_ALL", "page_size": 200})).get("tasks", [])


def projects():
    return {p["title"]: p for p in rpc("ProjectService/ListProjects", '{"include_archived": true}').get("projects", [])}


def notes():
    return {n["title"]: n for n in rpc("NoteService/ListNotes", '{"page_size": 200}').get("notes", [])}


def by_title(ts, title):
    return [t for t in ts if t["title"] == title]


def export():
    z = zipfile.ZipFile("/tmp/noted-ui-export.zip")
    names = z.namelist()
    d = json.loads(z.read("noted.json"))
    titles = {n["title"] for n in d["notes"]}
    assert {"周会纪要", "旅行清单", "德语单词"} <= titles, titles
    assert len(d["events"]) >= 3 and any(p["title"] == "菲律宾旅行" for p in d["projects"])
    assert any(n.startswith("notes/") and n.endswith("旅行清单.md") for n in names), names
    assert "BEGIN:VEVENT" in z.read("calendar.ics").decode()
    assert "symbol,currency,time,side" in z.read("trades.csv").decode()


def check_tasks():
    ts = tasks()
    tomorrow = (dt.datetime.now().astimezone() + dt.timedelta(days=1)).date()
    trip = projects()["菲律宾旅行"]["id"]
    [e] = by_title(ts, "UIT 编辑")
    due = local(e["dueTime"])
    assert e.get("projectId") == trip and e.get("space") == "SPACE_WORK", e
    assert due.date() == tomorrow and (due.hour, due.minute) != (0, 0), e
    assert (due - local(e["remindTime"])) == dt.timedelta(minutes=15), e
    [p] = by_title(ts, "UIT 推迟")
    assert local(p["dueTime"]).date() == tomorrow, p
    [c] = by_title(ts, "UIT 滑动完成")
    assert c.get("completed"), c
    assert not by_title(ts, "UIT 滑动删除") and not by_title(ts, "UIT 页删除")
    [k] = by_title(ts, "UIT 留下")
    assert not k.get("projectId"), k
    made = [t for t in ts if t["title"].startswith("UIT 新待办")]
    assert made and all(t.get("remindTime") for t in made), made
    weekly = [t for t in ts if t["title"].startswith("UIT 每周")]
    assert len(weekly) == 2 and sum(bool(t.get("completed")) for t in weekly) == 1, weekly
    assert all(t.get("rrule", "").startswith("FREQ=WEEKLY") for t in weekly), weekly
    gap = abs(local(weekly[0]["dueTime"]) - local(weekly[1]["dueTime"]))
    assert gap == dt.timedelta(days=7), weekly


def sittings():
    ts = tasks()
    [t] = by_title(ts, "UIT 记录")
    w = rpc("CalendarService/ListTaskWork", json.dumps({"task_id": t["id"]})).get("entries", [])
    assert len(w) == 1 and w[0]["minutes"] == 30 and w[0]["note"] == "UIT 写了开头", w
    assert t.get("progress", 0) == w[0]["progress"] > 0, (t, w)
    [d] = by_title(ts, "UIT 删记录")
    assert not rpc("CalendarService/ListTaskWork", json.dumps({"task_id": d["id"]})).get("entries"), d


def objective():
    os_ = {o["title"]: o for o in rpc("ObjectiveService/ListObjectives")["objectives"]}
    o = os_["减肥"]
    assert abs(o["progress"]["metricCurrent"] - 78.2) < 1e-9, o["progress"]
    big = [o for t, o in os_.items() if t.startswith("UIT 大目标")]
    assert big and big[0]["metricUnit"] == "kg" and any(g["title"] == "UIT 游泳" for g in big[0]["goals"]), big


def holding():
    hs = {h["symbol"]: h for h in rpc("HoldingService/ListHoldings")["holdings"]}
    p = hs["VOO"]["position"]
    assert abs(p["shares"] - 12) < 1e-9 and p["trades"] == 4, p


def check_projects():
    ps = projects()
    assert "UIT 新名字" in ps and "UIT 改名" not in ps and "UIT 删项目" not in ps, list(ps)
    ts = tasks()
    link = ps["UIT 关联"]["id"]
    assert by_title(ts, "UIT 待关联")[0].get("projectId") == link
    assert not by_title(ts, "UIT 移出")[0].get("projectId")


def check_notes():
    ns = notes()
    assert "自动保存的第二行" in ns["UIT 编辑笔记"]["content"], ns["UIT 编辑笔记"]
    assert ns["UIT 置顶我"].get("pinned"), ns["UIT 置顶我"]
    assert "UIT 删除我" not in ns
    assert "- [x] 买水" in ns["UIT 清单"]["content"], ns["UIT 清单"]
    trip = projects()["菲律宾旅行"]["id"]
    assert f"noted://project/{trip}" in ns["UIT 引用"]["content"], ns["UIT 引用"]


def events():
    now = dt.datetime.now(dt.timezone.utc)
    fmt = "%Y-%m-%dT%H:%M:%SZ"
    evs = rpc("CalendarService/ListEvents", json.dumps({"from": (now - dt.timedelta(days=2)).strftime(fmt), "to": (now + dt.timedelta(days=2)).strftime(fmt)}))
    titles = [o["event"]["title"] for o in evs.get("occurrences", [])]
    assert "UIT 日程改名" in titles and "UIT 日程编辑" not in titles and "UIT 日程删除" not in titles, titles


def tokens():
    ts = rpc("AccountService/ListTokens").get("tokens", [])
    assert not [t for t in ts if t.get("label", "").startswith("UIT 令牌")], ts


{"export": export, "tasks": check_tasks, "sittings": sittings, "objective": objective, "holding": holding,
 "projects": check_projects, "notes": check_notes, "events": events, "tokens": tokens}[sys.argv[1]]()
