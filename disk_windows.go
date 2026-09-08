//go:build windows
// +build windows

package gobackupcleaner

import (
	"errors"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Эта реализация использует Windows API напрямую через syscall,
// внешние зависимости не требуются

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetDiskFreeSpace   = kernel32.NewProc("GetDiskFreeSpaceW")
)

// GetDiskUsage возвращает информацию об использовании диска для указанного пути
func (d *DefaultDiskInfoProvider) GetDiskUsage(path string) (*DiskUsage, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	// Для несуществующих путей нужно проверять сам путь, а не только том.
	// Сначала пробуем получить информацию о диске по самому пути, затем — по тому
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	// Преобразуем путь в UTF16 для Windows API
	pathPtr, err := syscall.UTF16PtrFromString(absPath)
	if err != nil {
		return nil, err
	}

	// Сначала пробуем с фактическим путём
	ret, _, err := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)

	if ret == 0 {
		// Если путь не существует, вызов должен завершиться ошибкой
		return nil, err
	}

	used := totalBytes - totalFreeBytes

	if totalBytes == 0 {
		return nil, errors.New("total disk size is 0")
	}

	usedPercent := float64(used) / float64(totalBytes) * 100

	return &DiskUsage{
		Total:       totalBytes,
		Free:        freeBytesAvailable,
		Used:        used,
		UsedPercent: usedPercent,
	}, nil
}

// GetBlockSize возвращает размер блока для указанного пути
func (d *DefaultDiskInfoProvider) GetBlockSize(path string) (int64, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}

	// Преобразуем путь в UTF16 для Windows API
	pathPtr, err := syscall.UTF16PtrFromString(absPath)
	if err != nil {
		return 0, err
	}

	var sectorsPerCluster, bytesPerSector, numberOfFreeClusters, totalNumberOfClusters uint32

	// Сначала пробуем с фактическим путём
	ret, _, err := procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)),
		uintptr(unsafe.Pointer(&numberOfFreeClusters)),
		uintptr(unsafe.Pointer(&totalNumberOfClusters)),
	)

	if ret == 0 {
		// Если путь не существует, вызов должен завершиться ошибкой
		return 0, err
	}

	// Размер кластера — это эффективный "размер блока" в Windows
	clusterSize := int64(sectorsPerCluster) * int64(bytesPerSector)
	return clusterSize, nil
}
