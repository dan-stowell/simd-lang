#!/usr/bin/env python3
"""Unit tests, plus bounded live tests with MEMLIMIT_INTEGRATION=1."""

import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest

import memlimit


HELPER = Path(__file__).with_name("memlimit.py").resolve()


class UnitTests(unittest.TestCase):
    def test_budgets(self):
        for value, expected in [("4096", 4096), ("64M", 64 * 1024**2),
                                ("8G", 8 * 1024**3), ("1T", 1024**4)]:
            self.assertEqual(memlimit.memory_bytes(value), expected)
        for value in ["0", "-1", "1", "infinity", "50%", "1.5G", "8GB", "8g",
                      "9223372036854775808", "8G\n"]:
            with self.subTest(value=value), self.assertRaises(argparse.ArgumentTypeError):
                memlimit.memory_bytes(value)

    def test_service_argv(self):
        command = ["./compiler", "$HOME", "${LITERAL}", "a b", "", "--scope"]
        argv = memlimit.service_command(64 * 1024**2, command, "test.service",
                                       {"FC_BIN": "secret-value", "PATH": "/bin"})
        for option in ["--user", "--wait", "--pipe", "--collect", "--same-dir",
                       "--expand-environment=no", "--service-type=exec",
                       "--property=MemoryMax=67108864", "--property=MemorySwapMax=0",
                       "--property=OOMPolicy=kill", "--property=KillMode=control-group",
                       "--setenv=FC_BIN", "--setenv=PATH"]:
            self.assertIn(option, argv)
        self.assertNotIn("--scope", argv[:argv.index("--")])
        self.assertNotIn("secret-value", " ".join(argv))
        self.assertEqual(argv[-len(command):], command)
        with self.assertRaises(ValueError):
            memlimit.service_command(4096, ["true"], "test.service", {"bad=name": ""})

    def test_enforcement_checks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            proc = root / "proc-cgroup"
            proc.write_text("0::/app.slice/test.service\n")
            group = root / "app.slice" / "test.service"
            group.mkdir(parents=True)
            good = {"memory.max": "67108864", "memory.swap.max": "0",
                    "memory.oom.group": "1"}
            for name, value in good.items():
                (group / name).write_text(value + "\n")
            memlimit.verify_cgroup(67108864, "test.service", proc, root)
            for name, value in [("memory.max", "max"), ("memory.max", "67112960"),
                                ("memory.max", "0"), ("memory.swap.max", "max"),
                                ("memory.oom.group", "0")]:
                with self.subTest(name=name, value=value):
                    (group / name).write_text(value)
                    with self.assertRaises(RuntimeError):
                        memlimit.verify_cgroup(67108864, "test.service", proc, root)
                    (group / name).write_text(good[name])
            (group / "memory.max").unlink()
            with self.assertRaises(OSError):
                memlimit.verify_cgroup(67108864, "test.service", proc, root)
            for value in ["0::/app.slice/other.service\n", "1:memory:/old\n",
                          "0::/../test.service\n"]:
                proc.write_text(value)
                with self.assertRaises(RuntimeError):
                    memlimit.verify_cgroup(67108864, "test.service", proc, root)

    def test_cli_rejects_missing_command_and_unbounded_limit(self):
        for argv in [[], ["--memory", "infinity", "--", "true"], ["-m", "64M"]]:
            result = subprocess.run([sys.executable, str(HELPER), *argv],
                                    capture_output=True)
            self.assertEqual(result.returncode, 2)


@unittest.skipUnless(os.environ.get("MEMLIMIT_INTEGRATION") == "1",
                     "set MEMLIMIT_INTEGRATION=1 for live systemd/cgroup/OOM tests")
class IntegrationTests(unittest.TestCase):
    def run_limited(self, code, *arguments, **kwargs):
        return subprocess.run(
            [sys.executable, str(HELPER), "-m", "64M", "--",
             sys.executable, "-c", code, *arguments],
            capture_output=True, text=True, timeout=30, **kwargs)

    def test_environment_directory_argv_and_stdio(self):
        with tempfile.TemporaryDirectory(prefix="memlimit space ") as tmp:
            env = dict(os.environ, FC_BIN="relative compiler",
                       MEMLIMIT_TEST="spaces $DOLLAR ${BRACES}\nnewline")
            result = self.run_limited(
                "import json,os,sys; "
                "print(json.dumps([os.getcwd(),os.environ['FC_BIN'],"
                "os.environ['MEMLIMIT_TEST'],sys.argv[1:],sys.stdin.read()])); "
                "print('stderr passed',file=sys.stderr)",
                "$HOME", "${MEMLIMIT_TEST}", "", "a b",
                cwd=tmp, env=env, input="input passed\n")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout),
                             [tmp, env["FC_BIN"], env["MEMLIMIT_TEST"],
                              ["$HOME", "${MEMLIMIT_TEST}", "", "a b"], "input passed\n"])
            self.assertIn("stderr passed", result.stderr)

    def test_exit_status_and_missing_executable(self):
        result = self.run_limited("raise SystemExit(37)")
        self.assertEqual(result.returncode, 37, result.stderr)
        result = subprocess.run([sys.executable, str(HELPER), "-m", "64M", "--",
                                 "/definitely/not/a/command"], capture_output=True, timeout=30)
        self.assertNotEqual(result.returncode, 0)

    def test_oom_kills_service_not_supervisor(self):
        # Bounded at 256 MiB even in case of a broken limit. The allocator is a
        # child of the service's main process: OOMPolicy must kill both of them.
        supervisor_pid = os.getpid()
        before = Path("/proc/self/cgroup").read_text()
        code = """
import json, os, pathlib, subprocess, sys
group = pathlib.Path('/proc/self/cgroup').read_text()
print(json.dumps({'main': os.getpid(), 'group': group}), flush=True)
child = subprocess.Popen([sys.executable, '-c',
    'import os; print(os.getpid(), flush=True); '
    'chunks = [bytearray(1024*1024) for _ in range(256)]'])
child.wait()
print('ERROR: main process survived child OOM', flush=True)
"""
        result = self.run_limited(code)
        self.assertNotEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertGreaterEqual(len(lines), 2, result.stdout + result.stderr)
        service = json.loads(lines[0])
        child_pid = int(lines[1])
        self.assertNotEqual(service["group"], before)
        self.assertIn("compiler-test-", service["group"])
        self.assertNotIn("ERROR:", result.stdout)
        self.assertIn("oom-kill", result.stderr.lower())
        for pid in [service["main"], child_pid]:
            self.assertFalse(Path("/proc", str(pid)).exists(), f"service PID {pid} survived")
        self.assertEqual(os.getpid(), supervisor_pid)
        self.assertEqual(Path("/proc/self/cgroup").read_text(), before)
        print(f"64M OOM: status={result.returncode}, service/allocator dead; "
              f"supervisor {supervisor_pid} alive in {before.strip()}", flush=True)
        self.assertEqual(self.run_limited("print('still alive')").returncode, 0)

    def test_cancellation_stops_service(self):
        with tempfile.TemporaryFile(mode="w+") as output:
            process = subprocess.Popen(
                [sys.executable, str(HELPER), "-m", "64M", "--",
                 sys.executable, "-c", "import os,time; print(os.getpid(),flush=True); "
                 "time.sleep(60)"], stdout=subprocess.PIPE, stderr=output, text=True)
            try:
                pid = int(process.stdout.readline())
                process.send_signal(signal.SIGTERM)
                self.assertEqual(process.wait(timeout=20), 128 + signal.SIGTERM)
                self.assertFalse(Path("/proc", str(pid)).exists())
            finally:
                process.stdout.close()
                if process.poll() is None:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    unittest.main(verbosity=2)
