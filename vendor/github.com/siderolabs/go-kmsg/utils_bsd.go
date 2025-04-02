// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package kmsg

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

func getBootTime() (time.Time, error) {
	timeval, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return time.Time{}, fmt.Errorf("could not get boot time: %w", err)
	}

	return time.Unix(timeval.Unix()), nil
}
