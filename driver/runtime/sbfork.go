// sb: fork without exec, for the compiler fork-server (rung F1). Not part of Go.
//
// A Go process is many threads and fork copies one. SBFork makes that sound for one narrow use: a
// quiet parent (one goroutine doing the work, nothing blocked in a system call that matters) forks
// children that then run as ordinary Go programs.
//
// The world is stopped and sysmon is locked out, so every other thread is parked on its note (or is
// sysmon, asleep, holding no runtime lock). In the child those threads do not exist: their M
// structures are dropped from the idle list and counted as freed, so the scheduler starts new threads
// instead of waking ghosts; a new sysmon is started; and the surviving thread learns its new thread id
// (preemption signals are sent to it by id).

package runtime

import "internal/runtime/syscall/linux"

// SBFork forks the process. It returns the child's pid in the parent, 0 in the child, -errno on failure.
func SBFork() int {
	var pid int
	stw := stopTheWorld(stwForTestResetDebugLog)
	systemstack(func() {
		var old sigset
		sigsave(&old)
		sigblock(false)
		lock(&sched.sysmonlock)
		lock(&sched.lock)
		r, _, e := linux.Syscall6(57 /* fork */, 0, 0, 0, 0, 0, 0)
		if e != 0 {
			pid = -int(e)
		} else {
			pid = int(r)
		}
		if pid == 0 {
			mp := getg().m
			mp.procid = uint64(gettid())
			// every other M is gone
			for sched.midle.pop() != nil {
			}
			sched.nmidle = 0
			sched.nmidlelocked = 0
			sched.nmsys = 0
			sched.nmfreed = sched.mnext - 1
			sched.nmspinning.Store(0)
			sched.sysmonwait.Store(false)
			noteclear(&sched.sysmonnote)
		}
		unlock(&sched.lock)
		unlock(&sched.sysmonlock)
		msigrestore(old)
	})
	startTheWorld(stw)
	if pid == 0 {
		systemstack(func() { newm(sysmon, nil, -1) })
	}
	return pid
}
