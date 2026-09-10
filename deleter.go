package gobackupcleaner

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// deletedDirs отслеживает директории, из которых были удалены файлы
type deletedDirs struct {
	mu   sync.Mutex
	dirs map[string]struct{}
}

// add добавляет директорию в набор
func (d *deletedDirs) add(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirs[dir] = struct{}{}
}

// toSlice возвращает все директории в виде среза
func (d *deletedDirs) toSlice() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	dirs := make([]string, 0, len(d.dirs))
	for dir := range d.dirs {
		dirs = append(dirs, dir)
	}
	return dirs
}

// deleter управляет операциями удаления файлов
type deleter struct {
	config        *CleaningConfig
	blockSize     int64
	workerCount   int
	deletedDirs   *deletedDirs
	mu            sync.Mutex
	deletedFiles  int
	deletedSize   int64
	deletedBlocks int64
}

// newDeleter создаёт новый экземпляр deleter
func newDeleter(config *CleaningConfig, blockSize int64) *deleter {
	return &deleter{
		config:      config,
		blockSize:   blockSize,
		workerCount: config.ActualWorkerCount(),
		deletedDirs: &deletedDirs{
			dirs: make(map[string]struct{}),
		},
	}
}

// deleteFiles удаляет файлы старше threshold
func (d *deleter) deleteFiles(rootPath string, threshold time.Time) error {
	taskChan := make(chan scanTask, 100)
	errChan := make(chan error, d.workerCount)
	var wg sync.WaitGroup
	var taskWg sync.WaitGroup

	// Запускаем рабочие процессы
	for i := 0; i < d.workerCount; i++ {
		wg.Add(1)
		go d.worker(taskChan, errChan, threshold, &wg, &taskWg)
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
		if d.config.Callbacks.OnError != nil {
			d.config.Callbacks.OnError(ErrorInfo{
				Type:  ErrorTypeDelete,
				Error: err,
			})
		}
	}

	return firstErr
}

// worker обрабатывает задачи удаления
func (d *deleter) worker(taskChan chan scanTask, errChan chan error, threshold time.Time, wg *sync.WaitGroup, taskWg *sync.WaitGroup) {
	defer wg.Done()

	for task := range taskChan {
		if err := d.processPath(task.path, taskChan, threshold, taskWg); err != nil {
			errChan <- err
		}
		taskWg.Done()
	}
}

// processPath обрабатывает один путь для удаления
func (d *deleter) processPath(path string, taskChan chan scanTask, threshold time.Time, taskWg *sync.WaitGroup) error {
	info, err := os.Lstat(path) // Используем Lstat для обнаружения символьных ссылок
	if err != nil {
		if os.IsNotExist(err) {
			// Файл уже удалён, это не ошибка
			return nil
		}
		return err
	}

	// Пропускаем символьные ссылки
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	if info.IsDir() {
		if d.config.IsExcludedDir(info.Name()) {
			return nil
		}
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
				// Очередь задач (ёмкость 100) переполнена: рекурсивно обрабатываем
				// синхронно в текущей горутине вместо блокировки на отправке — это
				// предотвращает взаимоблокировку, если все рабочие процессы ожидают
				// отправки, и обеспечивает непрерывную обработку глубоких/широких
				// деревьев без неограниченного роста количества горутин.
				taskWg.Done()
				if err := d.processPath(fullPath, taskChan, threshold, taskWg); err != nil {
					return err
				}
			}
		}
	} else if info.Mode().IsRegular() &&
		info.ModTime().Before(threshold) &&
		!d.config.isExcluded(path) {
		size := info.Size()
		blockSize := calculateBlockSize(size, d.blockSize)

		if !d.config.DryRun {
			if err := os.Remove(path); err != nil {
				return err
			}
		}

		// Учитываем удалённый файл
		d.mu.Lock()
		d.deletedFiles++
		d.deletedSize += size
		d.deletedBlocks += blockSize
		d.mu.Unlock()

		// Запоминаем родительскую директорию
		d.deletedDirs.add(filepath.Dir(path))

		// Вызываем коллбэк
		callSafe(d.config.Callbacks.OnFileDeleted, FileDeletedInfo{
			Path:      path,
			Size:      size,
			BlockSize: blockSize,
			ModTime:   info.ModTime(),
		})
	}

	return nil
}

// deleteEmptyDirs удаляет пустые директории
func (d *deleter) deleteEmptyDirs(dirPath string) (int, error) {
	if !d.config.RemoveEmptyDirs {
		return 0, nil
	}

	var dirs []string

	err := filepath.WalkDir(dirPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path == dirPath {
			//Корень не удаляем
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})
	if err != nil {
		return 0, err
	}

	deletedCount := 0
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		if err := d.deleteEmptyDirRecursive(dir, &deletedCount); err != nil {
			if d.config.Callbacks.OnError != nil {
				d.config.Callbacks.OnError(ErrorInfo{
					Type:  ErrorTypeDir,
					Path:  dir,
					Error: err,
				})
			}
		}
	}

	return deletedCount, nil
}

// deleteEmptyDirRecursive рекурсивно удаляет пустые директории.
// Стартует только с директорий, из которых реально был удалён файл
// (см. deletedDirs), затем идёт вверх по дереву: удаление директории может
// сделать пустой и её родителя, поэтому после каждого успешного удаления
// делается попытка удалить директорию уровнем выше, пока не встретится
// непустая директория (или корень/".").
func (d *deleter) deleteEmptyDirRecursive(dir string, deletedCount *int) error {
	// Проверяем, пуста ли директория
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Директория уже удалена
			return nil
		}
		return err
	}

	if len(entries) == 0 {
		// Директория пуста, удаляем её

		if !d.config.DryRun {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}

		(*deletedCount)++

		// Вызываем коллбэк
		callSafe(d.config.Callbacks.OnDirDeleted, DirDeletedInfo{
			Path: dir,
		})

		// Пробуем удалить родительскую директорию
		parent := filepath.Dir(dir)
		if parent != dir && parent != "." && parent != "/" {
			return d.deleteEmptyDirRecursive(parent, deletedCount)
		}
	}

	return nil
}

// getStats возвращает статистику удаления
func (d *deleter) getStats() (files int, size int64, blocks int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deletedFiles, d.deletedSize, d.deletedBlocks
}
