import gzip
import os
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ARCHIVER = os.path.join(ROOT, "scripts", "reproducible-archive.py")


class ReproducibleArchiveTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="futurediff-repro-archive-test.")
        self.tree = os.path.join(self.tmp, "tree")
        os.makedirs(os.path.join(self.tree, "bin"))
        with open(os.path.join(self.tree, "README.md"), "w") as fh:
            fh.write("hello\n")
        with open(os.path.join(self.tree, "bin", "fdif"), "w") as fh:
            fh.write("#!/bin/sh\n")
        os.chmod(os.path.join(self.tree, "bin", "fdif"), 0o755)
        self.mtime = 1786380694

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def _archive(self, out, tree=None, name="futurediff-v0.1.0-alpha.3-darwin-arm64", mtime=None):
        subprocess.run(
            [
                sys.executable,
                ARCHIVER,
                "--root",
                tree or self.tree,
                "--name",
                name,
                "--output",
                out,
                "--mtime",
                str(mtime if mtime is not None else self.mtime),
            ],
            check=True,
            capture_output=True,
        )

    def test_identical_tree_identical_bytes(self):
        a = os.path.join(self.tmp, "a.tar.gz")
        b = os.path.join(self.tmp, "b.tar.gz")
        self._archive(a)
        self._archive(b)
        with open(a, "rb") as fa, open(b, "rb") as fb:
            self.assertEqual(fa.read(), fb.read())

    def test_gzip_header_mtime_and_no_filename(self):
        out = os.path.join(self.tmp, "a.tar.gz")
        self._archive(out)
        with open(out, "rb") as fh:
            header = fh.read(10)
        self.assertEqual(header[:2], b"\x1f\x8b")
        self.assertEqual(int.from_bytes(header[4:8], "little"), self.mtime)
        self.assertEqual(header[3], 0)  # no FNAME/extra flags

    def test_owner_modes_and_mtime_normalized(self):
        out = os.path.join(self.tmp, "a.tar.gz")
        self._archive(out)
        with gzip.open(out, "rb") as fh, tarfile.open(fileobj=fh, mode="r|") as tf:
            for member in tf:
                self.assertEqual(member.uid, 0)
                self.assertEqual(member.gid, 0)
                self.assertEqual(member.uname, "root")
                self.assertEqual(member.gname, "root")
                self.assertEqual(member.mtime, self.mtime)
                if member.isdir():
                    self.assertEqual(member.mode & 0o777, 0o755)
                elif member.name.endswith("/bin/fdif"):
                    self.assertEqual(member.mode & 0o777, 0o755)
                elif member.isfile():
                    self.assertEqual(member.mode & 0o777, 0o644)

    def test_different_owner_does_not_change_bytes(self):
        a = os.path.join(self.tmp, "a.tar.gz")
        b = os.path.join(self.tmp, "b.tar.gz")
        os.chmod(self.tree, 0o700)
        os.chmod(os.path.join(self.tree, "bin"), 0o700)
        self._archive(a)
        # The archiver ignores host ownership entirely; simulate a different
        # build user by chowning where permitted (best effort) and rebuild.
        try:
            os.chown(self.tree, 0, 0)
            os.chown(os.path.join(self.tree, "bin"), 0, 0)
        except PermissionError:
            pass
        self._archive(b)
        with open(a, "rb") as fa, open(b, "rb") as fb:
            self.assertEqual(fa.read(), fb.read())

    def test_different_mtime_changes_bytes(self):
        a = os.path.join(self.tmp, "a.tar.gz")
        b = os.path.join(self.tmp, "b.tar.gz")
        self._archive(a)
        self._archive(b, mtime=self.mtime + 1)
        with open(a, "rb") as fa, open(b, "rb") as fb:
            self.assertNotEqual(fa.read(), fb.read())

    def test_changed_payload_changes_bytes(self):
        a = os.path.join(self.tmp, "a.tar.gz")
        b = os.path.join(self.tmp, "b.tar.gz")
        self._archive(a)
        with open(os.path.join(self.tree, "README.md"), "a") as fh:
            fh.write("changed payload\n")
        self._archive(b)
        with open(a, "rb") as fa, open(b, "rb") as fb:
            self.assertNotEqual(fa.read(), fb.read())

    def test_sorted_and_complete_entries(self):
        extra = os.path.join(self.tree, "bin", "futurediffd")
        with open(extra, "w") as fh:
            fh.write("daemon\n")
        out = os.path.join(self.tmp, "a.tar.gz")
        self._archive(out)
        with gzip.open(out, "rb") as fh, tarfile.open(fileobj=fh, mode="r|") as tf:
            names = [m.name for m in tf]
        expected = [
            "futurediff-v0.1.0-alpha.3-darwin-arm64",
            "futurediff-v0.1.0-alpha.3-darwin-arm64/README.md",
            "futurediff-v0.1.0-alpha.3-darwin-arm64/bin",
            "futurediff-v0.1.0-alpha.3-darwin-arm64/bin/fdif",
            "futurediff-v0.1.0-alpha.3-darwin-arm64/bin/futurediffd",
        ]
        self.assertEqual(names, expected)

    def test_extractable_by_system_tar(self):
        out = os.path.join(self.tmp, "a.tar.gz")
        self._archive(out)
        dest = os.path.join(self.tmp, "x")
        os.makedirs(dest)
        subprocess.run(["tar", "-xzf", out, "-C", dest], check=True)
        with open(os.path.join(dest, "futurediff-v0.1.0-alpha.3-darwin-arm64", "README.md")) as fh:
            self.assertEqual(fh.read(), "hello\n")


if __name__ == "__main__":
    unittest.main()
