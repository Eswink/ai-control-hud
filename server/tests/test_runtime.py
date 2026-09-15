from __future__ import annotations

import asyncio
from datetime import datetime, timezone

from server.hud.adapters.base import CommandCodePayload, PublicAdapterError, ZCodePayload
from server.hud.adapters.mock import StaticCommandCodeAdapter, StaticZCodeAdapter
from server.hud.bootstrap import build_bootstrap_state
from server.hud.models import CommandCodeState, CreditBalance, HudState, OverallStatus, ServerInfo, SourceHealth, TaskSummary, UsageSummary, UsageWindow, ZCodeState, ZCodeSummary
from server.hud.runtime import HudRuntime
from server.hud.store import SnapshotStore


def run(coro):
    return asyncio.run(coro)


def healthy_state() -> HudState:
    now = datetime.now(timezone.utc)
    return HudState(
        server=ServerInfo(version="test", time=now, uptime_seconds=0),
        overall=OverallStatus(status="live"),
        zcode=ZCodeState(
            health=SourceHealth(status="ok", observed_at=now, last_success_at=now),
            summary=ZCodeSummary(running=1, waiting=0, failed=0, completed=0),
            tasks=[TaskSummary(id="t1", title="Task", status="running")],
        ),
        command_code=CommandCodeState(
            health=SourceHealth(status="ok", observed_at=now, last_success_at=now),
            usage=UsageSummary(
                plan="plan",
                credit=CreditBalance(remaining=5, limit=10, unit="USD"),
                windows=[UsageWindow(name="5h", used_percent=50)],
            ),
        ),
    )


def test_failure_with_last_good_becomes_stale_and_preserves_data():
    async def scenario():
        store = SnapshotStore(healthy_state())
        adapter = StaticZCodeAdapter(
            payload=ZCodePayload(
                summary=ZCodeSummary(running=0, waiting=0, failed=0, completed=0),
                tasks=[],
            ),
            error=RuntimeError("TOP_SECRET_SHOULD_NOT_LEAK"),
        )
        runtime = HudRuntime(store, zcode_adapter=adapter)
        await runtime.collect_zcode_once()
        state = await store.get()
        assert state.zcode.health.status == "stale"
        assert state.zcode.tasks[0].id == "t1"
        assert "TOP_SECRET" not in (state.zcode.health.message or "")
        assert state.overall.status == "degraded"

    run(scenario())


def test_failure_without_last_good_is_error_not_fake_zero():
    async def scenario():
        store = SnapshotStore(build_bootstrap_state("test"))
        adapter = StaticZCodeAdapter(
            payload=ZCodePayload(
                summary=ZCodeSummary(running=0, waiting=0, failed=0, completed=0),
                tasks=[],
            ),
            error=RuntimeError("database unavailable"),
        )
        runtime = HudRuntime(store, zcode_adapter=adapter)
        await runtime.collect_zcode_once()
        state = await store.get()
        assert state.zcode.health.status == "error"
        assert state.zcode.summary is None
        assert state.zcode.tasks is None

    run(scenario())


def test_public_adapter_error_can_expose_sanitized_message():
    async def scenario():
        store = SnapshotStore(healthy_state())
        adapter = StaticCommandCodeAdapter(
            payload=CommandCodePayload(usage=UsageSummary(plan="x")),
            error=PublicAdapterError("authentication expired"),
        )
        runtime = HudRuntime(store, command_code_adapter=adapter)
        await runtime.collect_command_code_once()
        state = await store.get()
        assert state.command_code.health.status == "stale"
        assert state.command_code.health.message == "authentication expired"
        assert state.command_code.usage.plan == "plan"

    run(scenario())


def test_successful_collectors_update_independently():
    async def scenario():
        store = SnapshotStore(build_bootstrap_state("test"))
        zcode = StaticZCodeAdapter(
            ZCodePayload(
                summary=ZCodeSummary(running=1, waiting=0, failed=0, completed=2),
                tasks=[TaskSummary(id="new", title="New task", status="running")],
            )
        )
        command = StaticCommandCodeAdapter(
            CommandCodePayload(
                usage=UsageSummary(
                    plan="new-plan",
                    windows=[UsageWindow(name="weekly", used_percent=12)],
                )
            )
        )
        runtime = HudRuntime(store, zcode_adapter=zcode, command_code_adapter=command)
        await runtime.collect_zcode_once()
        mid = await store.get()
        assert mid.zcode.health.status == "ok"
        assert mid.command_code.health.status == "disabled"
        assert mid.overall.status == "degraded"

        await runtime.collect_command_code_once()
        final = await store.get()
        assert final.command_code.health.status == "ok"
        assert final.command_code.usage.plan == "new-plan"
        assert final.overall.status == "live"

    run(scenario())


def test_runtime_start_and_stop_are_idempotent():
    async def scenario():
        from server.hud.config import RuntimeConfig

        store = SnapshotStore(build_bootstrap_state("test"))
        zcode = StaticZCodeAdapter(
            ZCodePayload(
                summary=ZCodeSummary(running=1, waiting=0, failed=0, completed=0),
                tasks=[TaskSummary(id="loop", title="Loop task", status="running")],
            )
        )
        runtime = HudRuntime(
            store,
            zcode_adapter=zcode,
            config=RuntimeConfig(
                zcode_interval_seconds=0.01,
                command_code_interval_seconds=1,
            ),
        )
        await runtime.start()
        await runtime.start()
        await asyncio.sleep(0.03)
        await runtime.stop()
        await runtime.stop()
        state = await store.get()
        assert state.zcode.health.status == "ok"
        assert state.zcode.tasks[0].id == "loop"

    run(scenario())


def test_runtime_config_from_env():
    from server.hud.config import RuntimeConfig

    config = RuntimeConfig.from_env(
        {
            "HUD_ZCODE_INTERVAL_SECONDS": "2.5",
            "HUD_COMMANDCODE_INTERVAL_SECONDS": "45",
        }
    )
    assert config.zcode_interval_seconds == 2.5
    assert config.command_code_interval_seconds == 45.0
