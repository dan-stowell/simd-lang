#!/usr/bin/env python3
"""Run a correctness workload in a separate, memory-limited systemd user service.

Usage: tools/memlimit.py --memory 8G -- COMMAND [ARG ...]
Requires Linux cgroup v2 and systemd-run (255+, for --expand-environment=no).
See LOOP.md, systemd-run(1), systemd.resource-control(5), systemd.service(5).
There is deliberately no unlimited fallback if isolation cannot be established.
"""

import argparse
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import uuid


def memory_bytes(value):
    """Accept only an explicit, finite binary byte budget, not a percentage."""
    match = re.fullmatch(r"([0-9]+)([KMGT]?)", value)
    if not match:
        raise argparse.ArgumentTypeError("use bytes or an integer with K/M/G/T (e.g. 8G)")
    size = int(match[1]) * 1024 ** (" KMGT".index(match[2]) if match[2] else 0)
    if not 4096 <= size < 2**63:
        raise argparse.ArgumentTypeError("budget must be at least 4096 and less than 2^63 bytes")
    return size


def service_command(memory, command, unit, environ):
    # Forward names, not values, so secrets are not exposed in systemd-run's argv.
    # A service does not otherwise inherit the invoking shell's environment.
    names = sorted(environ)
    if any(not re.fullmatch(r"[A-Za-z_][A-Za-z_0-9]*", name) for name in names):
        raise ValueError("exported environment has a name systemd cannot preserve")
    return [
        "systemd-run", "--user", "--wait", "--pipe", "--collect",
        "--service-type=exec", "--expand-environment=no", "--same-dir",
        "--unit=" + unit,
        "--property=MemoryMax=" + str(memory),
        "--property=MemorySwapMax=0", "--property=OOMPolicy=kill",
        "--property=KillMode=control-group", "--property=TimeoutStopSec=5s",
        *["--setenv=" + name for name in names],
        "--", sys.executable, str(Path(__file__).resolve()),
        "--_exec", str(memory), unit, *command,
    ]


def verify_cgroup(memory, unit, proc=Path("/proc/self/cgroup"),
                  root=Path("/sys/fs/cgroup")):
    """Fail closed if systemd accepted properties without enforcing them."""
    entries = proc.read_text().splitlines()
    paths = [line[3:] for line in entries if line.startswith("0::")]
    if len(paths) != 1 or Path(paths[0]).name != unit or ".." in Path(paths[0]).parts:
        raise RuntimeError("not in the requested cgroup-v2 service")
    group = root / paths[0].lstrip("/")
    maximum = (group / "memory.max").read_text().strip()
    if maximum == "max" or not 0 < int(maximum) <= memory:
        raise RuntimeError("MemoryMax is not enforced")
    if (group / "memory.swap.max").read_text().strip() != "0":
        raise RuntimeError("MemorySwapMax=0 is not enforced")
    if (group / "memory.oom.group").read_text().strip() != "1":
        raise RuntimeError("OOMPolicy=kill is not enforced")


def supervise(argv, unit):
    """Keep the caller outside the limit; cancel the whole service on signals."""
    interrupted = 0

    def interrupt(signum, _frame):
        nonlocal interrupted
        interrupted = interrupted or signum

    previous = {sig: signal.signal(sig, interrupt)
                for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
    try:
        process = subprocess.Popen(argv)
        while True:
            if interrupted:
                # Retry if a signal arrived before systemd created the unit.
                # Default control-group killing also cleans up grandchildren.
                subprocess.run(
                    ["systemctl", "--user", "stop", unit], check=False,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
            try:
                result = process.wait(timeout=0.25)
                return 128 + interrupted if interrupted else (
                    result if result >= 0 else 128 - result)
            except subprocess.TimeoutExpired:
                continue
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def main():
    # Private service entrypoint: check the actual kernel settings before any
    # workload runs. No shell or second round of systemd '$' expansion is used.
    if len(sys.argv) >= 5 and sys.argv[1] == "--_exec":
        memory, unit = int(sys.argv[2]), sys.argv[3]
        verify_cgroup(memory, unit)
        os.execvpe(sys.argv[4], sys.argv[4:], os.environ)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--memory", "-m", type=memory_bytes, required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command
    if command[:1] == ["--"]:
        command = command[1:]
    if not command:
        parser.error("a command is required after --")
    unit = "compiler-test-" + uuid.uuid4().hex + ".service"
    return supervise(service_command(args.memory, command, unit, os.environ), unit)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("memlimit: " + str(error), file=sys.stderr)
        sys.exit(125)
