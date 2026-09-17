# UI and reliability plan completion

Status: complete

This file records the implementation closure of `docs/UI_DESIGN_PLAN.md` v2. The original plan remains the architectural contract; this document records which merged change completed each roadmap item.

## Roadmap closure

| Milestone | Completion | Merged change |
|---|---|---|
| R0 — Resource baseline & budgets | Complete | PR #93 baseline/resource instrumentation and release-budget foundation |
| R1 — Hub storage retention | Complete | PR #93 retention policy, retention floor, batched pruning |
| R2 — SQLite efficiency & storage diagnostics | Complete | PR #94 state cache, Hub stats, maintenance/checkpoint coverage |
| R3 — Agent local DB hygiene | Complete | PR #95 outbox diagnostics/warnings and conservative hygiene behavior |
| R4 — Leak/endurance gates | Complete | PR #96 accelerated resource/leak regression gates |
| UI0 — Design baseline | Complete | PR #92 shared design/reference baseline |
| UI1 — Android foundation | Complete | PR #97 resources/i18n/lifecycle foundation |
| UI2 — Android portrait redesign | Complete | PR #98 portrait operational dashboard |
| UI3 — Android landscape Focus Mode | Complete | PR #99 landscape focus layout, opt-in foreground keep-awake policy |
| UI4 — Windows native shell | Complete | PR #100 C++20 Win32/Direct2D/DirectWrite/WinHTTP native shell |
| UI5 — Windows operational dashboard | Complete | PR #101 dashboard, sources, diagnostics and bounded real data views |
| UI6 — Windows privileged actions | Complete | PR #102 UAC-separated allowlisted service actions |
| UI7 — Mini HUD / polish | Complete | PR #103 topmost Mini HUD, persistence, reduced-motion/data-change rendering |
| UI8 — Delivery/resource gates | Complete | PR #104 deterministic Windows bundle, Android resource/package gates, resource/privacy regression gates |

## Acceptance state

The UI8 merge closed the implementation roadmap with all required CI gates green on `main`:

- Python CI;
- Android CI, including portrait/landscape packaged-resource verification and signed CI release verification;
- Windows Native UI CI, including reproducible executable checks, native artifact privacy scanning, model tests, resource/leak smoke and deterministic packaging.

The Windows UI remains native and event-driven. Android remains Java/XML framework Views. Hub remains headless. No non-goal from the plan was introduced.

## Release closure

Tagged releases use the repository release contract in `docs/RELEASE.md`. A tagged version is considered UI-plan releasable only when both release workflows succeed and the GitHub Release contains the Go Agent/Hub artifacts plus the Windows UI archive and Android APK/metadata.
