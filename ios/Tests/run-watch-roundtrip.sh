#!/bin/bash
# The phone and the watch, paired simulators, against a real noted: finish a task on the watch and check the server.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"; ROOT="$(cd "$HERE/../.." && pwd)"
WORK="${NOTED_TEST_WORK:-$(mktemp -d)}"
PHONE="${NOTED_PHONE:-15160279-1157-4011-B3D6-A9121772CBC1}"; WATCH="${NOTED_WATCH:-858E6559-9EEB-4802-B81D-22059706107C}"
PORT=43961; DD="$WORK/dd"; SIGN=(CODE_SIGN_IDENTITY=- CODE_SIGNING_ALLOWED=YES)
trap '[ -n "${SP:-}" ] && kill $SP 2>/dev/null' EXIT
cd "$HERE/.." && xcodegen generate >/dev/null
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$WORK/noted" ./cmd/noted) || exit 1
export NOTED_DATA_DIR="$WORK/data"; mkdir -p "$NOTED_DATA_DIR"
TOKEN=$("$WORK/noted" user add watcher 2>&1 | grep -o 'noted_[0-9a-f]*' | head -1)
NOTED_LISTEN=127.0.0.1:$PORT "$WORK/noted" serve >"$WORK/server.log" 2>&1 & SP=$!; sleep 2
g() { grpcurl -plaintext -H "authorization: Bearer $TOKEN" -d "$2" 127.0.0.1:$PORT "$1"; }
TITLE="WATCH done $RANDOM"
DUE=$(python3 -c 'import datetime as d;print((d.datetime.now(d.timezone.utc)+d.timedelta(minutes=20)).strftime("%Y-%m-%dT%H:%M:%SZ"))')
g noted.v1.CalendarService/CreateTask "{\"task\":{\"title\":\"$TITLE\",\"due_time\":\"$DUE\"}}" >/dev/null

OID=$(g noted.v1.ObjectiveService/CreateObjective '{"objective":{"title":"WATCH plan","metric_unit":"kg","metric_start":75,"metric_target":68}}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
g noted.v1.GoalService/CreateGoal "{\"goal\":{\"title\":\"WATCHswim\",\"period\":\"GOAL_PERIOD_WEEK\",\"target\":2,\"unit\":\"次\",\"objective_id\":\"$OID\"}}" >/dev/null

# build and put both apps on the paired simulators; the watch app rides along with the phone app
xcodebuild build-for-testing -project Noted.xcodeproj -scheme Noted -destination "platform=iOS Simulator,id=$PHONE" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 | grep -E "error:|TEST BUILD" 
xcodebuild build-for-testing -project Noted.xcodeproj -scheme NotedWatch -destination "platform=watchOS Simulator,id=$WATCH" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 | grep -E "error:|TEST BUILD"
xcrun simctl boot "$PHONE" 2>/dev/null; xcrun simctl boot "$WATCH" 2>/dev/null
# the pair must be connected before the phone app starts talking to the watch
for i in $(seq 1 60); do
  xcrun simctl list pairs | grep -A2 "(active, connected)" | grep -q "$WATCH" && break
  sleep 3
done
sleep 5
xcrun simctl install "$PHONE" "$(find "$DD" -name Noted.app -path '*iphonesimulator*' | head -1)"
SIMCTL_CHILD_NOTED_PROVISIONAL_NOTIFICATIONS=1 SIMCTL_CHILD_NOTED_HOST=127.0.0.1 SIMCTL_CHILD_NOTED_PORT=$PORT SIMCTL_CHILD_NOTED_TOKEN=$TOKEN SIMCTL_CHILD_NOTED_MODE=all \
  xcrun simctl launch --terminate-running-process "$PHONE" cn.superleo.noted >/dev/null
sleep 8

TEST_RUNNER_NOTED_WATCH_TASK="$TITLE" TEST_RUNNER_NOTED_WATCH_DIM="WATCHswim" xcodebuild test-without-building -project Noted.xcodeproj -scheme NotedWatch \
  -only-testing:NotedWatchUITests/WatchRoundTripUITests -destination "platform=watchOS Simulator,id=$WATCH" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 \
  | grep -E "error:|Test Case.*(passed|failed)|TEST (SUCCEEDED|FAILED)"
RC=${PIPESTATUS[0]}
if g noted.v1.CalendarService/ListTasks '{"filter":"FILTER_COMPLETED"}' | grep -q "$TITLE"; then echo "  PASS  the server shows the task finished"; else echo "  FAIL  the server does not show the task finished"; RC=1; fi
if g noted.v1.GoalService/ListGoals '{}' | python3 -c 'import sys,json;gs=json.load(sys.stdin)["goals"];assert any(x["title"]=="WATCHswim" and x["progress"]["done"]==1 for x in gs), gs' 2>/dev/null; then echo "  PASS  the server counted the check-in made on the watch"; else echo "  FAIL  the server has no check-in for the dimension"; RC=1; fi
exit $RC
