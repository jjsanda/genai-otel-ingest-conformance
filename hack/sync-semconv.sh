#!/usr/bin/env bash
# Vendor the OpenTelemetry GenAI semantic-conventions model at a pinned commit.
#
# Usage: hack/sync-semconv.sh [<commit-sha>]
#
# Downloads the model definitions (Weaver YAML + content JSON Schemas),
# LICENSE, and versions.env from open-telemetry/semantic-conventions-genai
# at the given SHA (default: the currently pinned one) into
# third_party/semconv-genai/. The vendored copy is the ground truth that
# registry unit tests and hack/check-drift.sh compare against.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/third_party/semconv-genai"
REPO="open-telemetry/semantic-conventions-genai"
RAW="https://raw.githubusercontent.com/$REPO"

SHA="${1:-$(cat "$DEST/PINNED_SHA")}"

FILES=(
  LICENSE
  versions.env
  model/gen-ai/registry.yaml
  model/gen-ai/spans.yaml
  model/gen-ai/metrics.yaml
  model/gen-ai/events.yaml
  model/gen-ai/gen-ai-input-messages.json
  model/gen-ai/gen-ai-output-messages.json
  model/gen-ai/gen-ai-system-instructions.json
  model/gen-ai/gen-ai-tool-definitions.json
  model/gen-ai/gen-ai-tool-call-arguments.json
  model/gen-ai/gen-ai-tool-call-result.json
  model/gen-ai/gen-ai-retrieval-documents.json
  model/gen-ai/gen-ai-memory-records.json
)

echo "Vendoring $REPO @ $SHA"
# Stage first, replace last: a failed fetch must never leave the vendored
# tree half-deleted (registry tests and the drift check depend on it).
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
for f in "${FILES[@]}"; do
  mkdir -p "$STAGE/$(dirname "$f")"
  curl -sSfL "$RAW/$SHA/$f" -o "$STAGE/$f"
  echo "  $f"
done
printf '%s\n' "$SHA" >"$STAGE/PINNED_SHA"
rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
mv "$STAGE" "$DEST"
trap - EXIT
echo "Done. Pinned SHA recorded in third_party/semconv-genai/PINNED_SHA"
