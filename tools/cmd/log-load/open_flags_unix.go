//go:build unix

package main

import "golang.org/x/sys/unix"

const writerOpenFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW
