// sb: the compiler as a fork-server (rung F1). Not part of Go.
//
// With SB_ZYGOTE=<fd> the compiler does not compile: it has finished process start (the runtime, every
// package's init) and now waits on a SOCK_SEQPACKET socket. Each "F" request carries a working
// directory, an environment and a command line, and two descriptors (stdout, stderr); the zygote forks
// (runtime.SBFork), answers with the child's pid, and the child returns from sbZygote into the ordinary
// main with those in place. A "W" request waits for one child and answers with its exit status and
// resource use. Everything happens on the main goroutine, so at the fork no other thread matters.

package main

import (
	"cmd/compile/internal/noder"
	"cmd/compile/internal/reflectdata"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func sbZygote() {
	v := os.Getenv("SB_ZYGOTE")
	if v == "" {
		return
	}
	sock, _ := strconv.Atoi(v)
	buf := make([]byte, 1<<20)
	oob := make([]byte, 128)
	for {
		n, oobn, _, _, err := syscall.Recvmsg(sock, buf, oob, 0)
		if err == syscall.EINTR {
			continue
		}
		if err != nil || n == 0 {
			os.Exit(0) // the driver is gone
		}
		switch buf[0] {
		case 'W':
			pid, _ := strconv.Atoi(string(buf[1:n]))
			var ws syscall.WaitStatus
			var ru syscall.Rusage
			for {
				if _, err = syscall.Wait4(pid, &ws, 0, &ru); err != syscall.EINTR {
					break
				}
			}
			st := ws.ExitStatus()
			if err != nil || !ws.Exited() {
				st = 255
				if ws.Signaled() {
					st = 128 + int(ws.Signal())
				}
			}
			us := func(t syscall.Timeval) int64 { return t.Sec*1e6 + t.Usec }
			syscall.Write(sock, []byte(fmt.Sprintf("%d %d %d %d %d", st, us(ru.Utime), us(ru.Stime), ru.Minflt, ru.Majflt)))
		case 'F':
			var fds []int
			if msgs, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil {
				for _, m := range msgs {
					if f, err := syscall.ParseUnixRights(&m); err == nil {
						fds = append(fds, f...)
					}
				}
			}
			if len(fds) != 2 {
				syscall.Write(sock, []byte("-1"))
				continue
			}
			parts := strings.Split(string(buf[1:n]), "\x00")
			pid := runtime.SBFork()
			if pid != 0 {
				syscall.Close(fds[0])
				syscall.Close(fds[1])
				syscall.Write(sock, []byte(strconv.Itoa(pid)))
				continue
			}
			// the child: become the compiler this request describes
			syscall.Close(sock)
			syscall.Dup2(fds[0], 1)
			syscall.Dup2(fds[1], 2)
			syscall.Close(fds[0])
			syscall.Close(fds[1])
			nenv, _ := strconv.Atoi(parts[1])
			os.Clearenv()
			for _, kv := range parts[2 : 2+nenv] {
				if i := strings.IndexByte(kv, '='); i > 0 {
					os.Setenv(kv[:i], kv[i+1:])
				}
			}
			os.Unsetenv("SB_ZYGOTE")
			if err := os.Chdir(parts[0]); err != nil {
				fmt.Fprintln(os.Stderr, "sb zygote:", err)
				os.Exit(2)
			}
			os.Setenv("PWD", parts[0])
			os.Args = parts[2+nenv:]
			// What the compiler read from the environment while it was still the zygote and that differs
			// from compile to compile. (Everything else it reads at init is the same for a whole build: the
			// driver starts the zygote with the environment it gives every compile.)
			noder.SbShard, noder.SbShards = noder.SbShardEnv()
			reflectdata.SbShardOther = noder.SbShards != 0 && noder.SbShard != 0
			return
		}
	}
}
