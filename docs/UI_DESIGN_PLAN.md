# AI Control HUD — Agent UI Design Plan v1

Status: design baseline / implementation planning
Scope: Windows Agent UI + Android Agent-monitor UI only. Central Hub administration UI is explicitly out of scope.

## 1. Reference screens

The approved visual direction is represented by three reference images:

| Reference | File | Intent |
|---|---|---|
| Windows Agent | `windows-agent-reference.png` | Full operational desktop dashboard |
| Android portrait | `android-portrait-agent-reference.png` | Detailed mobile dashboard and TTS controls |
| Android landscape | `android-landscape-agent-reference.png` | Focus mode: CommandCode remaining quota + current task |

The images are visual references, not literal data contracts. Example values in the images are synthetic. Implementation must never fabricate missing source values.

## 2. Product principles

1. **Glance first.** A user should understand Agent status, current work, and CommandCode budget in 1–2 seconds.
2. **Health and data are separate.** `stale` retains last trustworthy data; `error`/`disabled` must never render fake zero/empty values.
3. **Portrait = detail, landscape = focus.** Landscape is intentionally sparse and optimized as a desk-side status display.
4. **Agent only.** The UI may show Hub connectivity/resolved address but must not become a Hub administration console.
5. **Credentials stay invisible.** Never render CommandCode API keys, Hub tokens, Authorization headers, DPAPI/SecretStore contents, or full private source paths.
6. **Service and UI lifecycles stay separate.** Closing the PC UI must not stop the Windows Agent service.
7. **Internationalized from the first implementation.** Simplified Chinese is a first-class locale; English is the fallback locale.
8. **Old-phone friendly.** Android remains API 23 compatible and should avoid heavyweight visual effects or permanent high-frequency animation.

## 3. Design language

### Color semantics

- App background: `#07111B`
- Elevated surface: `#0D2131`
- Secondary surface: `#102A3D`
- Border/divider: `#173D55`
- Primary cyan: `#27B9FF`
- Accent teal: `#22E0CF`
- Healthy/live green: `#2EE6A6`
- Warning/stale amber: `#F6C453`
- Failure/error red: `#FF6B72`
- Primary text: `#F4F8FC`
- Secondary text: `#A9B8C7`
- Muted text: `#71869A`

Color is never the only state signal; every health color is paired with a text label/icon.

### Typography

- Windows: system `Segoe UI Variable` / `Microsoft YaHei UI` fallback.
- Android: system sans-serif / system CJK font; do not bundle custom font files.
- Hero metric: 32–48sp/dp-equivalent depending on screen class.
- Card title: 16–20sp.
- Body/status text: 13–16sp.

### Geometry

- Card radius: 14–18dp.
- Desktop spacing grid: 8px base, 16–24px card padding.
- Android spacing grid: 4dp base, 12–20dp card padding.
- Shadows/glow remain subtle; no constant animated glow.

## 4. Canonical state mapping

### Overall status

- `live` -> `运行中 / Live`
- `degraded` -> `状态降级 / Degraded`
- Hub reachable + state HTTP 503 -> `等待 Agent / Waiting for Agent`
- Transport failure -> `离线 / Offline`
- Other HTTP failure -> `服务器错误 / Server error`
- Unsupported schema -> `版本不兼容 / Schema error`

### Source health

- `ok` -> green `健康`
- `stale` -> amber `数据过期`
- `error` -> red `数据不可用`
- `disabled` -> gray `未启用`

When health is `error` or `disabled`, UI shows `暂无可信数据 / No trustworthy data`, never 0 quota or an empty task list pretending to be valid.

### Current task selection

UI implementation should deterministically select:

1. most recently updated `running` task;
2. otherwise most recently updated `waiting` task;
3. otherwise no current task.

The schema currently exposes task title/workspace/activity/duration/changes. It does **not** expose a distinct canonical "goal" field. Therefore the reference mock's `当前目标` must not be fabricated. V1 should use `当前任务` + `当前活动`; a separate goal can be introduced only if a future verified source field is added.

### CommandCode hero metric

Priority for the large remaining indicator:

1. if `credit.remaining` + `credit.limit` are present, show remaining / limit with the real `unit`;
2. otherwise show the most useful available usage window as remaining percentage (`100 - usedPercent`), clearly labeled `5H` or `Weekly`;
3. otherwise show `额度数据不可用` without synthesizing zero.

Reset times and both 5H/weekly windows belong in portrait/desktop detail, not the landscape hero view.

## 5. Windows Agent UI

### 5.1 Recommended architecture

Keep `ai-control-agent.exe` as the Windows service/CLI. Add a separate per-user process:

```text
ai-control-agent-ui.exe
    -> local Agent HTTP API (/state, /health, /diagnostics)
    -> Hub read-only event API when available
    -> existing ai-control-agent.exe CLI for privileged service actions
```

Recommended UI shell: **Wails (Go backend + HTML/CSS/TypeScript frontend)**, because it keeps Go integration simple while matching the high-fidelity reference screen. The UI process runs in the interactive user session; the service remains in SCM/Session 0.

Privileged actions such as start/stop/restart/upgrade should use the existing CLI path and request UAC elevation only when needed. Read-only dashboard operation must never require elevation.

### 5.2 Desktop navigation

- Dashboard
- Sources
- Events
- Diagnostics
- Settings

System tray icon should expose: Open Dashboard, Agent status, Restart service (elevated), Exit UI. Exiting the UI does not stop the Agent.

### 5.3 Dashboard information hierarchy

Top summary:
- Agent state/freshness
- Windows service state
- Hub sync state
- logical + resolved Hub address

Main content:
- Current task/activity (largest work panel)
- ZCode health/summary
- CommandCode plan/credit/windows
- Recent events from Hub when reachable

Side diagnostics:
- Agent version / schema
- Agent uptime
- per-source adapter/status/observed age/last-success age/schema support
- last successful Hub resolution/upload status when available

Reference-only fields such as exact outbox count/upload queue count are **phase-2** unless a sanitized local API field is added. Do not read internal SQLite files directly from the UI just to fill a card.

### 5.4 Desktop quick actions

Phase 1:
- Open logs/documented log directory
- Test local health
- Re-resolve/display Hub
- Open settings

Phase 2 (UAC):
- Start / Stop / Restart Agent service
- Transactional Agent upgrade

Destructive actions such as credential removal or purge should stay outside the dashboard and require a separate confirmation flow.

### 5.5 Optional compact desktop mode

A later "Mini HUD" window can show only:
- Agent state
- current task/activity
- CommandCode remaining credit/window

It can support always-on-top and remember window placement. This should be a later iteration, not mixed into the first desktop build.

## 6. Android portrait UI

Portrait is the **detailed mobile dashboard** matching `android-portrait-agent-reference.png`.

Order:
1. App header + settings
2. AUTO/resolved server card + connection state
3. Overall Agent status/freshness
4. ZCode task card
5. CommandCode plan/credit/windows card
6. Recent durable events
7. TTS controls
8. App/cursor/version diagnostics

The existing 2-second state polling and durable event cursor semantics remain unchanged.

### Simplification rule

Settings should not dominate the main dashboard. Server configuration, quiet-hour details, language override, and advanced diagnostics should move into a settings/detail view while the dashboard keeps only the most commonly inspected controls.

## 7. Android landscape UI

Landscape is a **focus display**, matching `android-landscape-agent-reference.png`.

Only three visual layers:

1. slim header + AUTO/resolved Hub + connection indicator;
2. two hero cards occupying almost the full screen:
   - CommandCode remaining plan/quota;
   - current task/activity + running state;
3. one low-emphasis footer strip for TTS ready / warning state.

Do not show the full event list, full settings, all usage windows, app info, or task counters in landscape.

Landscape tap behavior:
- tap CommandCode hero -> quota/5H/weekly detail overlay;
- tap task hero -> task detail overlay;
- tap server header -> connection detail;
- tap TTS footer -> voice settings.

### Old-phone / desk-display behavior

- Keep screen on only while the dashboard is foreground and user enabled "桌面显示模式".
- Optional dim-after-idle mode to reduce OLED/LCD retention and heat.
- Avoid continuously animated rings; refresh on data change/poll only.

## 8. Android implementation strategy

Keep the existing Java/XML/API-23 stack; do not introduce Compose for this redesign.

### Resource refactor

Create:

```text
res/layout/activity_main.xml          portrait/detail
res/layout-land/activity_main.xml     landscape/focus
res/values/colors.xml
res/values/dimens.xml
res/values/styles.xml
res/values/strings.xml                English fallback
res/values-zh-rCN/strings.xml         Simplified Chinese
res/drawable/...                      card/vector/shape assets
```

Remove hard-coded UI strings from Java/XML. Locale follows the OS by default; an explicit language override can be added later.

### Orientation

Remove the current manifest portrait lock. Allow normal Activity recreation on orientation change so Android selects `layout` vs `layout-land`. Persist only durable user settings/cursor; refresh state immediately after recreation.

### Rendering layer

Refactor `MainActivity` into small render helpers/view-state objects instead of expanding one large method. Suggested pieces:

- `ConnectionViewState`
- `AgentStatusViewState`
- `TaskViewState`
- `CommandCodeViewState`
- `VoiceViewState`
- `EventViewState`

Keep parsing/domain semantics separate from presentation formatting.

## 9. Internationalization

### Android

- `values/strings.xml`: English fallback
- `values-zh-rCN/strings.xml`: Simplified Chinese
- dates/numbers formatted with device locale
- technical names remain stable: `ZCode`, `CommandCode`, `Agent`, `Hub`, `TTS`, `AUTO`

### Windows

Frontend string catalogs:

```text
ui/locales/en-US.json
ui/locales/zh-CN.json
```

Default follows Windows locale. Settings can expose a language override later.

Do not concatenate translated sentence fragments where word order may differ; use formatted complete strings.

## 10. Interaction and accessibility

- Minimum Android touch target: 48dp.
- Support system font scaling up to at least 1.3x without clipping hero values.
- Status is text + icon + color.
- Error messages are short and sanitized.
- Keyboard navigation and visible focus states on Windows.
- Reduced-motion mode follows OS preference where practical.
- Avoid tiny low-contrast gray text on the old Android target.

## 11. Implementation roadmap

### UI0 — Design baseline
- Archive these three reference screens.
- Add design tokens/state mapping/i18n contract.
- No runtime behavior changes.

### UI1 — Android resources + i18n foundation
- Move all hard-coded strings/colors/dimens to resources.
- Add `zh-CN` + English.
- Preserve existing portrait behavior/functionality.

### UI2 — Android portrait redesign
- Implement portrait reference hierarchy.
- Keep schema-v1 parser/event/TTS behavior unchanged.
- Add screenshot/rendering tests where practical.

### UI3 — Android landscape focus mode
- Remove portrait lock.
- Add `layout-land` two-hero-card design.
- Add focused quota/current-task selection rules.
- Validate API 23 + modern Android orientation recreation.

### UI4 — Windows desktop shell
- New `ai-control-agent-ui` command/app.
- Wails shell, tray, localization, theme tokens.
- Read-only `/state`, `/health`, `/diagnostics` dashboard first.

### UI5 — Windows operational dashboard
- Sources, task/activity, CommandCode detail.
- Hub recent events when reachable.
- Diagnostics page.

### UI6 — Windows service actions
- UAC-separated Start/Stop/Restart/Upgrade using existing CLI semantics.
- Explicit confirmation for disruptive actions.
- UI process never owns collector/service lifecycle.

### UI7 — Polish / compact mode
- Desktop mini HUD/always-on-top.
- Android detail overlays.
- Accessibility, reduced motion, window/layout persistence.

### UI8 — Delivery gates
- Android portrait + landscape CI builds.
- Windows UI build artifact.
- Repository safety/APK secret scans remain active.
- Manual visual acceptance against the three reference screens.

## 12. First implementation recommendation

Start with **Android UI1 + UI2 + UI3** before the PC GUI. Android already has a functioning UI and all domain/event/TTS logic; the redesign can therefore deliver visible value without inventing a new desktop runtime.

Then implement the Windows shell as a separate process. Do not embed GUI code into the Windows service.

## 13. Explicit non-goals for the first UI cycle

- Hub administration GUI
- editing vendor credentials in the dashboard
- exposing raw logs/secrets/private paths
- changing schema-v1 solely for decorative UI fields
- speculative ZCode goal/token/tool fields
- cloud/public-Internet control plane
- always-on high-FPS animation
