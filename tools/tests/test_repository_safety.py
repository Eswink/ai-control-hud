from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "ci-repository-safety.py"


def init_repo(tmp_path: Path, files: dict[str, bytes | str]) -> Path:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
    for relative, content in files.items():
        path = repo / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(content, bytes):
            path.write_bytes(content)
        else:
            path.write_text(content, encoding="utf-8")
    subprocess.run(["git", "add", "-A"], cwd=repo, check=True)
    return repo


def run_guard(repo: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(SCRIPT), "--repo", str(repo)],
        text=True,
        capture_output=True,
        check=False,
    )


def test_clean_repo_and_env_example_are_allowed(tmp_path: Path) -> None:
    repo = init_repo(
        tmp_path,
        {
            "README.md": "token names in documentation are not secrets\n",
            ".env.example": "EXAMPLE_ONLY=replace-me\n",
            "docs/config.txt": "HUD_HUB_AGENT_TOKEN=<project token>\n",
        },
    )
    result = run_guard(repo)
    assert result.returncode == 0, result.stderr
    assert "PASS" in result.stdout


def test_local_secret_and_database_paths_are_rejected(tmp_path: Path) -> None:
    repo = init_repo(
        tmp_path,
        {
            ".local/commandcode-provider.json": "{}\n",
            "state/hub.sqlite3": b"SQLite format 3\x00",
        },
    )
    result = run_guard(repo)
    assert result.returncode == 1
    assert "tracked private directory" in result.stderr
    assert "tracked generated/private artifact" in result.stderr


def test_environment_and_dpapi_artifacts_are_rejected(tmp_path: Path) -> None:
    repo = init_repo(
        tmp_path,
        {
            ".env.production": "SECRET=value\n",
            "windows/hub.dpapi": b"opaque",
        },
    )
    result = run_guard(repo)
    assert result.returncode == 1
    assert "tracked environment file" in result.stderr
    assert "tracked private artifact" in result.stderr


@pytest.mark.parametrize("kind", ["", "RSA"])
def test_private_key_material_is_rejected_even_in_text_file(tmp_path: Path, kind: str) -> None:
    prefix = "-----BEGIN "
    middle = (kind + " ") if kind else ""
    suffix = "PRIVATE" + " KEY-----"
    begin = prefix + middle + suffix
    end = "-----END " + middle + suffix
    repo = init_repo(
        tmp_path,
        {
            "notes.txt": begin + "\nnot-a-real-key\n" + end + "\n",
        },
    )
    result = run_guard(repo)
    assert result.returncode == 1
    assert "tracked private-key material" in result.stderr
