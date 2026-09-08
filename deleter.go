package gobackupcleaner

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// deletedDirs tracks directories that contained deleted files
type deletedDirs struct {
	mu   sync.Mutex
	dirs map[string]struct{}
}

// add adds a directory to the set
func (d *deletedDirs) add(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirs[dir] = struct{}{}
}

// toSlice returns all directories as a slice
func (d *deletedDirs) toSlice() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	dirs := make([]string, 0, len(d.dirs))
	for dir := range d.dirs {
		dirs = append(dirs, dir)
	}
	return dirs
}

// deleter handles file deletion operations
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

// newDeleter creates a new deleter instance
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

// deleteFiles deletes files older than the threshold
func (d *deleter) deleteFiles(rootPath string, threshold time.Time) error {
	taskChan := make(chan scanTask, 100)
	errChan := make(chan error, d.workerCount)
	var wg sync.WaitGroup
	var taskWg sync.WaitGroup

	// Start workers
	for i := 0; i < d.workerCount; i++ {
		wg.Add(1)
		go d.worker(taskChan, errChan, threshold, &wg, &taskWg)
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
		if d.config.Callbacks.OnError != nil {
			d.config.Callbacks.OnError(ErrorInfo{
				Type:  ErrorTypeDelete,
				Error: err,
			})
		}
	}

	return firstErr
}

// worker processes deletion tasks
func (d *deleter) worker(taskChan chan scanTask, errChan chan error, threshold time.Time, wg *sync.WaitGroup, taskWg *sync.WaitGroup) {
	defer wg.Done()

	for task := range taskChan {
		if err := d.processPath(task.path, taskChan, threshold, taskWg); err != nil {
			errChan <- err
		}
		taskWg.Done()
	}
}

// processPath processes a single path for deletion
func (d *deleter) processPath(path string, taskChan chan scanTask, threshold time.Time, taskWg *sync.WaitGroup) error {
	info, err := os.Lstat(path) // Use Lstat to detect symlinks
	if err != nil {
		if os.IsNotExist(err) {
			// File already deleted, not an error
			return nil
		}
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
				// If channel is full, process synchronously
				taskWg.Done()
				if err := d.processPath(fullPath, taskChan, threshold, taskWg); err != nil {
					return err
				}
			}
		}
	} else if info.Mode().IsRegular() && info.ModTime().Before(threshold) {
		// Delete file if it's older than threshold.
		// This is a fresh Lstat-based walk, independent of the scanner's
		// timeSlot map - the deleter never holds the file list from phase 1
		// in memory, it just re-visits the tree and re-checks each file's
		// own ModTime against the single threshold computed in cleaner.go.
		size := info.Size()
		blockSize := calculateBlockSize(size, d.blockSize)

		if err := os.Remove(path); err != nil {
			return err
		}

		// Track deleted file
		d.mu.Lock()
		d.deletedFiles++
		d.deletedSize += size
		d.deletedBlocks += blockSize
		d.mu.Unlock()

		// Track parent directory
		d.deletedDirs.add(filepath.Dir(path))

		// Call callback
		callSafe(d.config.Callbacks.OnFileDeleted, FileDeletedInfo{
			Path:      path,
			Size:      size,
			BlockSize: blockSize,
			ModTime:   info.ModTime(),
		})
	}

	return nil
}

// deleteEmptyDirs deletes empty directories
func (d *deleter) deleteEmptyDirs() (int, error) {
	if !d.config.RemoveEmptyDirs {
		return 0, nil
	}

	deletedCount := 0
	dirs := d.deletedDirs.toSlice()

	// Process directories in reverse order (deepest first)
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

// deleteEmptyDirRecursive recursively deletes empty directories.
// Starts only from directories that actually had a file deleted from them
// (see deletedDirs), then walks upward: removing a dir can make its parent
// empty too, so each successful removal retries one level up until it hits
// a non-empty (or root/"." ) directory.
func (d *deleter) deleteEmptyDirRecursive(dir string, deletedCount *int) error {
	// Check if directory is empty
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory already deleted
			return nil
		}
		return err
	}

	if len(entries) == 0 {
		// Directory is empty, delete it
		if err := os.Remove(dir); err != nil {
			return err
		}

		(*deletedCount)++

		// Call callback
		callSafe(d.config.Callbacks.OnDirDeleted, DirDeletedInfo{
			Path: dir,
		})

		// Try to delete parent directory
		parent := filepath.Dir(dir)
		if parent != dir && parent != "." && parent != "/" {
			return d.deleteEmptyDirRecursive(parent, deletedCount)
		}
	}

	return nil
}

// getStats returns deletion statistics
func (d *deleter) getStats() (files int, size int64, blocks int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deletedFiles, d.deletedSize, d.deletedBlocks
}
