from __future__ import annotations

import argparse
import gzip
import io
import tarfile
import zipfile
from pathlib import Path


def zip_binary(binary: Path, output: Path, archive_name: str) -> None:
    data = binary.read_bytes()
    info = zipfile.ZipInfo(archive_name, date_time=(1980, 1, 1, 0, 0, 0))
    info.compress_type = zipfile.ZIP_DEFLATED
    info.create_system = 3
    info.external_attr = 0o100755 << 16
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        archive.writestr(info, data)


def targz_binary(binary: Path, output: Path, archive_name: str) -> None:
    data = binary.read_bytes()
    tar_buffer = io.BytesIO()
    with tarfile.open(fileobj=tar_buffer, mode="w", format=tarfile.GNU_FORMAT) as archive:
        info = tarfile.TarInfo(archive_name)
        info.size = len(data)
        info.mode = 0o755
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
    args = parser.parse_args()

    binary = args.binary.resolve()
    output = args.output.resolve()
    if not binary.is_file():
        raise SystemExit(f"binary does not exist: {binary}")
    output.parent.mkdir(parents=True, exist_ok=True)
    if args.format == "zip":
        zip_binary(binary, output, args.archive_name)
    else:
        targz_binary(binary, output, args.archive_name)


if __name__ == "__main__":
    main()
