//go:build !windows

package source

import (
	"os"
	"syscall"
)

func statFile(path string) (uint64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return stat.Ino, info.Size(), nil
}
