import contextlib
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "npm_build", Path(__file__).with_name("build-npm-multipackage.py")
)
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class BuildVersionTests(unittest.TestCase):
    def test_default_uses_package_json(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "package.json").write_text(json.dumps({"version": "2.3.4"}), encoding="utf-8")
            args = BUILD.parse_build_args(root, [])
        self.assertEqual(args.version, "2.3.4")
        self.assertEqual(args.version_main, "2.3.4")
        self.assertEqual(args.version_prerelease, "")
        self.assertEqual(args.version_build_metadata, "")

    def test_package_version_drives_binary_version(self):
        for version, expected in (
            ("2.3.4", ("2.3.4", "", "")),
            ("2.3.4-rc.2", ("2.3.4", "-rc.2", "")),
            ("2.3.4+20260919", ("2.3.4", "", "+20260919")),
            ("2.3.4-rc.2+abc.01", ("2.3.4", "-rc.2", "+abc.01")),
        ):
            with self.subTest(version=version):
                args = BUILD.parse_build_args(Path("unused"), ["--version", version])
                self.assertEqual(
                    (args.version_main, args.version_prerelease, args.version_build_metadata),
                    expected,
                )

    def test_explicit_overrides_and_empty_suffixes(self):
        args = BUILD.parse_build_args(Path("unused"), [
            "--version=2.3.4-rc.2+old",
            "--version-main=3.0.0",
            "--version-prerelease=",
            "--version-build-metadata=+20260919",
        ])
        self.assertEqual(args.version, "2.3.4-rc.2+old")
        self.assertEqual(args.version_main, "3.0.0")
        self.assertEqual(args.version_prerelease, "")
        self.assertEqual(args.version_build_metadata, "+20260919")

    def test_legacy_full_version_main_is_supported(self):
        args = BUILD.parse_build_args(Path("unused"), [
            "--version=2.3.4-rc.2", "--version-main=2.3.4-rc.2",
        ])
        self.assertEqual(args.version_main, "2.3.4")
        self.assertEqual(args.version_prerelease, "-rc.2")

    def test_invalid_versions_fail_before_building(self):
        for version in ("", "1.2", "01.2.3", "1.2.3-01", "1.2.3+", "1.2.3+one+two"):
            with self.subTest(version=version), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    BUILD.parse_build_args(Path("unused"), ["--version", version])
                self.assertEqual(error.exception.code, 2)

    def test_empty_metadata_is_injected(self):
        self.assertIn("-X 'main.VERSION_BUILD_METADATA='", BUILD.make_ldflags("2.3.4", "", "", "stable"))

    def test_print_build_vars_does_not_build(self):
        output = io.StringIO()
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            root = Path(directory)
            stack.enter_context(patch.object(BUILD, "__file__", str(root / "build-npm-multipackage.py")))
            stack.enter_context(patch("sys.argv", ["build-npm-multipackage.py", "--version=2.3.4", "--print-build-vars"]))
            build = stack.enter_context(patch.object(BUILD, "build_go_multiplatform"))
            stack.enter_context(contextlib.redirect_stdout(output))
            self.assertEqual(BUILD.main(), 0)
            build.assert_not_called()
            self.assertEqual(list(root.iterdir()), [])
        self.assertEqual(
            output.getvalue(),
            "VERSION_MAIN=2.3.4\nVERSION_PRERELEASE=\nVERSION_BUILD_METADATA=\nAPP_CHANNEL=stable\n",
        )


if __name__ == "__main__":
    unittest.main()
