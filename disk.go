package gobackupcleaner

// DiskUsage представляет информацию об использовании диска
type DiskUsage struct {
	Total       uint64
	Free        uint64
	Used        uint64
	UsedPercent float64
}

// DiskInfoProvider — интерфейс для получения информации о диске
type DiskInfoProvider interface {
	GetDiskUsage(path string) (*DiskUsage, error)
	GetBlockSize(path string) (int64, error)
}

// DefaultDiskInfoProvider — реализация DiskInfoProvider по умолчанию
type DefaultDiskInfoProvider struct{}

// calculateBlockSize вычисляет фактический размер блока, занимаемого файлом
func calculateBlockSize(fileSize int64, blockSize int64) int64 {
	if blockSize <= 0 {
		return fileSize
	}
	blocks := (fileSize + blockSize - 1) / blockSize
	return blocks * blockSize
}
