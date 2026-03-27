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

func getBootTimeOffset() (time.Duration, error) {
	timeval, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0, fmt.Errorf("could not get boot time: %w", err)
	}

	return time.Until(time.Unix(timeval.Unix())), nil
}
