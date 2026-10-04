#!/usr/bin/env bash
# Compare the vendored (pinned) GenAI semconv model against upstream main.
#
# The GenAI semantic conventions are Development-status: they move without
# tagged releases. This script makes that movement visible instead of silent.
#
# Exit codes: 0 = in sync, 3 = drift detected (report printed), else = error.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/third_party/semconv-genai"
REPO="open-telemetry/semantic-conventions-genai"
RAW="https://raw.githubusercontent.com/$REPO"
REF="${1:-main}"

PINNED="$(cat "$DEST/PINNED_SHA")"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Compare every vendored file (versions.env included) and also list the
# upstream directory so newly added model files are caught — a comparison
# limited to files we already have would miss upstream additions entirely.
mapfile -t FILES < <(cd "$DEST" && find model -type f | sort; echo versions.env)

drift=0
# List the upstream model/gen-ai directory so newly added files are caught.
# A failed API call must be an error, not a silent "no additions" — the
# whole point of this check is that it does not quietly pass.
listing_json="$TMP/listing.json"
auth=()
[ -n "${GITHUB_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $GITHUB_TOKEN")
api_code="$(curl -sSL -w '%{http_code}' \
  -H "Accept: application/vnd.github+json" "${auth[@]}" \
  "https://api.github.com/repos/$REPO/contents/model/gen-ai?ref=$REF" \
  -o "$listing_json" || echo 000)"
if [ "$api_code" != "200" ]; then
  echo "ERROR: listing upstream model/gen-ai returned HTTP $api_code (rate limit? network?)" >&2
  echo "       set GITHUB_TOKEN to raise the unauthenticated rate limit" >&2
  exit 1
fi
while IFS= read -r path; do
  [ -n "$path" ] || continue
  if [ ! -f "$DEST/$path" ]; then
    echo "UPSTREAM ADDED: $path (not vendored at the pin)"
    drift=1
  fi
done < <(grep -o '"path": *"[^"]*"' "$listing_json" | cut -d'"' -f4)

for f in "${FILES[@]}"; do
  mkdir -p "$TMP/$(dirname "$f")"
  http_code="$(curl -sSL -w '%{http_code}' "$RAW/$REF/$f" -o "$TMP/$f" || echo 000)"
  if [ "$http_code" = "404" ]; then
    echo "UPSTREAM REMOVED: $f"
    drift=1
    continue
  elif [ "$http_code" != "200" ]; then
    echo "ERROR: fetching $f returned HTTP $http_code" >&2
    exit 1
  fi
  if ! diff -u --label "pinned/$f" --label "upstream/$f" "$DEST/$f" "$TMP/$f" >"$TMP/diff.out"; then
    drift=1
    echo "DRIFT in $f:"
    # Summarize: show changed attribute/metric/event ids plus the raw diff size.
    grep -E '^[+-]\s+(- )?id:' "$TMP/diff.out" | sed 's/^/  /' || true
    echo "  ($(grep -c '^[+-]' "$TMP/diff.out") changed lines total — full diff below)"
    sed 's/^/  | /' "$TMP/diff.out"
  fi
done

if [ "$drift" -eq 0 ]; then
  echo "No drift: upstream $REF matches the pinned model ($PINNED)."
  exit 0
fi

echo
echo "Upstream $REF has moved since the pin ($PINNED)."
echo "To upgrade: hack/sync-semconv.sh <new-sha>, update internal/registry/curated/, update goldens."
exit 3
