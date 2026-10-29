//go:build !windows

package privatefs

import (
	"fmt"
	"os"
)

func Directory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("cannot create private output directory")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("output directory must be private (0700)")
	}
	return nil
}
