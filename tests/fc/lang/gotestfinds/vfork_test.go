package gotestfinds

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// A process that starts with its soft open-file limit under the hard one has the limit raised by package
// os, and syscall.forkAndExecInChild1 puts the old one back in the vfork child, which runs in its parent's
// frame. fc kept the address of err1 (`pid, err1 = rawVforkSyscall(...)`) in a frame temporary across the
// call; the child's extra statement reused the temporary, and the parent stored through it: a
// segmentation fault with every signal blocked, in every fc-built program that started another program
// (found by running the Go test directory inside a systemd service, where the soft limit is 1024).
func TestVforkChildFrame(t *testing.T) {
	if os.Getenv("FCLANG_VFORK") == "1" {
		for i := 0; i < 3; i++ {
			if err := exec.Command("/bin/true").Run(); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Skip(err)
	}
	if lim.Max < 512 {
		t.Skip("hard limit too low")
	}
	// (the limits of this process as it was started: package os has raised the soft one since)
	cmd := exec.Command("prlimit", "--nofile=256:"+itoa(lim.Max), os.Args[0], "-test.run=^TestVforkChildFrame$")
	cmd.Env = append(os.Environ(), "FCLANG_VFORK=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a program started with a soft open-file limit under the hard one could not start another: %v\n%s", err, out)
	}
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
