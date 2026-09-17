from __future__ import annotations

import argparse
import gzip
import io
import tarfile
import zipfile
from pathlib import Path, PurePosixPath


def archive_path(value: str) -> str:
    value = value.strip().replace("\\", "/")
    path = PurePosixPath(value)
    if not value or path.is_absolute() or any(part in {"", ".", ".."} for part in path.parts):
        raise ValueError(f"invalid archive path: {value!r}")
    return path.as_posix()


def parse_extra(spec: str) -> tuple[Path, str]:
    source_text, separator, name = spec.partition("=")
    if not separator:
        raise ValueError("--extra must use SOURCE=ARCHIVE_NAME")
    source = Path(source_text).resolve()
    if not source.is_file():
        raise FileNotFoundError(f"extra file does not exist: {source}")
    return source, archive_path(name)


def collect_entries(binary: Path, archive_name: str, extras: list[str]) -> list[tuple[str, bytes, int]]:
    entries: list[tuple[str, bytes, int]] = [(archive_path(archive_name), binary.read_bytes(), 0o755)]
    for spec in extras:
        source, name = parse_extra(spec)
        entries.append((name, source.read_bytes(), 0o755))
    names = [name for name, _, _ in entries]
    if len(names) != len(set(names)):
        raise ValueError("archive entries must have unique names")
    return sorted(entries, key=lambda item: item[0])


def zip_entries(entries: list[tuple[str, bytes, int]], output: Path) -> None:
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name, data, mode in entries:
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = (0o100000 | mode) << 16
            archive.writestr(info, data)


def targz_entries(entries: list[tuple[str, bytes, int]], output: Path) -> None:
    tar_buffer = io.BytesIO()
    with tarfile.open(fileobj=tar_buffer, mode="w", format=tarfile.GNU_FORMAT) as archive:
        for name, data, mode in entries:
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = mode
            info.uid = 0
            info.gid = 0
            info.uname = ""
            info.gname = ""
            info.mtime = 0
            archive.addfile(info, io.BytesIO(data))
    tar_buffer.seek(0)
    with output.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, compresslevel=9, mtime=0) as compressed:
            compressed.write(tar_buffer.getvalue())


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--archive-name", required=True)
    parser.add_argument("--format", required=True, choices=("zip", "tar.gz"))
    parser.add_argument(
        "--extra",
        action="append",
        default=[],
        metavar="SOURCE=ARCHIVE_NAME",
        help="additional executable file to include with deterministic metadata; repeatable",
    )
    args = parser.parse_args()

    binary = args.binary.resolve()
    output = args.output.resolve()
    if not binary.is_file():
        raise SystemExit(f"binary does not exist: {binary}")
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        entries = collect_entries(binary, args.archive_name, args.extra)
    except (FileNotFoundError, ValueError) as exc:
        parser.error(str(exc))
    if args.format == "zip":
        zip_entries(entries, output)
    else:
        targz_entries(entries, output)


if __name__ == "__main__":
    main()
