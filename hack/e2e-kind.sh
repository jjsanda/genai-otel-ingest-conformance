#!/usr/bin/env bash
# End-to-end proof in a real cluster.
#
# Creates a kind cluster, builds and side-loads the images, deploys the
# full topology (collector-fronted compliant apps, direct noncompliant app),
# drives bounded traffic, and asserts the gateway's live report:
#   - both compliant services score exactly 1.0 with zero errors/warnings
#   - the noncompliant service scores below 0.8 with errors
#   - every rule from the offline golden also fires live
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CLUSTER="${CLUSTER:-genai-conformance-e2e}"
NODE_IMAGE="kindest/node:v1.32.2@sha256:f226345927d7e348497136874b6d207e0b32cc52154ad8323129352923a3142f"
NS=genai-conformance
PF_PORT="${PF_PORT:-18081}"
KEEP_CLUSTER="${KEEP_CLUSTER:-0}"

PF_PID=""
cleanup() {
  status=$?
  [ -n "$PF_PID" ] && kill "$PF_PID" 2>/dev/null || true
  if [ "$status" -ne 0 ]; then
    echo "=== e2e FAILED — cluster state dump ==="
    kubectl --context "kind-$CLUSTER" -n "$NS" get pods -o wide 2>/dev/null || true
    kubectl --context "kind-$CLUSTER" -n "$NS" get events --sort-by=.lastTimestamp 2>/dev/null | tail -30 || true
    for d in gateway collector agent-demo agent-demo-noncompliant; do
      echo "--- logs: $d"
      kubectl --context "kind-$CLUSTER" -n "$NS" logs "deploy/$d" --tail=20 2>/dev/null || true
    done
  fi
  if [ "$KEEP_CLUSTER" != "1" ]; then
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

echo "==> creating kind cluster $CLUSTER"
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" --wait 180s

echo "==> building images"
docker build -q -t genai-conformance:local --build-arg VERSION=e2e . >/dev/null
docker build -q -t genai-agent-demo:local apps/agent-demo >/dev/null
docker build -q -t genai-langgraph-demo:local apps/langgraph-demo >/dev/null

echo "==> loading images into the cluster"
kind load docker-image --name "$CLUSTER" \
  genai-conformance:local genai-agent-demo:local genai-langgraph-demo:local

echo "==> deploying"
kubectl --context "kind-$CLUSTER" apply -k deploy/k8s/overlays/kind
for d in gateway collector agent-demo agent-demo-noncompliant langgraph-demo; do
  kubectl --context "kind-$CLUSTER" -n "$NS" rollout status "deploy/$d" --timeout=240s
done

echo "==> waiting for the traffic job"
kubectl --context "kind-$CLUSTER" -n "$NS" wait --for=condition=complete job/traffic --timeout=420s

# Let exporters flush (1s batch delay in this overlay) and the gateway's
# 10s idle windows close before judging the report.
sleep 20

echo "==> fetching the live report"
kubectl --context "kind-$CLUSTER" -n "$NS" port-forward svc/gateway "$PF_PORT:8080" >/dev/null 2>&1 &
PF_PID=$!
for _ in $(seq 1 30); do
  curl -sf "localhost:$PF_PORT/readyz" >/dev/null 2>&1 && break
  sleep 1
done
REPORT="$(mktemp)"
curl -sf "localhost:$PF_PORT/api/report" >"$REPORT"

fail() {
  echo "FAIL: $1" >&2
  jq '{summary, services}' "$REPORT" >&2 || cat "$REPORT" >&2
  exit 1
}

echo "==> asserting the report"
jq -e '.services["agent-demo"].score == 1 and .services["agent-demo"].errors == 0 and .services["agent-demo"].warnings == 0' "$REPORT" >/dev/null ||
  fail "agent-demo is not flawless"
jq -e '.services["demo-langgraph-agent"].score == 1' "$REPORT" >/dev/null ||
  fail "demo-langgraph-agent is not flawless"
jq -e '.services["unknown_service"].score < 0.8 and .services["unknown_service"].errors > 0' "$REPORT" >/dev/null ||
  fail "noncompliant service was not caught"

# Every rule from the offline golden must also fire against live traffic.
missing="$(jq -r --slurpfile g apps/agent-demo/expected_findings.json \
  '$g[0].noncompliant.rule_ids - [.rules[] | select(.count > 0) | .rule_id] | join(",")' "$REPORT")"
[ -z "$missing" ] || fail "golden rules did not fire live: $missing"

echo "PASS: live cluster report matches expectations"
jq '{summary: (.summary | {score, checks, errors, warnings}), services: (.services | map_values({score, errors, warnings}))}' "$REPORT"
