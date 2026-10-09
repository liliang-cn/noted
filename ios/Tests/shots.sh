#!/bin/bash
# Captures the app's screens from the simulator against a seeded real noted server.
#   ios/Tests/shots.sh <outdir>
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"; ROOT="$(cd "$HERE/../.." && pwd)"
OUT="${1:-/tmp/noted-shots}"; mkdir -p "$OUT"
WORK="$(mktemp -d)"; PORT=43921; LLM=43931
DEV="iPhone 18 Pro"; DD="$WORK/dd"
trap '[ -n "${SP:-}" ] && kill $SP 2>/dev/null; [ -n "${LP:-}" ] && kill $LP 2>/dev/null' EXIT
cd "$HERE/.." && xcodegen generate >/dev/null || exit 1
xcodebuild build -project Noted.xcodeproj -scheme Noted -destination "platform=iOS Simulator,name=$DEV" -derivedDataPath "$DD" CODE_SIGN_IDENTITY=- CODE_SIGNING_ALLOWED=YES 2>&1 | grep -E "error:|BUILD" 
APP=$(find "$DD/Build/Products" -name Noted.app -path '*iphonesimulator*' | head -1)
xcrun simctl install "$DEV" "$APP" || exit 1
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$WORK/noted" ./cmd/noted) || exit 1
python3 "$HERE/fake_llm.py" $LLM >/dev/null 2>&1 & LP=$!
export NOTED_DATA_DIR="$WORK/data"; mkdir -p "$NOTED_DATA_DIR"
TOKEN=$("$WORK/noted" user add shots 2>&1 | grep -o 'noted_[0-9a-f]*' | head -1)
TZNAME=$(readlink /etc/localtime | sed "s|.*/zoneinfo/||")
NOTED_TIME_ZONE="${TZNAME:-UTC}" NOTED_AI_ENABLED=true NOTED_AI_LLM_BASE_URL="${SHOTS_LLM_URL:-http://127.0.0.1:$LLM/v1}" NOTED_AI_LLM_API_KEY="${SHOTS_LLM_KEY:-fake}" NOTED_AI_LLM_MODEL="${SHOTS_LLM_MODEL:-fake-model}" \
  NOTED_LISTEN=127.0.0.1:$PORT "$WORK/noted" serve >"$WORK/server.log" 2>&1 & SP=$!
sleep 2
A=127.0.0.1:$PORT
g() { grpcurl -plaintext -H "authorization: Bearer $TOKEN" -d "$2" $A "$1"; }
local_at() { python3 -c "import datetime as d,sys;h,m=map(int,sys.argv[1].split(':'));t=d.datetime.now().astimezone().replace(hour=h,minute=m,second=0,microsecond=0);print(t.astimezone(d.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'))" "$1"; }


g noted.v1.ProjectService/CreateProject '{"project":{"title":"菲律宾旅行","pinned":true,"space":"SPACE_LIFE","start_time":"2026-11-20T00:00:00Z"}}' >/dev/null
g noted.v1.ProjectService/CreateProject '{"project":{"title":"Q4 产品发布","pinned":true,"space":"SPACE_WORK","due_time":"2026-10-20T00:00:00Z"}}' >/dev/null
PID=$(g noted.v1.ProjectService/ListProjects '{"space":"SPACE_LIFE"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["projects"][0]["id"])')
g noted.v1.CalendarService/CreateEvent "{\"event\":{\"title\":\"团队站会\",\"start_time\":\"$(local_at 09:00)\",\"end_time\":\"$(local_at 09:30)\",\"space\":\"SPACE_WORK\"}}" >/dev/null
g noted.v1.CalendarService/CreateEvent "{\"event\":{\"title\":\"牙医复诊\",\"start_time\":\"$(local_at 14:00)\",\"end_time\":\"$(local_at 15:00)\",\"space\":\"SPACE_LIFE\"}}" >/dev/null
g noted.v1.CalendarService/CreateTask "{\"task\":{\"title\":\"交房租\",\"due_time\":\"$(local_at 10:00)\",\"space\":\"SPACE_LIFE\"}}" >/dev/null
g noted.v1.CalendarService/CreateTask "{\"task\":{\"title\":\"UIT work only\",\"due_time\":\"$(local_at 11:00)\",\"space\":\"SPACE_WORK\"}}" >/dev/null
g noted.v1.CalendarService/CreateTask "{\"task\":{\"title\":\"办理签证\",\"project_id\":\"$PID\",\"space\":\"SPACE_LIFE\",\"due_time\":\"2026-10-18T02:00:00Z\"}}" >/dev/null
g noted.v1.CalendarService/CreateTask "{\"task\":{\"title\":\"准备酒店\",\"project_id\":\"$PID\",\"space\":\"SPACE_LIFE\"}}" >/dev/null
g noted.v1.CalendarService/CreateTask "{\"task\":{\"title\":\"订机票\",\"project_id\":\"$PID\",\"space\":\"SPACE_LIFE\",\"completed\":true}}" >/dev/null
g noted.v1.CalendarService/CreateEvent "{\"event\":{\"title\":\"航班 CTU → MNL\",\"project_id\":\"$PID\",\"space\":\"SPACE_LIFE\",\"start_time\":\"2026-11-20T01:00:00Z\",\"end_time\":\"2026-11-20T05:00:00Z\"}}" >/dev/null
g noted.v1.NoteService/CreateNote "{\"note\":{\"title\":\"行程想法\",\"content\":\"先去巴拉望,留一天机动\",\"project_id\":\"$PID\",\"space\":\"SPACE_LIFE\"}}" >/dev/null
g noted.v1.NoteService/CreateNote '{"note":{"title":"周会纪要","tags":["work"],"content":"讨论了季度报告和发布节奏","pinned":true,"space":"SPACE_WORK"}}' >/dev/null
g noted.v1.NoteService/CreateNote '{"note":{"title":"旅行清单","tags":["travel"],"content":"护照、充电器、防晒","space":"SPACE_LIFE"}}' >/dev/null
g noted.v1.GoalService/CreateGoal '{"goal":{"title":"运动","period":"GOAL_PERIOD_WEEK","target":3,"unit":"次","space":"SPACE_LIFE"}}' >/dev/null
g noted.v1.GoalService/CreateGoal '{"goal":{"title":"德语","period":"GOAL_PERIOD_WEEK","target":140,"unit":"分钟","space":"SPACE_LIFE","counter_unit":"课时","counter_target":40}}' >/dev/null
OID=$(g noted.v1.ObjectiveService/CreateObjective "{\"objective\":{\"title\":\"减肥\",\"metric_name\":\"体重\",\"metric_unit\":\"kg\",\"metric_start\":75,\"metric_target\":68,\"start_time\":\"$(date -u -v-30d +%Y-%m-%dT00:00:00Z)\",\"due_time\":\"$(date -u -v+60d +%Y-%m-%dT00:00:00Z)\"}}" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
for d in "游泳:2:次" "跑步:3:次" "健身:3:次" "轻食:10:餐" "控糖:7:天"; do IFS=: read t n u <<<"$d"; GID=$(g noted.v1.GoalService/CreateGoal "{\"goal\":{\"title\":\"$t\",\"period\":\"GOAL_PERIOD_WEEK\",\"target\":$n,\"unit\":\"$u\",\"objective_id\":\"$OID\"}}" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])'); [ "$t" = 游泳 ] && g noted.v1.GoalService/RecordCheckIn "{\"goal_id\":\"$GID\",\"amount\":1}" >/dev/null; done
for v in 75 74.6 74.1 73.8 73.2; do g noted.v1.ObjectiveService/RecordMeasurement "{\"objective_id\":\"$OID\",\"value\":$v}" >/dev/null; sleep 0.05; done
HID=$(g noted.v1.HoldingService/CreateHolding '{"holding":{"symbol":"VOO","name":"Vanguard S&P 500","dca_day":15,"dca_amount":500,"last_price":720}}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
g noted.v1.HoldingService/RecordTrade "{\"holding_id\":\"$HID\",\"trade\":{\"side\":\"buy\",\"shares\":14,\"price\":699.19,\"time\":\"$(date -u -v-40d +%Y-%m-%dT12:00:00Z)\"}}" >/dev/null
g noted.v1.HoldingService/CreateHolding '{"holding":{"symbol":"QQQM","name":"Invesco NASDAQ 100","dca_day":15,"dca_amount":300}}' >/dev/null
shot() { # name, then KEY=VAL env for the app
  local name=$1; shift
  xcrun simctl terminate "$DEV" cn.superleo.noted 2>/dev/null
  env ${SHOT_NOHOST:+SIMCTL_CHILD_UNUSED=1} ${SHOT_NOHOST:-SIMCTL_CHILD_NOTED_HOST=127.0.0.1} SIMCTL_CHILD_NOTED_PORT=$PORT ${SHOT_NOHOST:-SIMCTL_CHILD_NOTED_TOKEN=$TOKEN} SIMCTL_CHILD_NOTED_PROVISIONAL_NOTIFICATIONS=1 \
    $(for kv in "$@"; do echo "SIMCTL_CHILD_$kv"; done) xcrun simctl launch "$DEV" cn.superleo.noted >/dev/null
  sleep "${SHOT_WAIT:-5}"
  xcrun simctl io "$DEV" screenshot "$OUT/$name.png" >/dev/null 2>&1
  echo "  $name"
}
xcrun simctl status_bar "$DEV" override --time 9:41 --batteryState charged --batteryLevel 100 --cellularBars 4 >/dev/null 2>&1
shot main NOTED_RESET=1
shot focus-work NOTED_MODE=work
shot focus-life NOTED_MODE=life
shot calendar NOTED_TAB=calendar
shot projects NOTED_TAB=projects
shot project-detail NOTED_SHEET=project
shot notes NOTED_TAB=notes
shot goals NOTED_TAB=projects NOTED_PTAB=1
shot holdings NOTED_TAB=projects NOTED_PTAB=2
shot suggestions NOTED_SHEET=suggestions
shot review NOTED_SHEET=review
shot ask NOTED_SHEET=ask
shot settings NOTED_SHEET=settings
shot aisettings NOTED_SHEET=aisettings
shot paywall NOTED_SHEET=paywall
shot themeeditor NOTED_SHEET=themeeditor NOTED_PRO=1
shot modules NOTED_SHEET=modules NOTED_PRO=1
shot extract NOTED_SHEET=extract
SHOT_NOHOST=1 shot connect NOTED_RESET=1
xcrun simctl status_bar "$DEV" clear >/dev/null 2>&1
