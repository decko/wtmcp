//go:build !unix

package plugin

import "os"

func rejectHandlerHardlink(_ os.FileInfo) error {
	return nil
}
