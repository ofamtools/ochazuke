package sysstat

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// macOS has no sysctl for CPU ticks: host_statistics(HOST_CPU_LOAD_INFO)
// is a Mach call, reached without cgo through purego (as gopsutil does).

const (
	hostCPULoadInfo = 3 // HOST_CPU_LOAD_INFO
	cpuStates       = 4 // user, system, idle, nice
	cpuStateIdle    = 2
)

var mach struct {
	once  sync.Once
	err   error
	host  uint32
	stats func(host uint32, flavor int32, info unsafe.Pointer, count *uint32) int32
}

func loadMach() error {
	mach.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			mach.err = err
			return
		}
		var self func() uint32
		purego.RegisterLibFunc(&self, lib, "mach_host_self")
		purego.RegisterLibFunc(&mach.stats, lib, "host_statistics")
		mach.host = self()
	})
	return mach.err
}

// readTicks reads the machine-wide CPU ticks.
func readTicks() (ticks, error) {
	if err := loadMach(); err != nil {
		return ticks{}, err
	}
	var load [cpuStates]uint32
	count := uint32(cpuStates)
	if status := mach.stats(mach.host, hostCPULoadInfo, unsafe.Pointer(&load), &count); status != 0 {
		return ticks{}, fmt.Errorf("host_statistics: kern_return %d", status)
	}
	var t ticks
	for _, v := range load {
		t.total += uint64(v)
	}
	t.idle = uint64(load[cpuStateIdle])
	return t, nil
}

// readMemory reads hw.memsize and the VM page counts: used is the total
// less the free, speculative, file-backed and purgeable pages, near what
// Activity Monitor calls Memory Used.
func readMemory() (Memory, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return Memory{}, err
	}
	page, err := unix.SysctlUint32("hw.pagesize")
	if err != nil {
		return Memory{}, err
	}
	var free uint64
	for _, name := range []string{"vm.page_free_count", "vm.page_speculative_count", "vm.page_pageable_external_count", "vm.page_purgeable_count"} {
		n, err := sysctlCount(name)
		if err != nil {
			return Memory{}, err
		}
		free += n * uint64(page)
	}
	return Memory{Used: total - min(free, total), Total: total}, nil
}

// sysctlCount reads a page count: most are 32-bit, vm.page_purgeable_count
// is 64-bit, and the kernel refuses a buffer of the wrong size.
func sysctlCount(name string) (uint64, error) {
	if n, err := unix.SysctlUint32(name); err == nil {
		return uint64(n), nil
	}
	return unix.SysctlUint64(name)
}
