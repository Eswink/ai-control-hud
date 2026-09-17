"""Keep milestone PRs and shared inputs covered by the relevant CI workflows."""
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / ".github" / "workflows"


def event_block(workflow: str, event: str) -> str:
    lines = (WORKFLOWS / workflow).read_text(encoding="utf-8").splitlines()
    start = lines.index(f"  {event}:") + 1
    block = []
    for line in lines[start:]:
        if line.strip() and not line.lstrip().startswith("#"):
            if len(line) - len(line.lstrip()) <= 2:
                break
        block.append(line)
    return "\n".join(block)


class CICheckpointTests(unittest.TestCase):
    def assert_trigger(self, workflow: str, event: str, path: str) -> None:
        pattern = r"(?m)^\s+- ['\"]?" + re.escape(path) + r"['\"]?\s*$"
        self.assertRegex(event_block(workflow, event), pattern)

    def test_roadmap_pr_runs_every_path_filtered_gate(self):
        for workflow in ("go-agent.yml", "go-hub.yml", "android.yml", "go-release.yml"):
            with self.subTest(workflow=workflow):
                self.assert_trigger(workflow, "pull_request", "docs/HUB_V2_PLAN.md")

    def test_roadmap_push_runs_non_release_gates(self):
        for workflow in ("go-agent.yml", "go-hub.yml", "android.yml"):
            with self.subTest(workflow=workflow):
                self.assert_trigger(workflow, "push", "docs/HUB_V2_PLAN.md")

    def test_android_retests_shared_contract_fixtures(self):
        for event in ("push", "pull_request"):
            with self.subTest(event=event):
                self.assert_trigger("android.yml", event, "server/tests/fixtures/**")

    def test_hub_retests_all_runtime_and_upgrade_helpers(self):
        paths = (
            "scripts/ai-control-hub-upgrade.sh",
            "scripts/ci-hub-upgrade-smoke.sh",
            "scripts/ci-hub-lan-doctor-smoke.sh",
            "scripts/ci-hub-runtime-smoke.py",
            "tools/tests/test_hub_runtime_smoke.py",
        )
        for event in ("push", "pull_request"):
            for path in paths:
                with self.subTest(event=event, path=path):
                    self.assert_trigger("go-hub.yml", event, path)

    def test_hub_invokes_tested_lifecycle_gate(self):
        workflow = (WORKFLOWS / "go-hub.yml").read_text(encoding="utf-8")
        self.assertIn("run: python3 scripts/ci-hub-runtime-smoke.py --hub dist/ai-control-hub", workflow)
        self.assertNotIn("if ! wait", workflow)


if __name__ == "__main__":
    unittest.main()
