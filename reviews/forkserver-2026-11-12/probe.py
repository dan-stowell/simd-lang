#!/usr/bin/env python3
"""Standalone review probes, not part of the test suite. Run from the repository root."""
import array
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
COMPILER = str(ROOT / "build/compile-sb")
STOCK = "/usr/local/go/pkg/tool/linux_amd64/compile"
CASE = Path(__file__).resolve().parent / "cases/socket-limit.go"


class Zygote:
    def __init__(self, env):
        self.sock, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4 << 20)
        self.sock.settimeout(10)
        # Unlike the driver, pass the actual descriptor number rather than assuming 3.
        self.proc = subprocess.Popen([COMPILER], env={**env, "SB_ZYGOTE": str(child.fileno())},
                                     pass_fds=(child.fileno(),), stdout=subprocess.DEVNULL)
        child.close()

    def compile(self, argv, env, directory, kill_child=False):
        fields = [str(directory), str(len(env)), *[f"{k}={v}" for k, v in env.items()], *argv]
        with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
            self.sock.sendmsg([b"F" + "\0".join(fields).encode()],
                              [(socket.SOL_SOCKET, socket.SCM_RIGHTS,
                                array.array("i", [out.fileno(), err.fileno()]))])
            pid = int(self.sock.recv(256))
            if kill_child:
                os.kill(pid, signal.SIGKILL)
            self.sock.send(b"W" + str(pid).encode())
            status = self.sock.recv(256).decode()
            out.seek(0)
            err.seek(0)
            return status, out.read(), err.read()

    def close(self):
        self.sock.close()
        self.proc.wait(timeout=10)


def socket_limit():
    # Each string is below Linux's per-string exec limit, and the total is below ARG_MAX.
    env = {**os.environ, "GOGC": "off"}
    for i in range(5):
        env[f"REVIEW_PAD_{i}"] = "x" * 90000
    with tempfile.TemporaryDirectory(prefix="forkreview-") as tmp:
        argv = [COMPILER, "-p", "main", "-o", tmp + "/out.o", str(CASE)]
        for name, compiler in [("stock", STOCK), ("patched exec", COMPILER)]:
            p = subprocess.run([compiler, *argv[1:]], env=env, capture_output=True, timeout=10)
            print(f"{name}: exit {p.returncode}")
            assert p.returncode == 0, p.stderr
        z = Zygote(env)
        try:
            z.compile(argv, env, tmp)
        except OSError as e:
            print(f"fork: {e}")
            assert e.errno == 90  # EMSGSIZE
        else:
            raise AssertionError("expected the driver-sized socket to reject the request")
        finally:
            z.close()
        module = Path(tmp) / "module"
        module.mkdir()
        (module / "go.mod").write_text("module review\n\ngo 1.27\n")
        (module / "main.go").write_bytes(CASE.read_bytes())
        for flags in ([], ["-zygote"]):
            argv = [str(ROOT / "build/sb"), "-C", str(module), "-compiler", COMPILER,
                    "-pipeline", "-noearly", "-o", tmp + "/bin", *flags, "."]
            p = subprocess.run(argv, env=env, capture_output=True, timeout=60)
            print(f"driver {flags or ['exec']}: exit {p.returncode} {p.stderr.decode().strip()}")
            assert p.returncode == (1 if flags else 0), p.stderr
            if flags:
                assert b"message too long" in p.stderr, p.stderr


def corpus():
    env = {k: v for k, v in os.environ.items() if not k.startswith("SB_")}
    env["GOGC"] = "100"
    paths = [p for p in sorted(Path("/usr/local/go/test").glob("*.go"))
             if not re.search(r"(?m)^\s*import\b", p.read_text())][:160]
    with tempfile.TemporaryDirectory(prefix="fork-corpus-") as tmp:
        generated = Path(tmp) / "boundary.go"
        # A raw string, Unicode identifier, alias, generic receiver, and line directive.
        text = '''package p
type C interface { ~int | ~int64 }
type Box[T C] struct { V T }
func (b Box[T]) Twice() T { return b.V+b.V }
type Alias[T C] = Box[T]
const π = 1 << 100
var _ = Alias[int]{V: int(π >> 99)}.Twice()
//line virtual.go:91
const Raw = `raw
string`
'''
        generated.write_text(text)
        sources = paths + [generated]
        # Byte and pipe-buffer boundaries, then source-file contents large enough for GC.
        for size in (63, 64, 65, 4095, 4096, 4097, 65535, 65536, 65537, 1048575, 1048576, 1048577):
            p = Path(tmp) / f"boundary-{size}.go"
            p.write_text("package p\n/*" + "x" * size + "*/\n" + text.split("\n", 1)[1])
            sources.append(p)
        z = Zygote(env)
        passed = 0
        fd_counts = set()
        try:
            for path in sources:
                output = Path(tmp) / "out.o"
                argv = [COMPILER, "-p", "p", "-o", str(output), str(path)]
                a = subprocess.run([STOCK, *argv[1:]], env=env, capture_output=True, timeout=10)
                stock_bytes = output.read_bytes() if a.returncode == 0 else None
                output.unlink(missing_ok=True)
                b = subprocess.run(argv, env=env, capture_output=True, timeout=10)
                exec_bytes = output.read_bytes() if b.returncode == 0 else None
                output.unlink(missing_ok=True)
                status, stdout, stderr = z.compile(argv, env, tmp)
                fork_status = int(status.split()[0])
                fork_bytes = output.read_bytes() if fork_status == 0 else None
                assert (a.returncode, a.stdout, a.stderr, stock_bytes) == (
                    b.returncode, b.stdout, b.stderr, exec_bytes), f"stock/exec differs: {path}"
                assert (a.returncode, a.stdout, a.stderr, stock_bytes) == (
                    fork_status, stdout, stderr, fork_bytes), f"stock/fork differs: {path}"
                fd_counts.add(len(list(Path(f"/proc/{z.proc.pid}/fd").iterdir())))
                passed += 1
        finally:
            z.close()
        print(f"PASS {passed} stock/exec/fork inputs: statuses, diagnostics, archive bytes")
        print(f"zygote open FD counts after each request: {sorted(fd_counts)}")


def signals():
    env = {**os.environ, "GOGC": "off"}
    with tempfile.TemporaryDirectory(prefix="fork-signal-") as tmp:
        fifo = Path(tmp) / "blocked.go"
        os.mkfifo(fifo)
        argv = [COMPILER, "-p", "main", "-o", tmp + "/out.o", str(fifo)]
        for compiler in [STOCK, COMPILER]:
            p = subprocess.Popen([compiler, *argv[1:]], env=env,
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            p.kill()
            print(f"{Path(compiler).name} exec killed: status {p.wait(timeout=10)}")
        z = Zygote(env)
        try:
            status, _, _ = z.compile(argv, env, tmp, kill_child=True)
            assert int(status.split()[0]) == 137, status
            print(f"fork killed: wait status {status.split()[0]} (nonzero as expected)")
            argv[-1] = str(CASE)
            status, _, stderr = z.compile(argv, env, tmp)
            assert int(status.split()[0]) == 0, (status, stderr)
            print("PASS zygote survives a killed child and compiles the next request")
        finally:
            z.close()


if __name__ == "__main__":
    if sys.argv[1:] == ["socket-limit"]:
        socket_limit()
    elif sys.argv[1:] == ["corpus"]:
        corpus()
    elif sys.argv[1:] == ["signals"]:
        signals()
    else:
        raise SystemExit("usage: probe.py socket-limit|corpus|signals")
