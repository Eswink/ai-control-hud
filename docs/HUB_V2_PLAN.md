# AI Control HUD — Central Hub V2 Plan

Status: H0–H29 implementation delivered in the stacked branches; H30 is the final verification checkpoint. Merge, production signing, and additional field acceptance remain separate gates.

## Objective and production architecture

Keep the always-on authority on the 24/7 CentOS server while preserving local ZCode/CommandCode collection on Windows and the Android schema-v1 contract.

```text
Windows: ai-control-agent (Go)
    local read-only collectors + local diagnostics + durable SQLite outbox
                       |
                       | outbound state / heartbeat / durable events
                       v
CentOS: ai-control-hub (standalone static Go binary)
    SQLite snapshots/events + stale projection + optional LAN discovery
                       |
                       | schema-v1 state + ordered event cursor API
                       v
Android: native Java/XML HUD + local TextToSpeech + quiet hours
```

The standalone Go Hub is the only maintained Central Hub runtime. Python/FastAPI Central Hub code was retired in H9. Retained Python scripts are development diagnostics, source-verification, packaging, or CI tooling; they are not a production Hub runtime dependency.

Windows is the accepted real-device Agent deployment. Linux and macOS also have native Agent CI coverage; CI coverage must not be described as additional target-machine acceptance.

## Architectural decisions and security boundaries

1. Collection stays local. Vendor credentials and source databases remain on the development machine, never on the Hub or Android.
2. The Agent pushes. The Hub never polls Windows. Local diagnostics remain usable during Hub/network failure.
3. Snapshots and events remain separate protocols: snapshots are replaceable current state, while terminal task events use durable delivery.
4. CentOS is Android's authority. Notification policy, TextToSpeech preferences, and quiet hours remain on Android.
5. Trusted LAN/private overlays are the deployment model. Public Internet exposure is not the default.
6. Discovery locates but does not authenticate. Discovery packets contain no bearer token, vendor credential, or HUD data.
7. Stable logical identities survive DHCP: Windows stores `auto://lan`; Android stores `http://auto.lan`. Physical IP addresses are runtime/display state only.
8. CommandCode API keys are supplied explicitly by the operator. The project does not discover, scrape, recover, or auto-retrieve a real key.
9. Machine/service credential import never probes a provider file implicitly. Protected SecretStore is the default; `--provider-config PATH` is explicit compatibility input only.
10. Binary upgrades do not re-import credentials or rewrite machine configuration, SecretStores, service definitions, or durable data.
11. Linux Agent lifecycle must not infer ownership from a historical service filename alone. H29 recognizes the historical Agent unit by its exact Agent `ExecStart` form; non-Agent legacy-name units are preserved.
12. Operational HTTP diagnostics expose fixed metadata only: no private paths, provider URLs, raw errors, task content, usage payloads, tokens, API keys, or SecretStore contents.
13. Failure must not masquerade as trustworthy zero/empty data. Unsupported future schemas are rejected explicitly, not inferred from fields.
14. Production signing material never enters PR jobs, Git, or uploaded artifacts. Ephemeral CI signing does not establish a production signing identity.

## Protocol and behavior

Authenticated Agent ingest:

```text
POST /api/v1/agent/state
POST /api/v1/agent/heartbeat
POST /api/v1/agent/events
Authorization: Bearer <independent project-internal Hub token>
```

Android reads:

```text
GET /api/v1/state
GET /api/v1/health
GET /api/v1/events?after=<seq>&limit=<1..100>
```

Before the first Agent snapshot, Hub `/state` returns HTTP 503 while `/health` can return 200. Android shows **WAITING FOR AGENT**, clears obsolete in-memory snapshot values, and uses normal polling. A valid HTTP response does not invalidate LAN discovery; transport failures still cause rediscovery. Other HTTP errors, transport offline, and schema errors remain distinct states.

When the primary Agent heartbeat expires, the Hub retains last trustworthy data and projects source health to stale/aggregate degraded. Fresh Agent recovery requires no reconfiguration.

Agent-local `GET /api/v1/diagnostics` has independent `diagnosticsVersion=1`. It reports adapter kind, enabled/status, observation/last-success age, and schema support metadata; it does not extend the Android snapshot contract.

### Durable events

The Agent observes trustworthy task transitions independently of network delivery and persists each stable `eventId` in a SQLite outbox. Delivery is at least once; Hub insert is idempotent by globally unique event ID. The Hub assigns monotonic global `seq`, while the read feed filters the configured primary Agent.

A new Android installation silently baselines from `latestSeq`. A lower high-water mark after Hub restore/reset causes a silent cursor rebase. Android persists its cursor against the logical Hub identity, consumes ordered pages, suppresses individual speech for old events, and emits one catch-up summary. Completed/failed speech preferences are independent; default quiet hours are 23:00–08:00. Notification timestamps remain API-23 compatible and clock-skew suppression is tested.

### LAN discovery

```text
TCP 8787  Hub HTTP API
UDP 8788  Hub discovery
Probe:    AI_CONTROL_HUD_DISCOVER_V1
```

Clients derive the physical address from the UDP responder source IP plus advertised HTTP metadata. Explicit `--lan-auto` enables LAN binding/discovery; default Hub binding remains loopback. The LAN doctor checks TCP/UDP listeners, local health, and both runtime and permanent firewalld allowances without changing configuration or reading the token.

### Persistence and service identities

Hub SQLite keeps the compatible `agents`, `snapshots`, `events`, and `idx_events_agent_seq` schema at `/var/lib/ai-control-hud/hub.sqlite3` by default. Go `backup --database PATH --output PATH` uses live `VACUUM INTO`, integrity checking, restrictive permissions, fsync, atomic publication, and overwrite protection. Restore is an explicit stopped-service maintenance operation, not an automatically executed field drill.

```text
Linux Agent unit: ai-control-agent.service
Linux Hub unit:   ai-control-hub.service
Historical name: ai-control-hud.service (ownership must be checked)

Linux Agent outbox: /var/lib/ai-control-hud/agent/events.sqlite3
macOS Agent outbox: <machine-config-directory>/events.sqlite3
```

Windows SecretStore uses machine-scope DPAPI and protected ACLs. Unix SecretStore is permission-protected (`0700` directory / `0600` file), not encrypted at rest. Normal Agent service removal preserves state/outbox; explicit purge removes the Agent outbox DB/WAL/SHM. The independent Hub credential has its own removal command.

Windows SCM, Linux systemd, and macOS launchd binary upgrades preflight a separate candidate, retain rollback state, preserve running/stopped state, and return failure even when a failed candidate is rolled back successfully. launchd transitions use bounded polling/retry; Linux and macOS use their respective root group defaults. Detailed operational boundaries remain in the platform runbooks.

## Iteration delivery record

“Implemented” below means code/CI work in the stack, not merged or officially released. H3 has separate field evidence and explicit exclusions.

| Iteration | Delivered scope | Implementation PR |
| --- | --- | --- |
| H0 | Architecture plan and ADR/architecture alignment | #63 |
| H1 | SQLite Hub MVP, authenticated state/heartbeat, schema-v1 reads, stale projection, persistence/API tests | #63 |
| H2 | Independent outbound snapshot/heartbeat uploader, bounded timeout/backoff, local diagnostics retained | #63 |
| H3 | Core CentOS/Windows/Android cutover accepted; additional drills excluded below | Field record |
| H4 | Durable Agent outbox, independent observation/delivery, idempotent Hub events, ordered/high-water cursor feed | #64 |
| H5 | Android persistent event cursor, local TTS, quiet hours, old-event suppression/summary | #65 |
| H6 | Hardened deployment, credential/data separation, protected Hub token, rotation, backup/restore implementation | #66 |
| H7 | Trusted-LAN UDP discovery, stable auto identities, rediscovery, resolved-address display | #67 |
| H8 | Standalone static Go Hub, protocol/SQLite parity, native backup, binary-only deployment | #68 |
| H9 | Python Central Hub retirement and Go-only boundary guard | #69 |
| H10 | Android waiting/server-error/offline/schema-error semantics | #70 |
| H11 | Sanitized Agent source diagnostics and native privacy checks | #71 |
| H12 | Generic discovery report v1/v2 compatibility and explicit future-version rejection | #72 |
| H13 | Separate provider report v1/v2 compatibility without inventing later evidence | #73 |
| H14 | Debug/unsigned/ephemeral-signed APK gates, secret scan, production-Secrets-only signing workflow | #74 |
| H15 | Android release version injection and final APK metadata verification | #75 |
| H16 | Read-only trusted-LAN readiness doctor and negative firewall/listener cases | #76 |
| H17 | Repository guard against tracked credentials, private state, and build artifacts on every PR/push | #77 |
| H18 | Android production parser consumes shared canonical schema-v1 fixtures, including optional/future-schema cases | #78 |
| H19 | Unified reproducible Agent + Hub release archives, checksums, eligible-run attestation | #79 |
| H20 | Installed Hub LAN doctor exposed through systemd adapter with actual custom ports | #80 |
| H21 | Production Hub bundle metadata/full-member checksums; H3 compatibility packager retained | #81 |
| H22 | Machine-readable release manifest with exact target-set, size/hash, and schema verification | #82 |
| H23 | Operator-supplied CommandCode key CLI into protected SecretStore | #84 |
| H24 | Explicit-only provider import; no implicit local provider file or environment-path probing | #85 |
| H25 | Native Unix manual-key bootstrap and provider-file-free service reinstall | #86 |
| H26 | Windows SCM transactional binary upgrade and real failed-candidate rollback | #87 |
| H27 | Hub transactional systemd binary upgrade preserving config/SQLite/unit/firewall state | #88 |
| H28 | Unix Agent upgrade/rollback, service-owned durable outbox, platform assets in deterministic archives | #89 |
| H29 | Dedicated Linux Agent identity, historical Agent migration, non-Agent legacy-unit preservation | #90 |
| H30 | Tested Hub lifecycle gate, shared-input CI coverage, single-head full-stack checkpoint, reconciled completion record | Closeout PR |

H29's implementation head `16b9bb5929e282a97cee0b5167f95ed66e602b4a` passed Go Agent CI #350, Go Agent Release #69, and Python CI #382. H28's implementation head `22f9da8bda50d522c934a8fe5d3ce102248eb993` passed Go Agent CI #335, Go Agent Release #66, and Python CI #364. These are historical layer baselines, not substitutes for the final-head checkpoint.

### H30 — final verification checkpoint

The old inline Hub smoke used `if ! wait ...; rc=$?`, which could capture the inverted status and turn an abnormal shutdown into an exit-0 CI result. `scripts/ci-hub-runtime-smoke.py` replaces that gate with explicit process status, bounded readiness/shutdown, isolated synthetic configuration, loopback-only probing, strict health validation, and unconditional child cleanup.

Negative-control tests cover a clean exit, a ready process exiting 17, startup exit 23, shutdown timeout, and boolean schema-version rejection. Go Hub CI executes these tests and then the same probe against the actual built Go binary.

`tools/tests/test_ci_checkpoint.py` locks the following trigger contracts:

- roadmap PR checkpoints trigger all four path-filtered workflows;
- ordinary roadmap pushes trigger the three non-release component workflows;
- shared canonical fixture changes trigger Android CI;
- Hub runtime, upgrade, and LAN-doctor helper changes trigger Go Hub CI;
- Go Hub CI invokes the tested lifecycle script rather than the inverted shell gate.

Python CI remains unfiltered and checks the repository safety boundary on every PR/push. Production Android Release is deliberately not invoked by this checkpoint.

## Completion criteria and evidence

This implementation sequence is ready for handoff only after all five workflows on the **same final PR head SHA** report success:

| Workflow | Required evidence |
| --- | --- |
| Go Agent CI | Four native vet/test/build/runtime jobs; Windows SCM; Linux service/outbox/upgrade and identity migration; macOS arm64 launchd service/outbox/upgrade |
| Go Hub CI | Negative controls; Go-only guard; tests; reproducible static build; actual Hub health and clean shutdown; deployment and package contracts |
| Android CI | Debug/release lint and JVM tests; shared fixtures; debug and unsigned APKs; ephemeral signing, apksigner, injected version and secret scan |
| Go Agent Release | Four deterministic Agent archives plus Hub bundle; required assets; unified hashes and release-manifest verification |
| Python CI | Linux/Windows retained tests, repository safety, packaging/deployment/trigger regression tests |

Record final head SHA, run IDs/numbers, and artifact identity on the closeout PR and umbrella issue #62 after the runs finish. A queued/running/skipped workflow is not a passing required workflow. PR-only release attestation/publishing jobs are expected to be skipped; that is not evidence of platform signing or formal publication. A PR release manifest records the checked-out synthetic merge commit (`GITHUB_SHA`), which must not be confused with the PR head SHA.

macOS Intel has native runtime coverage, not a separate native launchd lifecycle gate. The new Hub process-fixture tests run on POSIX; they are explicitly skipped on Windows rather than claimed as Windows Hub validation.

## Field acceptance record — 2026-09-17

Accepted on real devices: standalone Go Hub startup/health; trusted-LAN TCP/UDP after firewalld correction; Windows `auto://lan`; Windows SCM first install; ZCode paths; operator-supplied CommandCode DPAPI plus `doctor --live`; Windows-to-Hub state upload; Android discovery/dashboard/resolved URL/TTS; and Windows stop → stale/degraded → restart → fresh recovery.

Explicitly **NOT RUN**, non-blocking by operator choice: Hub-outage durable-event catch-up, CentOS reboot/autostart, controlled DHCP re-address, live backup/restore, extended memory/temperature endurance, and controlled Wi-Fi/AP-outage endurance. Do not silently turn those exclusions into PASS.

## Remaining repository/release gates and optional backlog

| Item | Disposition |
| --- | --- |
| #62 implementation sequence | H0–H29 delivered; H30 closes final code/CI/documentation verification. Keep umbrella open while its stack is unmerged. |
| #10 release gate | Code/CI gates implemented. Real production-keystore Android signing is NOT RUN; no signing secrets are requested or retrieved. |
| #25 source diagnostics | Implemented in H11/#71; closure follows merge. |
| #26 discovery schema versions | Implemented in H12/#72, with separate provider compatibility in H13/#73; closure follows merge. |
| #46 Windows service/SecretStore | Implemented and core field path accepted; closure follows merge. Historical provider import is now explicit only. |
| #27 branch cleanup | Deferred until implementation branches are actually merged. No unmerged branch deletion. |
| #19 token/tool enrichment | Optional later work. Activity/duration/change metrics already exist; token/tool joins require verified target-machine identifiers. No speculative joins. |

Do not merge stacked PRs, create release tags, publish a formal release, configure production signing identities, delete unmerged branches, or perform excluded field drills as an implicit consequence of code/CI completion.

## Stack order

Implementation order is #63 → #64 → #65 → #66 → #67 → #68 → #69 → #70 → #71 → #72 → #73 → #74 → #75 → #76 → #77 → #78 → #79 → #80 → #81 → #82 → #84 → #85 → #86 → #87 → #88 → #89 → #90 → the H30 closeout PR.

PR #83 is closed unmerged and superseded: it used an outdated H18 base before H19–H22 were observed. It is not part of the active stack. The closeout branch is `feature/hub-v2-verification-closeout`, based on H29's verified head. Detailed historical branch names and evidence remain on #62 and the individual PRs.

## Non-goals

Phone-side ZCode control, vendor credentials on Hub/Android, automatic real-key retrieval, public Internet deployment by default, arbitrary routed-network discovery, multi-user RBAC, heavyweight message brokers, exactly-once delivery, speculative cross-database enrichment, and collector rewrites without a measured requirement are outside this sequence.

## Operational references

- [Deployment](HUB_DEPLOYMENT.md), [Go migration](GO_HUB_MIGRATION.md), and [field checklist](H3_FIELD_VALIDATION.md).
- [Windows service](G6_WINDOWS_SERVICE.md) and [cross-platform service/outbox/upgrade](G7_CROSS_PLATFORM.md).
- [Hub upgrade](HUB_UPGRADE.md) and [LAN doctor](HUB_LAN_DOCTOR.md).
- [Unified Go release](GO_RELEASE.md), [manifest](GO_RELEASE_MANIFEST.md), and [Android release](ANDROID_RELEASE.md).
- [API](API.md), [shared fixtures](CONTRACT_TESTING.md), and [repository safety](REPOSITORY_SAFETY.md).
