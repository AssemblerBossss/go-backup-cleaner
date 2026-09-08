package gobackupcleaner

import (
	"runtime"
	"strings"
	"time"
)

// CleaningConfig represents the configuration for cleaning operations
type CleaningConfig struct {
	// Параметры ёмкости (требуется хотя бы один)
	// MinFreeSpace — рекомендуемый основной параметр для большинства случаев использования.
	MinFreeSpace    *int64   // Минимальное свободное место в байтах (рекомендуется)
	MaxUsagePercent *float64 // Максимальный процент использования диска (0-100)
	MaxSize         *int64   // Максимальный размер в байтах (используйте, если информация о диске недоступна)

	// Дополнительные настройки
	TimeWindow      time.Duration // Интервал времени для агрегации файлов (по умолчанию: 5 минут)
	RemoveEmptyDirs bool          // Удалять ли пустые каталоги (по умолчанию: true)

	// Cписок расширений файлов, которые НИКОГДА не учитываются при сканировании и никогда не удаляются
	ExcludeExtensions []string

	// excludeExtSet — нормализованный набор расширений для O(1)-проверки.
	// Строится один раз в setDefaults(), чтобы не гонять по срезу
	// ExcludeExtensions на каждый файл при сканировании миллионов файлов.
	excludeExtSet map[string]struct{}

	// Настройки параллелизма
	// Concurrency задаёт желаемый уровень параллелизма.
	// Если значение 0, по умолчанию используется runtime.NumCPU().
	Concurrency int

	// MaxConcurrency ограничивает максимальный уровень параллелизма.
	// По умолчанию равен 4, так как тесты производительности показывают убывающую отдачу при превышении этого значения.
	// Фактический уровень параллелизма будет равен min(Concurrency, MaxConcurrency).
	MaxConcurrency int

	// Callbacks
	Callbacks Callbacks

	// Внедрение зависимостей
	// DiskInfo позволяет подставить в тестах поддельный источник DiskUsage/BlockSize
	// вместо обращения к реальной файловой системе (см. DiskInfoProvider в disk.go).
	DiskInfo DiskInfoProvider // Если nil, используется реализация по умолчанию
}

// setDefaults устанавливает значения по умолчанию для конфигурации
func (c *CleaningConfig) setDefaults() {
	if c.TimeWindow == 0 {
		c.TimeWindow = 5 * time.Minute
	}

	if len(c.ExcludeExtensions) > 0 {
		c.excludeExtSet = make(map[string]struct{})
		for _, ext := range c.ExcludeExtensions {
			ext = strings.ToLower(strings.TrimSpace(ext))
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			c.excludeExtSet[ext] = struct{}{}
		}
	}

	// Устанавливаем параллелизм по умолчанию равным количеству CPU, если не указано
	if c.Concurrency == 0 {
		c.Concurrency = runtime.NumCPU()
	}

	// Устанавливаем максимальный параллелизм по умолчанию
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = 4
	}

	if c.DiskInfo == nil {
		c.DiskInfo = &DefaultDiskInfoProvider{}
	}
	// RemoveEmptyDirs по умолчанию true, но мы не можем переопределить явное false
	// поэтому не устанавливаем его здесь — пусть решает вызывающий код
}

// ActualWorkerCount возвращает фактическое количество рабочих процессов, которое будет использовано
func (c *CleaningConfig) ActualWorkerCount() int {
	workers := c.Concurrency
	if workers > c.MaxConcurrency {
		workers = c.MaxConcurrency
	}
	return workers
}

// validate проверяет, является ли конфигурация допустимой
func (c *CleaningConfig) validate() error {
	if c.MinFreeSpace == nil && c.MaxUsagePercent == nil && c.MaxSize == nil {
		return ErrNoCapacitySpecified
	}

	if c.MinFreeSpace != nil && *c.MinFreeSpace < 0 {
		return ErrInvalidConfig
	}

	if c.MaxUsagePercent != nil && (*c.MaxUsagePercent < 0 || *c.MaxUsagePercent > 100) {
		return ErrInvalidConfig
	}

	if c.MaxSize != nil && *c.MaxSize < 0 {
		return ErrInvalidConfig
	}

	if c.TimeWindow < 0 {
		return ErrInvalidConfig
	}

	if c.Concurrency < 0 {
		return ErrInvalidConfig
	}

	if c.MaxConcurrency < 0 {
		return ErrInvalidConfig
	}

	return nil
}
