package gobackupcleaner

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileInfo represents information about a file
type fileInfo struct {
	path      string
	size      int64
	blockSize int64
	modTime   time.Time
}

// timeSlot represents files grouped by time interval
type timeSlot struct {
	time           time.Time
	files          []fileInfo
	totalSize      int64
	totalBlockSize int64
}

// scanTask represents a task for parallel scanning
type scanTask struct {
	path string
}

// scanner handles file scanning operations
type scanner struct {
	config      *CleaningConfig
	blockSize   int64
	workerCount int
	mu          sync.Mutex
	timeSlots   map[time.Time]*timeSlot
}

// newScanner creates a new scanner instance
func newScanner(config *CleaningConfig, blockSize int64) *scanner {
	return &scanner{
		config:      config,
		blockSize:   blockSize,
		workerCount: config.ActualWorkerCount(),
		timeSlots:   make(map[time.Time]*timeSlot),
	}
}

// scan performs parallel file scanning
func (s *scanner) scan(rootPath string) error {
	taskChan := make(chan scanTask, 100)
	errChan := make(chan error, s.workerCount)
	var wg sync.WaitGroup
	var taskWg sync.WaitGroup

	// Start workers
	for i := 0; i < s.workerCount; i++ {
		wg.Add(1)
		go s.worker(taskChan, errChan, &wg, &taskWg)
	}

	// Start with root directory
	taskWg.Add(1)
	taskChan <- scanTask{path: rootPath}

	// Close task channel when all tasks are done
	go func() {
		taskWg.Wait()
		close(taskChan)
	}()

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Collect errors
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

// worker processes scan tasks
func (s *scanner) worker(taskChan chan scanTask, errChan chan error, wg *sync.WaitGroup, taskWg *sync.WaitGroup) {
	defer wg.Done()

	for task := range taskChan {
		if err := s.processPath(task.path, taskChan, taskWg); err != nil {
			errChan <- err
		}
		taskWg.Done()
	}
}

// processPath processes a single path
func (s *scanner) processPath(path string, taskChan chan scanTask, taskWg *sync.WaitGroup) error {
	info, err := os.Lstat(path) // Use Lstat to detect symlinks
	if err != nil {
		return err
	}

	// Skip symlinks
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
				// Task queue (cap 100) is saturated: recurse synchronously on this
				// worker's own goroutine instead of blocking on send - avoids a
				// deadlock if every worker ends up waiting to enqueue at once,
				// and keeps deep/wide trees flowing without unbounded goroutines.
				taskWg.Done()
				if err := s.processPath(fullPath, taskChan, taskWg); err != nil {
					return err
				}
			}
		}
	} else if info.Mode().IsRegular() {
		// Process regular file
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

// addFile adds a file to the appropriate time slot.
// Files are never held in one giant slice - they're bucketed by
// Truncate()-rounded mtime immediately, which is what keeps memory use
// bounded on trees with millions of files (see TimeWindow in config.go).
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

// getTimeSlots returns time slots sorted by time (oldest first)
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

// getTotalFiles returns the total number of scanned files
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
	// Simple bubble sort for clarity (can be optimized if needed).
	// O(n^2): fine while slot count stays small (TimeWindow default 5m over
	// a bounded retention span), but if this ever gets driven by a much
	// finer TimeWindow or a multi-year backup tree, swap for sort.Slice.
	n := len(slots)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if slots[j].time.After(slots[j+1].time) {
				slots[j], slots[j+1] = slots[j+1], slots[j]
			}
		}
	}
}
