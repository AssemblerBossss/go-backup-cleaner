//go:build !windows
// +build !windows

package gobackupcleaner

import (
	"errors"
	"syscall"
)

// GetDiskUsage возвращает информацию об использовании диска для указанного пути
func (d *DefaultDiskInfoProvider) GetDiskUsage(path string) (*DiskUsage, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return nil, err
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free

	if total == 0 {
		return nil, errors.New("total disk size is 0")
	}

	usedPercent := float64(used) / float64(total) * 100

	return &DiskUsage{
		Total:       total,
		Free:        free,
		Used:        used,
		UsedPercent: usedPercent,
	}, nil
}

// GetBlockSize возвращает размер блока для указанного пути
func (d *DefaultDiskInfoProvider) GetBlockSize(path string) (int64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0, err
	}
	return int64(stat.Bsize), nil
}
