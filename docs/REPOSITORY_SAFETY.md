# Repository safety gate

AI Control HUD deliberately keeps vendor credentials, Hub ingest tokens, Windows DPAPI records, local SQLite state, Android signing keys, and generated APKs outside Git.

`.gitignore` prevents common accidental additions, while `scripts/ci-repository-safety.py` adds an independent CI check against the **tracked Git index**.

## CI behavior

Python CI runs on every pull request and push and executes:

```text
python scripts/ci-repository-safety.py
```

before editable installation or test execution. The same guard runs on Windows and Linux runners.

The command exits nonzero if tracked files include known private/local/generated artifact paths.

## Rejected tracked artifacts

The guard rejects:

- `.local/` directories;
- `.env` and `.env.*` except `.env.example`;
- `auth.json`;
- `local.properties` / `secrets.properties`;
- project Hub token/provider/DPAPI filenames;
- `.jks`, `.keystore`, `.p12`, `.pfx`, `.pem`, `.key`, `.dpapi`;
- SQLite/database/WAL/SHM files;
- `.log` / `.jsonl` local logs;
- `.apk` / `.aab` generated Android packages.

It also rejects tracked file contents containing standard PEM/OpenSSH private-key headers.

## Deliberate limits

The guard does not reject documentation merely because it mentions names such as `TOKEN`, `API_KEY`, `Authorization`, or environment-variable identifiers. Those names are required in operational documentation and are not secret values.

It is therefore a low-false-positive artifact boundary, not a general entropy/credential scanner. API keys and tokens are still protected architecturally and by provider-specific tests.

The Android APK has a separate post-build credential/local-state scanner. Windows runtime credentials use DPAPI/SecretStore. The Hub token is imported from a temporary file into root-only configuration.

## Recovery from a failure

If CI reports a tracked private artifact:

1. remove the file from the Git index;
2. keep it only in an ignored local/secret location when still needed;
3. rotate the credential if a real secret was ever committed or pushed;
4. rerun the guard locally before pushing again.

Do not fix a guard failure by weakening `.gitignore` or allow-listing a real credential/state file.
