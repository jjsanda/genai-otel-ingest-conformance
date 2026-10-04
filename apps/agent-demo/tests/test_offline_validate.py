"""End-to-end: offline export → the Go conformance gate.

This is the cross-language contract test: the Python demo's telemetry must
score 1.0 against the Go validator. Skipped when no Go toolchain is
available (CI always runs it).
"""

import json
import shutil
import subprocess
from pathlib import Path

import pytest

from agent_demo.evals.runner import run_offline

REPO_ROOT = Path(__file__).resolve().parents[3]


def validate(paths: list[Path]) -> tuple[int, dict]:
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cross-language gate runs in CI")
    proc = subprocess.run(
        ["go", "run", "./cmd/genai-conformance", "validate", "--format", "json"]
        + [str(p) for p in paths],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        timeout=600,
    )
    assert proc.stdout, f"validator produced no output; stderr: {proc.stderr}"
    return proc.returncode, json.loads(proc.stdout)


def test_compliant_export_scores_perfectly(tmp_path):
    paths = run_offline(str(tmp_path / "run"))
    assert len(paths) == 3, "expected traces, metrics, and logs files"

    code, report = validate(paths)
    assert code == 0, json.dumps(report["findings"], indent=2)
    assert report["summary"]["score"] == 1.0
    assert report["summary"]["errors"] == 0
    assert report["summary"]["warnings"] == 0
    # The only acceptable findings are INFO hints (custom provider "mock").
    assert {f["severity"] for f in report["findings"]} <= {"INFO"}
    assert {"agent-demo", "demo-eval-runner"} <= set(report["services"])
