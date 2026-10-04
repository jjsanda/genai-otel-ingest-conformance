# Kubernetes quickstart (kind)

The same topology as the compose stack, on Kubernetes: compliant demos ship
through an OpenTelemetry Collector, the noncompliant demo ships straight to
the conformance gateway, and a bounded traffic Job drives both.

## One command

```sh
hack/e2e-kind.sh
```

creates a kind cluster (digest-pinned node image), builds and side-loads the
three images, deploys `deploy/k8s/overlays/kind`, waits for the traffic Job,
and asserts the live report: both compliant services at exactly **1.0**, the
noncompliant service **below 0.8**, and every rule from the offline golden
firing live. It tears the cluster down when finished (`KEEP_CLUSTER=1` to
keep it).

## By hand

```sh
kind create cluster --name genai-conformance
docker build -t genai-conformance:local .
docker build -t genai-agent-demo:local apps/agent-demo
docker build -t genai-langgraph-demo:local apps/langgraph-demo
kind load docker-image --name genai-conformance \
  genai-conformance:local genai-agent-demo:local genai-langgraph-demo:local

kubectl apply -k deploy/k8s/overlays/kind
kubectl -n genai-conformance wait --for=condition=complete job/traffic --timeout=10m

kubectl -n genai-conformance port-forward svc/gateway 8080:8080
# → http://localhost:8080         live report UI
# → http://localhost:8080/api/report
```

Re-run traffic any time:

```sh
kubectl -n genai-conformance delete job traffic
kubectl apply -k deploy/k8s/overlays/kind
```

## What to look at

- The report shows `agent-demo` and `demo-langgraph-agent` at 100% while
  `unknown_service` (the noncompliant demo — it deliberately loses its
  `service.name`) drowns in findings, each with a remediation hint and a
  doc link pinned to the semconv commit.
- Gateway self-metrics (`kubectl -n genai-conformance port-forward
  svc/gateway 8080:8080` then `/metrics`) expose scores, finding rates, and
  trace-window behavior; the compose stack ships a provisioned Grafana
  dashboard over the same metrics.

## Notes

- Manifests set probes, resource requests/limits, and restrictive security
  contexts (non-root, no privilege escalation, read-only root fs for the
  gateway, RuntimeDefault seccomp).
- Images use `imagePullPolicy: Never` — they are side-loaded by kind, never
  pulled. For a real cluster, push them to a registry and drop that line.
- Prometheus/Grafana are intentionally compose-only; the kind overlay stays
  lean so the e2e suite is fast and reliable in CI.
