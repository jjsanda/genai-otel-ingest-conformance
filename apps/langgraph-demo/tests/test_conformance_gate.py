"""Cross-language end-to-end: this app's telemetry judged by the Go engine."""

import shutil
import subprocess
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[3]


def test_offline_export_passes_conformance(tmp_path):
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; the CI job runs this gate")

    prefix = tmp_path / "run"
    subprocess.run(
        [sys.executable, "-m", "langgraph_demo.main", "--otlp-out", str(prefix)],
        check=True,
        capture_output=True,
    )
    result = subprocess.run(
        [
            "go",
            "run",
            "./cmd/genai-conformance",
            "validate",
            str(prefix) + ".traces.binpb",
            str(prefix) + ".metrics.binpb",
        ],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        timeout=600,
    )
    assert result.returncode == 0, f"conformance gate failed:\n{result.stdout}\n{result.stderr}"
    assert "Score: 100.0%" in result.stdout
