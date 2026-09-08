package gobackupcleaner

import "time"

// CleaningReport представляет результат операции очистки
type CleaningReport struct {
	// Статистика удаления
	DeletedFiles     int   // Количество удалённых файлов
	DeletedSize      int64 // Фактический размер файлов в байтах
	DeletedBlockSize int64 // Размер с учётом выравнивания по блокам, в байтах
	DeletedDirs      int   // Количество удалённых директорий

	// Время обработки
	ScanDuration   time.Duration // Время, затраченное на сканирование файлов
	DeleteDuration time.Duration // Время, затраченное на удаление файлов
	TotalDuration  time.Duration // Общее время обработки

	// Прочая информация
	ScannedFiles  int       // Общее количество отсканированных файлов
	TimeThreshold time.Time // Временной порог удаления
	BlockSize     int64     // Размер блока файловой системы
}
