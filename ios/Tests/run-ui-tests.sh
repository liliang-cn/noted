#!/bin/bash
# Runs the iOS unit tests and the UI tests against a real noted server and a stand-in model.
#   ios/Tests/run-ui-tests.sh [unit|ui|all]      (default: all)
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
WORK="${NOTED_TEST_WORK:-$(mktemp -d)}"
PORT=43901; LLM=43911
DEVICE="${NOTED_TEST_DEVICE:-iPhone 18 Pro}"
DEST="platform=iOS Simulator,name=$DEVICE"
DD="$WORK/dd"
WHAT="${1:-all}"
ONLY="${2:-}"   # e.g. OverviewUITests/testWeeklyReviewShowsTheWeek, to run a single UI test
SIGN=(CODE_SIGN_IDENTITY=- CODE_SIGNING_ALLOWED=YES)
fail=0

cleanup() { [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null; [ -n "${LLM_PID:-}" ] && kill "$LLM_PID" 2>/dev/null; }
trap cleanup EXIT

cd "$HERE/.." && xcodegen generate >/dev/null || exit 1

if [ "$WHAT" = "unit" ] || [ "$WHAT" = "all" ]; then
  echo "== unit tests =="
  xcodebuild test -project Noted.xcodeproj -scheme Noted -only-testing:NotedTests -destination "$DEST" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 \
    | grep -E "error:|Test Case.*failed|Executed [0-9]+ tests|TEST (SUCCEEDED|FAILED)" | sort -u
  [ "${PIPESTATUS[0]}" = 0 ] || fail=1
fi

[ "$WHAT" = "unit" ] && exit $fail

echo "== starting noted and a stand-in model =="
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$WORK/noted" ./cmd/noted) || exit 1
python3 "$HERE/fake_llm.py" $LLM >/dev/null 2>&1 & LLM_PID=$!
export NOTED_DATA_DIR="$WORK/data"; mkdir -p "$NOTED_DATA_DIR"
TOKEN=$("$WORK/noted" user add tester 2>&1 | grep -o 'noted_[0-9a-f]*' | head -1)
TZNAME=$(readlink /etc/localtime | sed "s|.*/zoneinfo/||")
NOTED_TIME_ZONE="${TZNAME:-UTC}" NOTED_AI_ENABLED=true NOTED_AI_LLM_BASE_URL=http://127.0.0.1:$LLM/v1 NOTED_AI_LLM_API_KEY=fake NOTED_AI_LLM_MODEL=fake-model \
  NOTED_LISTEN=127.0.0.1:$PORT "$WORK/noted" serve >"$WORK/server.log" 2>&1 & SERVER_PID=$!
sleep 2
A=127.0.0.1:$PORT
g() { grpcurl -plaintext -H "authorization: Bearer $TOKEN" -d "$2" $A "$1"; }
# "Today" is the local day: UTC dates would put these on yesterday for part of every day.
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
g noted.v1.NoteService/CreateNote '{"note":{"title":"周会纪要","content":"讨论了季度报告和发布节奏","pinned":true,"space":"SPACE_WORK"}}' >/dev/null
g noted.v1.NoteService/CreateNote '{"note":{"title":"旅行清单","content":"护照、充电器、防晒","space":"SPACE_LIFE"}}' >/dev/null
g noted.v1.GoalService/CreateGoal '{"goal":{"title":"运动","period":"GOAL_PERIOD_WEEK","target":3,"unit":"次","space":"SPACE_LIFE"}}' >/dev/null
g noted.v1.GoalService/CreateGoal '{"goal":{"title":"德语","period":"GOAL_PERIOD_WEEK","target":140,"unit":"分钟","space":"SPACE_LIFE","counter_unit":"课时","counter_target":40}}' >/dev/null

echo "== UI tests =="
rm -f /tmp/noted-ui-pending.json
if [ -n "$ONLY" ]; then
  TEST_RUNNER_NOTED_TOKEN=$TOKEN TEST_RUNNER_NOTED_PORT=$PORT \
    xcodebuild test -project Noted.xcodeproj -scheme Noted -only-testing:"NotedUITests/$ONLY" -destination "$DEST" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 \
    | grep -E "error:|Test Case.*(passed|failed|skipped)|TEST (SUCCEEDED|FAILED)" | sort -u
  exit "${PIPESTATUS[0]}"
fi
TEST_RUNNER_NOTED_TOKEN=$TOKEN TEST_RUNNER_NOTED_PORT=$PORT \
  xcodebuild test -project Noted.xcodeproj -scheme Noted -only-testing:NotedUITests -skip-testing:NotedUITests/ReminderUITests/testAReminderThatFiresWhileTheAppIsOpenShowsABanner \
  -destination "$DEST" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 \
  | grep -E "error:|Test Case.*(passed|failed|skipped)|Executed [0-9]+ tests|TEST (SUCCEEDED|FAILED)" | sort -u
[ "${PIPESTATUS[0]}" = 0 ] || fail=1

echo "== a reminder fires while the app is open =="
START=$(python3 -c 'import datetime as d;print((d.datetime.now(d.timezone.utc)+d.timedelta(seconds=45)).strftime("%Y-%m-%dT%H:%M:%SZ"))')
g noted.v1.CalendarService/CreateEvent "{\"event\":{\"title\":\"UIT banner\",\"start_time\":\"$START\",\"remind_before_minutes\":0}}" >/dev/null
TEST_RUNNER_NOTED_TOKEN=$TOKEN TEST_RUNNER_NOTED_PORT=$PORT TEST_RUNNER_NOTED_EXPECT_BANNER=1 \
  xcodebuild test -project Noted.xcodeproj -scheme Noted -only-testing:NotedUITests/ReminderUITests/testAReminderThatFiresWhileTheAppIsOpenShowsABanner \
  -destination "$DEST" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 \
  | grep -E "error:|Test Case.*(passed|failed|skipped)|TEST (SUCCEEDED|FAILED)" | sort -u
[ "${PIPESTATUS[0]}" = 0 ] || fail=1

echo "== what the server ended up with =="
check() { if eval "$2"; then echo "  PASS  $1"; else echo "  FAIL  $1"; fail=1; fi; }
check "theme preference was written" "g noted.v1.PreferenceService/GetPreference '{\"key\":\"ui.theme\"}' | grep -q base"
check "tasks made in the app exist"  "g noted.v1.CalendarService/ListTasks '{\"filter\":\"FILTER_ALL\",\"page_size\":200}' | grep -q 'UIT '"
check "the assistant's staged task was written only after accepting" "g noted.v1.CalendarService/ListTasks '{\"filter\":\"FILTER_ALL\",\"page_size\":200}' | grep -q '买牛奶'"
check "notes made in the app were deleted again" "! g noted.v1.NoteService/ListNotes '{\"page_size\":200}' | grep -q 'UIT note'"
export NOTED_ADDR=$A NOTED_TOKEN=$TOKEN
check "the app's export zip holds the seeded data" "python3 $HERE/verify.py export"
check "the objective, its dimensions and the reading are on the server" "python3 $HERE/verify.py objective"
check "the holding, its trades and its monthly reminder are on the server" "python3 $HERE/verify.py holding"
check "AI access is back on for both spaces" "g noted.v1.AIService/GetAIAccess '{}' | grep -q allowWork"
echo
[ $fail = 0 ] && echo "ALL PASSED" || echo "SOME FAILED (server log: $WORK/server.log)"
exit $fail
