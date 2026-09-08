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

// GetDiskFreeSpace возвращает доступное место на диске для указанного пути
// директории, используя провайдер информации о диске по умолчанию.
// Это вспомогательная функция, полезная для быстрой проверки необходимости
// очистки перед выполнением самой операции очистки резервных копий.
//
// При использовании конфигурации MinFreeSpace (рекомендуется) эту функцию
// можно использовать для предварительной проверки необходимости очистки:
//
//	freeSpace, err := GetDiskFreeSpace("/backup")
//	if err == nil && freeSpace < requiredSpace {
//	    // Выполняем очистку
//	}
func GetDiskFreeSpace(dirPath string) (int64, error) {
	provider := &DefaultDiskInfoProvider{}
	return GetDiskFreeSpaceWithProvider(dirPath, provider)
}

// GetDiskFreeSpaceWithProvider возвращает доступное место на диске для
// указанного пути директории, используя пользовательский провайдер
// информации о диске. Это позволяет внедрять зависимости и тестировать
// с помощью mock-провайдеров.
//
// Пример с пользовательским провайдером:
//
//	provider := &CustomDiskInfoProvider{}
//	freeSpace, err := GetDiskFreeSpaceWithProvider("/backup", provider)
func GetDiskFreeSpaceWithProvider(dirPath string, provider DiskInfoProvider) (int64, error) {
	usage, err := provider.GetDiskUsage(dirPath)
	if err != nil {
		return 0, err
	}
	return int64(usage.Free), nil
}
