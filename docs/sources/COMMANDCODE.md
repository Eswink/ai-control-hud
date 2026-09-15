# Command Code source evidence

Status: discovery v1 did not find a usable local source on the target Windows machine; discovery v2 is required before production adapter implementation.

## What the first target-machine report proved

- Platform: native Windows 10 / AMD64.
- No Command Code CLI was detected by the v1 name list.
- No `~/.commandcode` (or other configured Command Code root) was detected.
- No auth/config JSON file was discovered.

The v1 CLI detector did **not** include the official native-Windows alias `cmdc`, so `cli: null` is not sufficient evidence that Command Code is absent.

## Current official behavior relevant to Windows

- Native Windows uses `cmdc`; the full executable name `command-code` is also supported.
- `cmdc status --json` is an official automation-friendly authentication-status command.
- Authentication may come from `~/.commandcode/auth.json` or the `COMMAND_CODE_API_KEY` environment variable.
- `/usage` is the official interactive view for credits, plan, 5-hour usage, weekly usage and reset information.

## Discovery v2

The v2 probe adds:

- executable detection order: `cmdc`, `command-code`, `commandcode`, `cmdcode`;
- presence-only reporting for `COMMAND_CODE_API_KEY` (the value is never emitted);
- optional `--commandcode-status`, which runs `cmdc status --json` (or equivalent) and records only JSON key/type shape, never values.

Run on the target Windows machine:

```cmd
python -m tools.discovery --commandcode-status
```

Then review and share `.local\discovery-report.json`.

## Production-adapter decision gate

Do not bind the backend to an undocumented billing transport until the target installation proves which auth source is actually in use. Once discovery v2 establishes the local CLI/auth source, the next probe will verify a usage transport while preserving these rules:

- credential values never leave the machine or enter git;
- probe output contains only endpoint result status, field names/types and explicitly approved numeric usage fields;
- Android never receives or stores Command Code credentials;
- internal/unstable endpoints remain isolated behind `CommandCodeAdapter`.
