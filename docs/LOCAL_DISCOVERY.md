# Local source discovery

Issues #1 and #2 require facts from the machine where ZCode and CommandCode are actually installed. `tools.discovery` generates a metadata-only report suitable for manual review before any information is shared.

## Safety properties

By default the tool:

- performs no network requests;
- does not query any SQLite table rows;
- opens discovered SQLite databases in read-only mode;
- records table/view and column schema only;
- parses selected CommandCode JSON files into key/type/length metadata without recording values;
- omits hostname and username;
- shortens home-directory paths to `~/...` and suppresses arbitrary external parent paths;
- runs only `--version` for a discovered CLI.

It intentionally does **not** determine task-state mappings or CommandCode usage endpoints yet. Those require a second, explicitly reviewed probe after we know the installed schema/client shape.

## Run

From the repository root:

```bash
python -m tools.discovery
```

Output defaults to:

```text
.local/discovery-report.json
```

`.local/` is ignored by Git. Review the JSON manually before sharing it.

If the installation uses a nonstandard location:

```bash
python -m tools.discovery \
  --zcode-root /path/to/zcode/state \
  --commandcode-root /path/to/commandcode/state
```

Additional roots are repeatable.

## Tests

The tests intentionally seed fake secret task/auth values and assert those values are absent from generated metadata:

```bash
python -m pytest -q tools/tests
```
