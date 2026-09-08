package gobackupcleaner

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileInfo представляет информацию о файле
type fileInfo struct {
	path      string
	size      int64
	blockSize int64
	modTime   time.Time
}

// timeSlot представляет файлы, сгруппированные по временному интервалу
type timeSlot struct {
	time           time.Time
	files          []fileInfo
	totalSize      int64
	totalBlockSize int64
}

// scanTask представляет задачу для параллельного сканирования
type scanTask struct {
	path string
}

// scanner управляет операциями сканирования файлов
type scanner struct {
	config      *CleaningConfig
	blockSize   int64
	workerCount int
	mu          sync.Mutex
	timeSlots   map[time.Time]*timeSlot
}

// newScanner создаёт новый экземпляр сканера
func newScanner(config *CleaningConfig, blockSize int64) *scanner {
	return &scanner{
		config:      config,
		blockSize:   blockSize,
		workerCount: config.ActualWorkerCount(),
		timeSlots:   make(map[time.Time]*timeSlot),
	}
}

// scan выполняет параллельное сканирование файлов
func (s *scanner) scan(rootPath string) error {
	taskChan := make(chan scanTask, 100)
	errChan := make(chan error, s.workerCount)
	var wg sync.WaitGroup
	var taskWg sync.WaitGroup

	// Запускаем рабочие процессы
	for i := 0; i < s.workerCount; i++ {
		wg.Add(1)
		go s.worker(taskChan, errChan, &wg, &taskWg)
	}

	// Начинаем с корневой директории
	taskWg.Add(1)
	taskChan <- scanTask{path: rootPath}

	// Закрываем канал задач, когда все задачи завершены
	go func() {
		taskWg.Wait()
		close(taskChan)
	}()

	// Ожидаем завершения всех рабочих процессов
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Собираем ошибки
	var firstErr error
	for err := range errChan {
		if firstErr == nil && err != nil {
			firstErr = err
		}
		if s.config.Callbacks.OnError != nil {
			s.config.Callbacks.OnError(ErrorInfo{
				Type:  ErrorTypeScan,
				Error: err,
			})
		}
	}

	return firstErr
}

// worker обрабатывает задачи сканирования
func (s *scanner) worker(taskChan chan scanTask, errChan chan error, wg *sync.WaitGroup, taskWg *sync.WaitGroup) {
	defer wg.Done()

	for task := range taskChan {
		if err := s.processPath(task.path, taskChan, taskWg); err != nil {
			errChan <- err
		}
		taskWg.Done()
	}
}

// processPath обрабатывает один путь
func (s *scanner) processPath(path string, taskChan chan scanTask, taskWg *sync.WaitGroup) error {
	info, err := os.Lstat(path) // Используем Lstat для обнаружения символьных ссылок
	if err != nil {
		return err
	}

	// Пропускаем символьные ссылки
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			fullPath := filepath.Join(path, entry.Name())
			taskWg.Add(1)
			select {
			case taskChan <- scanTask{path: fullPath}:
			default:
				// Очередь задач (ёмкость 100) переполнена: рекурсивно обрабатываем синхронно
				// в текущей горутине вместо блокировки на отправке — это предотвращает
				// взаимоблокировку, если все рабочие процессы ожидают отправки,
				// и обеспечивает непрерывную обработку глубоких/широких деревьев
				// без неограниченного роста количества горутин.
				taskWg.Done()
				if err := s.processPath(fullPath, taskChan, taskWg); err != nil {
					return err
				}
			}
		}
	} else if info.Mode().IsRegular() {
		//Обработка обычного файла
		if s.config.isExcluded(path) {
			return nil
		}

		fi := fileInfo{
			path:      path,
			size:      info.Size(),
			blockSize: calculateBlockSize(info.Size(), s.blockSize),
			modTime:   info.ModTime(),
		}
		s.addFile(fi)
	}

	return nil
}

// addFile добавляет файл в соответствующий временной слот.
// Файлы никогда не хранятся в одном огромном слайсе — они сразу группируются
// по времени, округлённому с помощью Truncate(), что ограничивает использование
// памяти в деревьях с миллионами файлов (см. TimeWindow в config.go).
func (s *scanner) addFile(fi fileInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Round time down to the nearest time window
	slotTime := fi.modTime.Truncate(s.config.TimeWindow)

	slot, exists := s.timeSlots[slotTime]
	if !exists {
		slot = &timeSlot{
			time:  slotTime,
			files: make([]fileInfo, 0),
		}
		s.timeSlots[slotTime] = slot
	}

	slot.files = append(slot.files, fi)
	slot.totalSize += fi.size
	slot.totalBlockSize += fi.blockSize
}

// getTimeSlots возвращает временные слоты, отсортированные по времени (сначала старые)
func (s *scanner) getTimeSlots() []*timeSlot {
	s.mu.Lock()
	defer s.mu.Unlock()

	slots := make([]*timeSlot, 0, len(s.timeSlots))
	for _, slot := range s.timeSlots {
		slots = append(slots, slot)
	}

	// Sort by time (oldest first)
	sortTimeSlots(slots)
	return slots
}

// getTotalFiles возвращает общее количество отсканированных файлов
func (s *scanner) getTotalFiles() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := 0
	for _, slot := range s.timeSlots {
		total += len(slot.files)
	}
	return total
}

// sortTimeSlots sorts time slots by time (oldest first)
func sortTimeSlots(slots []*timeSlot) {
	// Простая пузырьковая сортировка для ясности (может быть оптимизирована при необходимости).
	// O(n^2): работает нормально, пока количество слотов невелико (TimeWindow по умолчанию 5 минут
	// на ограниченном интервале хранения), но если это когда-либо будет использоваться с гораздо
	// меньшим TimeWindow или деревом с многолетним хранением, замените на sort.Slice.
	n := len(slots)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if slots[j].time.After(slots[j+1].time) {
				slots[j], slots[j+1] = slots[j+1], slots[j]
			}
		}
	}
}
