#!/usr/bin/env python3
"""Deterministic gzip'd tar archiver for FutureDiff public release archives.

Contract: same staged tree + same SOURCE_DATE_EPOCH + same archive layout
=> byte-identical .tar.gz on Linux and macOS (and across build hosts/users).

Normalization applied (everything that previously made `tar -czf` output
non-reproducible):

* entries are walked and written in sorted path order (no readdir order);
* uid/gid are forced to 0 and uname/gname to root/root (no build-user
  leakage);
* every entry mtime is forced to SOURCE_DATE_EPOCH (no wall clock);
* directory modes are normalized to 0755, files with any execute bit to
  0755, and other files to 0644 (umask-independent);
* GNU tar format is used: no pax extended headers, no atime/ctime, no
  xattrs, no extended ACLs;
* the gzip header records mtime = SOURCE_DATE_EPOCH and no original
  filename; the OS byte is the fixed Unix value on Linux and macOS.

The mtime argument is the integer epoch-seconds timestamp derived from
source control (the commit timestamp), matching SOURCE_DATE_EPOCH
semantics. Wall-clock time is never consulted.
"""

import argparse
import gzip
import os
import stat
import sys
import tarfile

EXEC_MODE = 0o755
FILE_MODE = 0o644
DIR_MODE = 0o755


def normalized_mode(mode: int, is_dir: bool) -> int:
    if is_dir:
        return DIR_MODE
    if mode & (stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH):
        return EXEC_MODE
    return FILE_MODE


def collect_entries(root: str):
    """Return a deterministic list of (relpath, is_dir) for the tree."""
    entries = []
    for dirpath, dirnames, filenames in os.walk(root, topdown=True):
        dirnames.sort()
        filenames.sort()
        rel = os.path.relpath(dirpath, root)
        entries.append((rel, True))
        for filename in filenames:
            child = filename if rel == "." else os.path.join(rel, filename)
            entries.append((child, False))
    entries.sort(key=lambda item: (item[0] != ".", item[0]))
    return entries


def build_archive(root: str, name: str, output: str, mtime: int) -> None:
    if not os.path.isdir(root):
        raise SystemExit(f"error: staged tree is not a directory: {root}")
    entries = collect_entries(root)
    with open(output, "wb") as out_fh:
        gz = gzip.GzipFile(
            filename="",
            mode="wb",
            fileobj=out_fh,
            compresslevel=9,
            mtime=mtime,
        )
        try:
            tf = tarfile.open(
                fileobj=gz, mode="w|", format=tarfile.GNU_FORMAT
            )
            try:
                for rel, is_dir in entries:
                    if rel == ".":
                        arcname = name + "/"
                    else:
                        arcname = name + "/" + rel
                    source = os.path.join(root, rel)
                    info = os.lstat(source)
                    ti = tarfile.TarInfo(arcname)
                    ti.type = tarfile.DIRTYPE if is_dir else tarfile.REGTYPE
                    ti.mode = normalized_mode(info.st_mode, is_dir)
                    ti.uid = 0
                    ti.gid = 0
                    ti.uname = "root"
                    ti.gname = "root"
                    ti.mtime = mtime
                    ti.size = 0 if is_dir else info.st_size
                    if is_dir:
                        tf.addfile(ti)
                    else:
                        with open(source, "rb") as src:
                            tf.addfile(ti, src)
            finally:
                tf.close()
        finally:
            gz.close()


def main(argv):
    parser = argparse.ArgumentParser(
        description=(
            "write a deterministic tar.gz for a staged release tree; "
            "mtime is the SOURCE_DATE_EPOCH-style commit timestamp"
        )
    )
    parser.add_argument("--root", required=True, help="staged tree directory")
    parser.add_argument(
        "--name", required=True, help="archive top-level directory name"
    )
    parser.add_argument("--output", required=True, help="output .tar.gz path")
    parser.add_argument(
        "--mtime",
        type=int,
        required=True,
        help="epoch-seconds timestamp derived from source control",
    )
    args = parser.parse_args(argv)
    if args.mtime < 0:
        raise SystemExit("error: mtime must be a non-negative epoch value")
    build_archive(args.root, args.name, args.output, args.mtime)


if __name__ == "__main__":
    main(sys.argv[1:])
