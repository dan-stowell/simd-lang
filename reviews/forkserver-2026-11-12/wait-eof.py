#!/usr/bin/env python3
"""Replay a lost wait reply against the repository's verbatim driver functions."""
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
source = (root / "driver/main.go").read_text()
functions = source[source.index("var zygoteOn bool"):source.index("func ms()")]
header = r'''package main
import (
 "bufio"; "bytes"; "fmt"; "io"; "os"; "os/exec"; "strconv"; "strings"; "sync";
 "sync/atomic"; "syscall"
)
var compiler = os.Args[1]
var noEarly = true
var taxProcs, taxForks, taxMinflt, taxMajflt atomic.Int64
'''
main = r'''
func main() {
 driver, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
 if err != nil { panic(err) }
 real, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
 if err != nil { panic(err) }
 cmd := exec.Command(compiler)
 cmd.Env = append(os.Environ(), "SB_ZYGOTE=3")
 childSocket := os.NewFile(uintptr(real[1]), "zygote")
 cmd.ExtraFiles = []*os.File{childSocket}
 cmd.Stderr = os.Stderr
 if err := cmd.Start(); err != nil { panic(err) }
 childSocket.Close()
 zy.once.Do(func() {})
 zy.sock = driver[0]
 done := make(chan struct{})
 go func() {
  defer close(done)
  buf, oob := make([]byte, 1<<20), make([]byte, 128)
  n, on, _, _, err := syscall.Recvmsg(driver[1], buf, oob, 0)
  if err != nil { panic(err) }
  if err := syscall.Sendmsg(real[0], buf[:n], oob[:on], nil, 0); err != nil { panic(err) }
  cms, _ := syscall.ParseSocketControlMessage(oob[:on])
  for _, cm := range cms {
   fds, _ := syscall.ParseUnixRights(&cm)
   for _, fd := range fds { syscall.Close(fd) }
  }
  n, err = syscall.Read(real[0], buf)
  if err != nil { panic(err) }
  syscall.Write(driver[1], buf[:n])
  n, err = syscall.Read(driver[1], buf)
  if err != nil || n == 0 || buf[0] != 'W' { panic("expected W") }
  syscall.Write(real[0], buf[:n])
  n, err = syscall.Read(real[0], buf)
  if err != nil { panic(err) }
  fmt.Printf("real compiler wait reply (discarded to model zygote death): %s\n", buf[:n])
  // A peer dying after reading W produces exactly this EOF at the driver's Read.
  syscall.Close(driver[1])
 }()
 _, _, err = zygoteCompile(os.Args[2],
  []string{compiler, "-p", "p", "-o", os.Args[2]+"/out.o", os.Args[3]},
  func(int) {}, false)
 <-done
 fmt.Printf("zygoteCompile error: %v\n", err)
 syscall.Close(driver[0])
 syscall.Close(real[0])
 cmd.Wait()
 if err != nil { os.Exit(1) }
}
'''
with tempfile.TemporaryDirectory(prefix="fork-eof-") as tmp:
    file = Path(tmp) / "replay.go"
    file.write_text(header + functions + main)
    subprocess.run(["go", "run", str(file), str(root / "build/compile-sb"), tmp,
                    str(Path(__file__).resolve().parent / "cases/wait-eof.go")],
                   check=True, timeout=30)
