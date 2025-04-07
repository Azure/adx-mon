// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build linux

package kmsg

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

func getBootTime() (time.Time, error) {
	var sysinfo unix.Sysinfo_t

	err := unix.Sysinfo(&sysinfo)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not get boot time: %w", err)
	}

	// sysinfo only has seconds
	return time.Now().Add(-1 * (time.Duration(sysinfo.Uptime) * time.Second)), nil
}
