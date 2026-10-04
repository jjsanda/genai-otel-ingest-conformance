#!/usr/bin/env bash
# Render docs/diagrams/src/*.mmd to SVG and PNG.
#
# GitHub renders the README's inline Mermaid natively — these exports exist
# for contexts that can't (slides, previews). Uses mermaid-cli via npx;
# falls back to the dockerized CLI when puppeteer can't run locally.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/docs/diagrams/src"
OUT="$ROOT/docs/diagrams"
MMDC_VERSION=11.16.0

render() { # render <in.mmd> <out-file> [extra args]
  local in="$1" out="$2"
  shift 2
  if npx -y "@mermaid-js/mermaid-cli@$MMDC_VERSION" \
    -p "$ROOT/hack/puppeteer-config.json" -i "$in" -o "$out" "$@" 2>/dev/null; then
    return 0
  fi
  echo "  (npx mmdc failed, trying docker fallback)"
  docker run --rm -u "$(id -u):$(id -g)" -v "$ROOT:/data" \
    "minlag/mermaid-cli:$MMDC_VERSION" \
    -i "/data/${in#"$ROOT"/}" -o "/data/${out#"$ROOT"/}" "$@"
}

for mmd in "$SRC"/*.mmd; do
  name="$(basename "$mmd" .mmd)"
  echo "==> $name"
  render "$mmd" "$OUT/$name.svg"
  render "$mmd" "$OUT/$name.png" --width 1600 --backgroundColor white
done
echo "Done: $(ls "$OUT"/*.svg "$OUT"/*.png 2>/dev/null | wc -l) files in docs/diagrams/"
