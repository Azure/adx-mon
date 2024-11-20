//go:build !linux

package main

func pinToCPU(cpu int) error { return nil }
