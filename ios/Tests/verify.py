#!/usr/bin/env python3
"""Checks what the UI tests left on the real server. Usage: verify.py export|objective|holding
Needs NOTED_ADDR and NOTED_TOKEN in the environment (and grpcurl)."""
import datetime as dt, json, os, subprocess, sys, zipfile

ADDR, TOKEN = os.environ["NOTED_ADDR"], os.environ["NOTED_TOKEN"]

def rpc(method, body="{}"):
    out = subprocess.run(["grpcurl", "-plaintext", "-H", f"authorization: Bearer {TOKEN}", "-d", body, ADDR, method],
                         capture_output=True, text=True, check=True).stdout
    return json.loads(out) if out.strip() else {}

def export():
    z = zipfile.ZipFile("/tmp/noted-ui-export.zip")
    names = z.namelist()
    d = json.loads(z.read("noted.json"))
    titles = {n["title"] for n in d["notes"]}
    assert {"周会纪要", "旅行清单"} <= titles, titles
    assert len(d["events"]) >= 3 and any(p["title"] == "菲律宾旅行" for p in d["projects"])
    assert any(n.startswith("notes/") and n.endswith("旅行清单.md") for n in names), names
    assert "BEGIN:VEVENT" in z.read("calendar.ics").decode()
    assert "symbol,currency,time,side" in z.read("trades.csv").decode()

def objective():
    o = rpc("noted.v1.ObjectiveService/ListObjectives")["objectives"][0]
    assert o["title"] == "减肥" and len(o["goals"]) == 5, o
    assert abs(o["progress"]["metricCurrent"] - 73.5) < 1e-9, o["progress"]
    assert any(g["title"] == "游泳" and g["progress"]["done"] == 1 for g in o["goals"]), o["goals"]

def holding():
    h = rpc("noted.v1.HoldingService/ListHoldings")["holdings"][0]
    assert h["symbol"] == "QQQM" and h["dcaDay"] == 15 and h["dcaEventId"], h
    assert abs(h["position"]["shares"] - 13) < 1e-9 and abs(h["lastPrice"] - 170) < 1e-9, h
    now = dt.datetime.now(dt.timezone.utc)
    fmt = "%Y-%m-%dT%H:%M:%SZ"
    evs = rpc("noted.v1.CalendarService/ListEvents", json.dumps({"from": now.strftime(fmt), "to": (now + dt.timedelta(days=62)).strftime(fmt)}))
    titles = [o["event"]["title"] for o in evs.get("occurrences", [])]
    assert titles.count("定投 QQQM") >= 2, titles

globals()[sys.argv[1]]()
