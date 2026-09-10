package gobackupcleaner

import (
	"os"
	"time"
)

// CleanBackup очищает резервные файлы согласно указанной конфигурации
func CleanBackup(dirPath string, config CleaningConfig) (CleaningReport, error) {
	startTime := time.Now()

	// Устанавливаем значения по умолчанию и проверяем конфигурацию
	config.setDefaults()
	if err := config.validate(); err != nil {
		return CleaningReport{}, err
	}

	// Проверяем, существует ли директория
	if _, err := os.Stat(dirPath); err != nil {
		if os.IsNotExist(err) {
			return CleaningReport{}, ErrDirectoryNotFound
		}
		return CleaningReport{}, err
	}

	// Получаем текущее использование диска
	currentUsage, err := config.DiskInfo.GetDiskUsage(dirPath)
	var diskUsageError error
	if err != nil {
		// Сохраняем ошибку, чтобы использовать её позже
		diskUsageError = err
		// Проверяем, можем ли мы продолжить без данных об использовании диска
		if config.MaxSize == nil {
			// Не можем продолжить без данных об использовании диска, если указан только MaxUsagePercent или MinFreeSpace
			return CleaningReport{}, err
		}
	}

	// Вычисляем целевой размер удаления
	var targetSize int64
	if diskUsageError != nil && config.MaxSize != nil {
		// Особый случай: не удалось получить данные об использовании диска, но указан MaxSize.
		// В этом случае мы просканируем все файлы и будем удалять, пока общий размер не станет меньше MaxSize.
		// Это позволяет cleaner'у работать в окружениях, где API получения информации о диске недоступен
		// (например, из-за ограниченных прав доступа, сетевого хранилища и т.д.)
		targetSize = -1 // Специальное значение, означающее "сканировать и удалять, пока размер не станет меньше MaxSize"
	} else {
		targetSize = calculateTargetSize(currentUsage, &config)
		if targetSize <= 0 {
			// Удалять ничего не нужно
			return CleaningReport{
				TotalDuration: time.Since(startTime),
			}, nil
		}
	}

	// Получаем размер блока
	blockSize, err := config.DiskInfo.GetBlockSize(dirPath)
	if err != nil {
		return CleaningReport{}, err
	}

	// Вызываем коллбэк OnStart
	if currentUsage != nil || targetSize == -1 {
		var usage DiskUsage
		if currentUsage != nil {
			usage = *currentUsage
		}
		callSafe(config.Callbacks.OnStart, StartInfo{
			TargetDir:    dirPath,
			CurrentUsage: usage,
			TargetSize:   targetSize,
		})
	}

	// Фаза 1: сканирование файлов
	scanStartTime := time.Now()
	scanner := newScanner(&config, blockSize)
	if err := scanner.scan(dirPath); err != nil {
		return CleaningReport{}, err
	}

	// Получаем отсортированные временные слоты
	timeSlots := scanner.getTimeSlots()
	if len(timeSlots) == 0 {
		// Файлы не найдены
		return CleaningReport{
			ScanDuration:  time.Since(scanStartTime),
			TotalDuration: time.Since(startTime),
		}, nil
	}

	// Вычисляем порог удаления
	var threshold time.Time
	var estimatedFiles int
	var estimatedSize int64

	if targetSize == -1 && config.MaxSize != nil {
		// Особый случай: удалять, пока общий размер не станет меньше MaxSize
		threshold, estimatedFiles, estimatedSize = calculateThresholdForMaxSize(timeSlots, *config.MaxSize)
	} else {
		threshold, estimatedFiles, estimatedSize = calculateThreshold(timeSlots, targetSize)
	}
	scanDuration := time.Since(scanStartTime)

	// Вызываем коллбэк OnScanComplete
	callSafe(config.Callbacks.OnScanComplete, ScanCompleteInfo{
		ScannedFiles:  scanner.getTotalFiles(),
		TotalSize:     getTotalSize(timeSlots),
		BlockSize:     blockSize,
		TimeThreshold: threshold,
		ScanDuration:  scanDuration,
	})

	// Фаза 2: удаление файлов
	deleteStartTime := time.Now()

	// Вызываем коллбэк OnDeleteStart
	callSafe(config.Callbacks.OnDeleteStart, DeleteStartInfo{
		EstimatedFiles: estimatedFiles,
		EstimatedSize:  estimatedSize,
	})

	deleter := newDeleter(&config, blockSize)
	if err := deleter.deleteFiles(dirPath, threshold); err != nil {
		return CleaningReport{}, err
	}

	// Фаза 3: удаление пустых директорий
	deletedDirs, _ := deleter.deleteEmptyDirs(dirPath)
	// Игнорируем ошибку, так как для удаления директорий она не критична

	deleteDuration := time.Since(deleteStartTime)
	deletedFiles, deletedSize, deletedBlocks := deleter.getStats()

	// Вызываем коллбэк OnComplete
	callSafe(config.Callbacks.OnComplete, CompleteInfo{
		DeletedFiles:     deletedFiles,
		DeletedSize:      deletedSize,
		DeletedBlockSize: deletedBlocks,
		DeletedDirs:      deletedDirs,
		DeleteDuration:   deleteDuration,
	})

	// Формируем отчёт
	return CleaningReport{
		DeletedFiles:     deletedFiles,
		DeletedSize:      deletedSize,
		DeletedBlockSize: deletedBlocks,
		DeletedDirs:      deletedDirs,
		ScanDuration:     scanDuration,
		DeleteDuration:   deleteDuration,
		TotalDuration:    time.Since(startTime),
		ScannedFiles:     scanner.getTotalFiles(),
		TimeThreshold:    threshold,
		BlockSize:        blockSize,
	}, nil
}

// calculateTargetSize вычисляет, сколько места нужно освободить.
// Каждое ограничение (MaxSize / MaxUsagePercent / MinFreeSpace) оценивается
// независимо, и побеждает НАИБОЛЬШИЙ из полученных размеров (а не их сумма):
// поскольку Used/Free/UsedPercent меняются согласованно по мере удаления
// файлов, выполнение самого строгого ограничения автоматически выполняет
// и более мягкие.
func calculateTargetSize(usage *DiskUsage, config *CleaningConfig) int64 {
	var targetSize int64

	// Проверяем MaxSize
	if config.MaxSize != nil {
		currentSize := int64(usage.Used)
		if currentSize > *config.MaxSize {
			size := currentSize - *config.MaxSize
			if size > targetSize {
				targetSize = size
			}
		}
	}

	// Проверяем MaxUsagePercent
	if config.MaxUsagePercent != nil {
		if usage.UsedPercent > *config.MaxUsagePercent {
			targetUsage := uint64(float64(usage.Total) * (*config.MaxUsagePercent / 100))
			if usage.Used > targetUsage {
				size := int64(usage.Used - targetUsage)
				if size > targetSize {
					targetSize = size
				}
			}
		}
	}

	// Проверяем MinFreeSpace
	if config.MinFreeSpace != nil {
		currentFree := int64(usage.Free)
		if currentFree < *config.MinFreeSpace {
			size := *config.MinFreeSpace - currentFree
			if size > targetSize {
				targetSize = size
			}
		}
	}

	return targetSize
}

// calculateThreshold вычисляет временной порог удаления.
// Проходит по временным слотам от старых к новым, накапливая размер, пока
// не будет достигнут targetSize, после чего устанавливает порог чуть позже
// этого слота. Deleter затем удаляет каждый файл, чьё время изменения
// строго меньше threshold, поэтому файлы в самом граничном слоте тоже
// должны быть удалены (именно поэтому здесь прибавляется +1 секунда,
// а не берётся время самого слота).
func calculateThreshold(slots []*timeSlot, targetSize int64) (time.Time, int, int64) {
	var accumulatedSize int64
	var accumulatedFiles int
	var threshold time.Time

	// Если слотов нет, возвращаем нулевое время
	if len(slots) == 0 {
		return time.Time{}, 0, 0
	}

	// Устанавливаем начальный порог как самое позднее время + 1 секунда
	// (чтобы по умолчанию ничего не удалялось)
	threshold = slots[len(slots)-1].time.Add(time.Second)

	for _, slot := range slots {
		accumulatedSize += slot.totalBlockSize
		accumulatedFiles += len(slot.files)

		if accumulatedSize >= targetSize {
			// Мы достигли целевого размера.
			// Включаем все файлы вплоть до этого слота включительно
			threshold = slot.time.Add(time.Second)
			break
		}
	}

	return threshold, accumulatedFiles, accumulatedSize
}

// getTotalSize вычисляет общий размер по временным слотам
func getTotalSize(slots []*timeSlot) int64 {
	var total int64
	for _, slot := range slots {
		total += slot.totalSize
	}
	return total
}

// calculateThresholdForMaxSize вычисляет временной порог для случая, когда
// общий размер должен стать меньше maxSize
func calculateThresholdForMaxSize(slots []*timeSlot, maxSize int64) (time.Time, int, int64) {
	var totalSize int64
	var remainingSize int64
	var deleteFiles int
	var deleteSize int64

	// Вычисляем общий размер
	for _, slot := range slots {
		totalSize += slot.totalBlockSize
	}

	// Если уже меньше maxSize, удалять не нужно
	if totalSize <= maxSize {
		return time.Time{}, 0, 0
	}

	// Начинаем с самых новых файлов и двигаемся назад.
	// Мы хотим сохранить как можно больше файлов, оставаясь в пределах maxSize
	remainingSize = totalSize

	// Ищем точку отсечения — удаляем старые файлы, пока не окажемся в пределах maxSize
	for i := 0; i < len(slots); i++ {
		slot := slots[i]

		// Удаляем весь этот слот целиком
		remainingSize -= slot.totalBlockSize
		deleteFiles += len(slot.files)
		deleteSize += slot.totalBlockSize

		// Проверяем, удалили ли мы уже достаточно
		if remainingSize <= maxSize {
			// Мы достигли цели — устанавливаем порог так, чтобы включить этот слот.
			// ПРИМЕЧАНИЕ: в отличие от calculateThreshold выше (где прибавляется
			// +1с), здесь прибавляется +1ч. Обоим достаточно оказаться после
			// округлённого через Truncate() времени слота и до следующего слота,
			// но это расхождение случайное, а не намеренное — учитывай это,
			// если когда-нибудь настроишь TimeWindow больше 1 часа.
			return slot.time.Add(time.Hour), deleteFiles, deleteSize
		}
	}

	// Если мы дошли до этой точки, значит нужно удалить всё (в норме такого быть не должно)
	if len(slots) > 0 {
		return time.Now().Add(time.Hour), deleteFiles, deleteSize
	}
	return time.Time{}, 0, 0
}
