//go:build windows

package source

import (
	"os"
)

func statFile(path string) (uint64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	return 0, info.Size(), nil
}
