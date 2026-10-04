#!/usr/bin/env bash
# Cross-language conformance gate.
#
# Runs the Python agent demo offline (no network), exports its telemetry as
# OTLP protobuf, validates it with the Go engine, and asserts the outcomes
# against the golden contract in apps/agent-demo/expected_findings.json:
# the compliant path must be flawless, and the noncompliant path must be
# caught with exactly the expected rule set.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DEMO=apps/agent-demo
GOLDEN="$DEMO/expected_findings.json"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

echo "==> exporting demo telemetry (compliant + noncompliant)"
(cd "$DEMO" && uv run python -m agent_demo.evals.runner --otlp-out out/gate-good >/dev/null)
(cd "$DEMO" && uv run python -m agent_demo.evals.runner --otlp-out out/gate-bad --noncompliant >/dev/null)

# The LangGraph demo has the same contract as the compliant path: perfect
# score, INFO hints only. It emits traces and metrics (no events).
LG=apps/langgraph-demo
if [ -f "$LG/pyproject.toml" ]; then
  echo "==> checking the LangGraph bridge"
  (cd "$LG" && uv run python -m langgraph_demo.main --otlp-out out/gate >/dev/null)
  set +e
  go run ./cmd/genai-conformance validate --format json "$LG"/out/gate.traces.binpb "$LG"/out/gate.metrics.binpb >"$TMP/lg.json" 2>"$TMP/lg.err"
  lg_exit=$?
  set -e
  [ "$lg_exit" -eq 0 ] || fail "langgraph-demo validation exited $lg_exit"
  jq -e '.summary.score == 1 and .summary.errors == 0 and .summary.warnings == 0' "$TMP/lg.json" >/dev/null ||
    fail "langgraph-demo telemetry is not flawless: $(jq -c '.summary' "$TMP/lg.json")"
  echo "    score 1.0, $(jq -r '.summary.checks' "$TMP/lg.json") checks"
fi

# go run relays the binary's nonzero exit as an "exit status" line on
# stderr; capture it so an expected exit-1 doesn't read like a failure.
set +e
go run ./cmd/genai-conformance validate --format json "$DEMO"/out/gate-good.*.binpb >"$TMP/good.json" 2>"$TMP/good.err"
good_exit=$?
go run ./cmd/genai-conformance validate --format json "$DEMO"/out/gate-bad.*.binpb >"$TMP/bad.json" 2>"$TMP/bad.err"
bad_exit=$?
set -e

echo "==> checking the compliant contract"
[ "$good_exit" -eq "$(jq -r '.compliant.exit_code' "$GOLDEN")" ] ||
  fail "compliant exit code $good_exit"
jq -e --slurpfile g "$GOLDEN" '.summary.score == $g[0].compliant.score' "$TMP/good.json" >/dev/null ||
  fail "compliant score $(jq -r '.summary.score' "$TMP/good.json") != $(jq -r '.compliant.score' "$GOLDEN")"
[ "$(jq -r '.summary.errors + .summary.warnings' "$TMP/good.json")" -eq 0 ] ||
  fail "compliant run has ERROR/WARNING findings"
unexpected_info="$(jq -r --slurpfile g "$GOLDEN" \
  '[.findings[].rule_id] - $g[0].compliant.allowed_info_rules | unique | join(",")' "$TMP/good.json")"
[ -z "$unexpected_info" ] || fail "unexpected findings on compliant run: $unexpected_info"
echo "    score 1.0, $(jq -r '.summary.checks' "$TMP/good.json") checks, only allowed INFO hints"

echo "==> checking the noncompliant contract"
[ "$bad_exit" -eq "$(jq -r '.noncompliant.exit_code' "$GOLDEN")" ] ||
  fail "noncompliant exit code $bad_exit"
jq -e --slurpfile g "$GOLDEN" \
  '.summary.score < $g[0].noncompliant.score_below' "$TMP/bad.json" >/dev/null ||
  fail "noncompliant score not below threshold"
got_rules="$(jq -r '[.findings[] | select(.severity != "INFO") | .rule_id] | unique | join(",")' "$TMP/bad.json")"
want_rules="$(jq -r '.noncompliant.rule_ids | join(",")' "$GOLDEN")"
[ "$got_rules" = "$want_rules" ] ||
  fail "noncompliant rule set drifted:
  got:  $got_rules
  want: $want_rules"
echo "    exit 1, score $(jq -r '.summary.score' "$TMP/bad.json"), rule set matches golden"

echo "OK: cross-language conformance contract holds"
