package cleaner

import (
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type CleaningConfig struct {
	// Параметры ёмкости (требуется хотя бы один)
	MinFreeSpace    *int64   // Минимальное свободное место в байтах (рекомендуется)
	MaxUsagePercent *float64 // Максимальный процент использования диска (0-100)
	MaxSize         *int64   // Максимальный размер в байтах (используйте, если информация о диске недоступна)

	// Дополнительные настройки
	TimeWindow      time.Duration // Интервал времени для агрегации файлов (по умолчанию: 5 минут)
	RemoveEmptyDirs bool          // Удалять ли пустые каталоги (по умолчанию: true)

	// MaxAge — альтернативный режим: удалить файлы старше этого возраста,
	// без учёта состояния диска (взаимоисключим с MinFreeSpace/MaxUsagePercent/MaxSize)
	MaxAge *time.Duration

	// MinTriggerSize — если задан вместе с MaxAge, режим "по возрасту" запускается если суммарный
	// вес target-папки (без учёта ExcludeExtensions/ExcludeDirs) превышает это значение в байтах
	MinTriggerSize *int64

	// MinTriggerFileCount — если задан вместе с MaxAge, режим "по возрасту" запускается, если
	// количество файлов в target-папке (без учёта ExcludeExtensions/ExcludeDirs) достигает этого
	// значения. Комбинируется с MinTriggerSize по ИЛИ: age-удаление запускается, если сработал
	// хотя бы один из заданных триггеров
	MinTriggerFileCount *int64

	// ExcludeDirs — имена директорий, которые нужно полностью
	// пропускать при сканировании и удалении, на любой глубине дерева
	ExcludeDirs []string

	// Cписок расширений файлов, которые НИКОГДА не учитываются при сканировании и никогда не удаляются
	ExcludeExtensions []string

	// excludeExtSet — нормализованный набор расширений, строится один раз в setDefaults().
	excludeExtSet map[string]struct{}

	// excludeDirSet — нормализованный набор шаблонов, строится один раз в setDefaults()
	excludeDirSet map[string]struct{}

	// DryRun — если true, все шаги выполняются, но os.Remove не вызывается.
	DryRun bool

	// Concurrency задаёт желаемый уровень параллелизма.
	Concurrency int

	// MaxConcurrency ограничивает максимальный уровень параллелизма. По умолчанию равен 4
	MaxConcurrency int

	Callbacks Callbacks

	// DiskInfo — внедрение зависимости для тестов; если nil, берётся
	// реализация по умолчанию (см. DiskInfoProvider в disk.go).
	DiskInfo DiskInfoProvider
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

	if len(c.ExcludeDirs) > 0 {
		c.excludeDirSet = make(map[string]struct{})
		for _, d := range c.ExcludeDirs {
			d = strings.ToLower(strings.TrimSpace(d))
			if d != "" {
				c.excludeDirSet[d] = struct{}{}
			}
		}
	}

	if c.Concurrency == 0 {
		c.Concurrency = runtime.NumCPU()
	}
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = 4
	}
	if c.DiskInfo == nil {
		c.DiskInfo = &DefaultDiskInfoProvider{}
	}
}

// ActualWorkerCount возвращает фактическое количество рабочих процессов
func (c *CleaningConfig) ActualWorkerCount() int {
	workers := c.Concurrency
	workers = min(workers, c.MaxConcurrency)
	return workers
}

// isExcluded сообщает, нужно ли пропустить файл целиком: не учитывать его
// в размере при сканировании и не удалять при очистке (см. ExcludeExtensions)
func (c *CleaningConfig) isExcluded(path string) bool {
	if len(c.excludeExtSet) == 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	_, found := c.excludeExtSet[ext]
	return found
}

// IsExcludedDir сообщает, нужно ли пропустить директорию целиком — сравнение идёт
// по имени директории (entry.Name()), поэтому совпадение срабатывает на любой глубине дерева.
// Регистр не учитывается: excludeDirSet хранит имена в нижнем регистре (см. setDefaults).
func (c *CleaningConfig) IsExcludedDir(name string) bool {
	if len(c.excludeDirSet) == 0 {
		return false
	}
	_, found := c.excludeDirSet[strings.ToLower(name)]
	return found
}

// validate для проверки корректности конфигурации
func (c *CleaningConfig) validate() error {
	if c.MinFreeSpace == nil && c.MaxUsagePercent == nil && c.MaxSize == nil && c.MaxAge == nil {
		return ErrNoCapacitySpecified
	}

	if c.MaxAge != nil && *c.MaxAge < 0 {
		return ErrInvalidConfig
	}

	if c.MinTriggerSize != nil && c.MaxAge == nil {
		return ErrInvalidConfig
	}

	if c.MinTriggerSize != nil && *c.MinTriggerSize < 0 {
		return ErrInvalidConfig
	}

	if c.MinTriggerFileCount != nil && c.MaxAge == nil {
		return ErrInvalidConfig
	}

	if c.MinTriggerFileCount != nil && *c.MinTriggerFileCount < 0 {
		return ErrInvalidConfig
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
