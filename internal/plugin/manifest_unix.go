//go:build unix

package plugin

import (
	"fmt"
	"os"
	"syscall"
)

func rejectHandlerHardlink(info os.FileInfo) error {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if sys.Nlink > 1 {
		return fmt.Errorf("hardlink detected (nlink=%d, not allowed in user plugins)", sys.Nlink)
	}
	return nil
}
