//go:build linux || darwin

package config

import (
	"os"
	"syscall"
)

func preserveOwner(path string, source os.FileInfo) error {
	sourceStat, sourceOK := source.Sys().(*syscall.Stat_t)
	current, err := os.Stat(path)
	if err != nil {
		return err
	}
	currentStat, currentOK := current.Sys().(*syscall.Stat_t)
	if !sourceOK || !currentOK ||
		(sourceStat.Uid == currentStat.Uid && sourceStat.Gid == currentStat.Gid) {
		return nil
	}
	return os.Chown(path, int(sourceStat.Uid), int(sourceStat.Gid))
}
