//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	routerKernel          = syscall.NewLazyDLL("kernel32.dll")
	routerCreateJob       = routerKernel.NewProc("CreateJobObjectW")
	routerSetJob          = routerKernel.NewProc("SetInformationJobObject")
	routerAssignJob       = routerKernel.NewProc("AssignProcessToJobObject")
	routerTerminateJob    = routerKernel.NewProc("TerminateJobObject")
	routerQueryJob        = routerKernel.NewProc("QueryInformationJobObject")
	routerSnapshot        = routerKernel.NewProc("CreateToolhelp32Snapshot")
	routerThreadFirst     = routerKernel.NewProc("Thread32First")
	routerThreadNext      = routerKernel.NewProc("Thread32Next")
	routerOpenThread      = routerKernel.NewProc("OpenThread")
	routerResumeThread    = routerKernel.NewProc("ResumeThread")
	routerGetProcessID    = routerKernel.NewProc("GetProcessId")
	routerThreadProcessID = routerKernel.NewProc("GetProcessIdOfThread")
	routerAssignJobCall   = func(job, process uintptr) (uintptr, uintptr, error) {
		return routerAssignJob.Call(job, process)
	}
)

const (
	routerCreateSuspended     = 0x4
	routerSnapshotThreads     = 0x4
	routerThreadSuspendResume = 0x2
	routerThreadQueryLimited  = 0x800
	routerProcessTerminate    = 0x1
	routerProcessSetQuota     = 0x100
	routerProcessQueryLimited = 0x1000
	routerSynchronize         = 0x100000
)

type routerThreadEntry struct {
	Size, Usage, ThreadID, OwnerPID uint32
	BasePriority, DeltaPriority     int32
	Flags                           uint32
}

type routerJobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodUserTime, ThisPeriodKernelTime       int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
}

type routerJobBasicLimits struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}

type routerJobExtendedLimits struct {
	Basic                                                                        routerJobBasicLimits
	IO                                                                           [6]uint64
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

type routerOwnedTree struct {
	mu       sync.Mutex
	job      syscall.Handle
	process  *os.Process
	assigned bool
	stopped  bool
}

func routerCallError(name string, err error) error {
	return fmt.Errorf("%s: %w", name, err)
}

func startRouterOwnedTree(cmd *exec.Cmd) (*routerOwnedTree, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 || unsafe.Sizeof(routerJobExtendedLimits{}) != 144 {
		return nil, errors.New("unsupported Windows job object layout")
	}
	job, _, err := routerCreateJob.Call(0, 0)
	if job == 0 {
		return nil, routerCallError("CreateJobObject", err)
	}
	tree := &routerOwnedTree{job: syscall.Handle(job)}
	limits := routerJobExtendedLimits{}
	limits.Basic.LimitFlags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	ok, _, setErr := routerSetJob.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(tree.job)
		return nil, routerCallError("SetInformationJobObject", setErr)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.CreationFlags&0x01000000 != 0 {
		syscall.CloseHandle(tree.job)
		return nil, errors.New("CREATE_BREAKAWAY_FROM_JOB is unsupported")
	}
	cmd.SysProcAttr.CreationFlags |= routerCreateSuspended
	if err := cmd.Start(); err != nil {
		syscall.CloseHandle(tree.job)
		return nil, err
	}
	tree.process = cmd.Process
	// cmd.Process retains a handle, preventing PID reuse until the caller waits.
	process, err := syscall.OpenProcess(routerProcessTerminate|routerProcessSetQuota|routerProcessQueryLimited|routerSynchronize, false, uint32(cmd.Process.Pid))
	if err != nil {
		cleanupErr := stopRouterOwnedTree(tree)
		return tree, errors.Join(err, cleanupErr)
	}
	defer syscall.CloseHandle(process)
	fail := func(cause error) (*routerOwnedTree, error) {
		cleanupErr := stopRouterOwnedTree(tree)
		return tree, errors.Join(cause, cleanupErr)
	}
	pid, _, err := routerGetProcessID.Call(uintptr(process))
	if pid == 0 || uint32(pid) != uint32(cmd.Process.Pid) {
		return fail(routerCallError("GetProcessId", err))
	}
	snapshot, _, err := routerSnapshot.Call(routerSnapshotThreads, 0)
	if snapshot == ^uintptr(0) {
		return fail(routerCallError("CreateToolhelp32Snapshot", err))
	}
	entry := routerThreadEntry{Size: uint32(unsafe.Sizeof(routerThreadEntry{}))}
	var threadID uint32
	var count int
	ok, _, scanErr := routerThreadFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.OwnerPID == uint32(pid) {
			count++
			threadID = entry.ThreadID
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, scanErr = routerThreadNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	syscall.CloseHandle(syscall.Handle(snapshot))
	if scanErr != syscall.Errno(18) {
		return fail(routerCallError("Thread32Next", scanErr))
	}
	if count != 1 {
		return fail(fmt.Errorf("expected one suspended thread, found %d", count))
	}
	thread, _, err := routerOpenThread.Call(routerThreadSuspendResume|routerThreadQueryLimited, 0, uintptr(threadID))
	if thread == 0 {
		return fail(routerCallError("OpenThread", err))
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	owner, _, err := routerThreadProcessID.Call(thread)
	if owner == 0 {
		return fail(routerCallError("GetProcessIdOfThread", err))
	}
	if owner != pid {
		return fail(fmt.Errorf("thread owner changed"))
	}
	ok, _, err = routerAssignJobCall(job, uintptr(process))
	if ok == 0 {
		return fail(routerCallError("AssignProcessToJobObject", err))
	}
	tree.assigned = true
	previous, _, err := routerResumeThread.Call(thread)
	if previous != 1 {
		// A successful resume with an unexpected count may have run code: kill the whole job.
		if previous == 0xffffffff {
			return fail(routerCallError("ResumeThread", err))
		}
		return fail(fmt.Errorf("unexpected thread suspend count %d", previous))
	}
	return tree, nil
}

func stopRouterOwnedTree(tree *routerOwnedTree) error {
	if tree == nil {
		return nil
	}
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.job == 0 {
		if tree.stopped {
			return nil
		}
		return errors.New("job termination not verified")
	}
	if !tree.assigned && tree.process != nil {
		process, err := syscall.OpenProcess(routerProcessTerminate|routerSynchronize, false, uint32(tree.process.Pid))
		if err != nil {
			return routerCallError("OpenProcess for cleanup", err)
		}
		killErr := syscall.TerminateProcess(process, 1)
		status, waitErr := syscall.WaitForSingleObject(process, 5000)
		syscall.CloseHandle(process)
		if waitErr != nil || status != syscall.WAIT_OBJECT_0 {
			return errors.Join(killErr, waitErr, fmt.Errorf("process termination not verified: wait status %d", status))
		}
		tree.process = nil
	}
	ok, _, err := routerTerminateJob.Call(uintptr(tree.job), 1)
	if ok == 0 {
		return routerCallError("TerminateJobObject", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var info routerJobAccounting
		ok, _, err = routerQueryJob.Call(uintptr(tree.job), 1, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
		if ok == 0 {
			return routerCallError("QueryInformationJobObject", err)
		}
		if info.ActiveProcesses == 0 {
			if err := syscall.CloseHandle(tree.job); err != nil {
				return err
			}
			tree.job = 0
			tree.stopped = true
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job still has %d active processes", info.ActiveProcesses)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A successful stop or release confirms the tree is gone; cmd.Wait remains the caller's responsibility.
func releaseRouterOwnedTree(tree *routerOwnedTree) error {
	return stopRouterOwnedTree(tree)
}
