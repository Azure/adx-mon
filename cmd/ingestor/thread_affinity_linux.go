package main

import (
	"golang.org/x/sys/unix"
)

func pinToCPU(cpu int) error {
	var newMask unix.CPUSet
	newMask.Set(cpu)
	return unix.SchedSetaffinity(cpu, &newMask)
}
