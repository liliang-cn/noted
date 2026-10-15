#!/bin/bash
# Runs the iOS unit tests and the UI tests against a real noted server and a stand-in model.
#   ios/Tests/run-ui-tests.sh [unit|ui|all] [Class or Class/test]     (default: all)
# The server is fresh on every run and seeded with the prototype's sample data (Tests/seed.py).
# NOTED_TEST_WORK=<dir> keeps the build between runs (the server data in it is wiped each time).
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
WORK="${NOTED_TEST_WORK:-$(mktemp -d)}"
PORT=43901; LLM=43911; HTTP=43921
DEVICE="${NOTED_TEST_DEVICE:-iPhone 18 Pro}"
DEST="platform=iOS Simulator,name=$DEVICE"
DD="$WORK/dd"
WHAT="${1:-all}"
ONLY="${2:-}"   # e.g. TodayUITests or TodayUITests/testSwipeRightCompletesATodo,NotesUITests: part of the UI tests
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
lsof -ti tcp:$PORT -sTCP:LISTEN | xargs kill 2>/dev/null
lsof -ti tcp:$LLM -sTCP:LISTEN | xargs kill 2>/dev/null
lsof -ti tcp:$HTTP -sTCP:LISTEN | xargs kill 2>/dev/null
python3 "$HERE/fake_llm.py" $LLM >/dev/null 2>&1 & LLM_PID=$!
export NOTED_DATA_DIR="$WORK/data"; rm -rf "$NOTED_DATA_DIR"; mkdir -p "$NOTED_DATA_DIR"
TOKEN=$("$WORK/noted" user add tester 2>&1 | grep -o 'noted_[0-9a-f]*' | head -1)
TZNAME=$(readlink /etc/localtime | sed "s|.*/zoneinfo/||")
export NOTED_TIME_ZONE="${TZNAME:-UTC}"
NOTED_AI_ENABLED=true NOTED_AI_LLM_BASE_URL=http://127.0.0.1:$LLM/v1 NOTED_AI_LLM_API_KEY=fake NOTED_AI_LLM_MODEL=fake-model \
  NOTED_LISTEN=127.0.0.1:$PORT NOTED_MCP_LISTEN=127.0.0.1:$HTTP "$WORK/noted" serve >"$WORK/server.log" 2>&1 & SERVER_PID=$!
A=127.0.0.1:$PORT
for _ in $(seq 50); do nc -z 127.0.0.1 $PORT 2>/dev/null && break; sleep 0.2; done
export NOTED_ADDR=$A NOTED_TOKEN=$TOKEN
python3 "$HERE/seed.py" || exit 1
g() { grpcurl -plaintext -H "authorization: Bearer $TOKEN" -d "$2" $A "$1"; }

echo "== UI tests =="
rm -f /tmp/noted-ui-pending.json /tmp/noted-ui-export.zip
TARGETS=(-only-testing:NotedUITests)
if [ -n "$ONLY" ]; then TARGETS=(); for t in ${ONLY//,/ }; do TARGETS+=(-only-testing:"NotedUITests/$t"); done; fi
TEST_RUNNER_NOTED_TOKEN=$TOKEN TEST_RUNNER_NOTED_PORT=$PORT TEST_RUNNER_NOTED_MCP_PORT=$HTTP \
  xcodebuild test -project Noted.xcodeproj -scheme Noted "${TARGETS[@]}" \
  -destination "$DEST" -derivedDataPath "$DD" "${SIGN[@]}" 2>&1 | tee "$WORK/ui-tests.log" \
  | grep -E "error:|Test Case.*(passed|failed|skipped)|Executed [0-9]+ tests|TEST (SUCCEEDED|FAILED)|: error: " | sort -u
[ "${PIPESTATUS[0]}" = 0 ] || fail=1
[ -n "$ONLY" ] && exit $fail

echo "== what the server ended up with =="
check() { if eval "$2"; then echo "  PASS  $1"; else echo "  FAIL  $1"; fail=1; fi; }
check "the app's export zip holds the seeded data" "python3 $HERE/verify.py export"
check "to-dos made and edited in the app are on the server" "python3 $HERE/verify.py tasks"
check "the sitting logged in the app is on the server" "python3 $HERE/verify.py sittings"
check "the weight reading is on the server" "python3 $HERE/verify.py objective"
check "the buy is on the server" "python3 $HERE/verify.py holding"
check "projects: linked, renamed, deleted keeping its to-do" "python3 $HERE/verify.py projects"
check "notes: edited, pinned, deleted, referenced" "python3 $HERE/verify.py notes"
check "the event title edit was saved, the deleted one is gone" "python3 $HERE/verify.py events"
check "the token made in the app was revoked" "python3 $HERE/verify.py tokens"
echo
[ $fail = 0 ] && echo "ALL PASSED" || echo "SOME FAILED (server log: $WORK/server.log)"
exit $fail
