package gobackupcleaner

import (
	"os"
	"time"
)

// CleanBackup cleans backup files based on the specified configuration
func CleanBackup(dirPath string, config CleaningConfig) (CleaningReport, error) {
	startTime := time.Now()

	// Set defaults and validate configuration
	config.setDefaults()
	if err := config.validate(); err != nil {
		return CleaningReport{}, err
	}

	// Check if directory exists
	if _, err := os.Stat(dirPath); err != nil {
		if os.IsNotExist(err) {
			return CleaningReport{}, ErrDirectoryNotFound
		}
		return CleaningReport{}, err
	}

	// Get current disk usage
	currentUsage, err := config.DiskInfo.GetDiskUsage(dirPath)
	var diskUsageError error
	if err != nil {
		// Save the error for later
		diskUsageError = err
		// Check if we can proceed without disk usage
		if config.MaxSize == nil {
			// Can't proceed without disk usage when only MaxUsagePercent or MinFreeSpace is specified
			return CleaningReport{}, err
		}
	}

	// Calculate target deletion size
	var targetSize int64
	if diskUsageError != nil && config.MaxSize != nil {
		// Special case: can't get disk usage but MaxSize is specified
		// In this case, we'll scan all files and delete until total size is under MaxSize
		// This allows the cleaner to work in environments where disk usage APIs are not available
		// (e.g., restricted permissions, network storage, etc.)
		targetSize = -1 // Special value to indicate "scan and delete until under MaxSize"
	} else {
		targetSize = calculateTargetSize(currentUsage, &config)
		if targetSize <= 0 {
			// No need to delete anything
			return CleaningReport{
				TotalDuration: time.Since(startTime),
			}, nil
		}
	}

	// Get block size
	blockSize, err := config.DiskInfo.GetBlockSize(dirPath)
	if err != nil {
		return CleaningReport{}, err
	}

	// Call OnStart callback
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

	// Phase 1: Scan files
	scanStartTime := time.Now()
	scanner := newScanner(&config, blockSize)
	if err := scanner.scan(dirPath); err != nil {
		return CleaningReport{}, err
	}

	// Get sorted time slots
	timeSlots := scanner.getTimeSlots()
	if len(timeSlots) == 0 {
		// No files found
		return CleaningReport{
			ScanDuration:  time.Since(scanStartTime),
			TotalDuration: time.Since(startTime),
		}, nil
	}

	// Calculate deletion threshold
	var threshold time.Time
	var estimatedFiles int
	var estimatedSize int64

	if targetSize == -1 && config.MaxSize != nil {
		// Special case: delete until total size is under MaxSize
		threshold, estimatedFiles, estimatedSize = calculateThresholdForMaxSize(timeSlots, *config.MaxSize)
	} else {
		threshold, estimatedFiles, estimatedSize = calculateThreshold(timeSlots, targetSize)
	}
	scanDuration := time.Since(scanStartTime)

	// Call OnScanComplete callback
	callSafe(config.Callbacks.OnScanComplete, ScanCompleteInfo{
		ScannedFiles:  scanner.getTotalFiles(),
		TotalSize:     getTotalSize(timeSlots),
		BlockSize:     blockSize,
		TimeThreshold: threshold,
		ScanDuration:  scanDuration,
	})

	// Phase 2: Delete files
	deleteStartTime := time.Now()

	// Call OnDeleteStart callback
	callSafe(config.Callbacks.OnDeleteStart, DeleteStartInfo{
		EstimatedFiles: estimatedFiles,
		EstimatedSize:  estimatedSize,
	})

	deleter := newDeleter(&config, blockSize)
	if err := deleter.deleteFiles(dirPath, threshold); err != nil {
		return CleaningReport{}, err
	}

	// Phase 3: Delete empty directories
	deletedDirs, _ := deleter.deleteEmptyDirs()
	// Ignore error as it's non-fatal for directory deletion

	deleteDuration := time.Since(deleteStartTime)
	deletedFiles, deletedSize, deletedBlocks := deleter.getStats()

	// Call OnComplete callback
	callSafe(config.Callbacks.OnComplete, CompleteInfo{
		DeletedFiles:     deletedFiles,
		DeletedSize:      deletedSize,
		DeletedBlockSize: deletedBlocks,
		DeletedDirs:      deletedDirs,
		DeleteDuration:   deleteDuration,
	})

	// Create report
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

// calculateTargetSize calculates how much space needs to be freed.
// Each constraint (MaxSize / MaxUsagePercent / MinFreeSpace) is evaluated
// independently, and the LARGEST resulting size wins (not the sum): since
// Used/Free/UsedPercent all move together as files are deleted, satisfying
// the strictest constraint automatically satisfies the looser ones too.
func calculateTargetSize(usage *DiskUsage, config *CleaningConfig) int64 {
	var targetSize int64

	// Check MaxSize
	if config.MaxSize != nil {
		currentSize := int64(usage.Used)
		if currentSize > *config.MaxSize {
			size := currentSize - *config.MaxSize
			if size > targetSize {
				targetSize = size
			}
		}
	}

	// Check MaxUsagePercent
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

	// Check MinFreeSpace
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

// calculateThreshold calculates the time threshold for deletion.
// Walks time slots oldest-first, accumulating size until targetSize is
// reached, then sets threshold just past that slot. The deleter later
// removes every file with ModTime strictly before threshold, so files in
// the exact boundary slot are deleted too (that's why we add +1s here
// rather than using the slot's own time).
func calculateThreshold(slots []*timeSlot, targetSize int64) (time.Time, int, int64) {
	var accumulatedSize int64
	var accumulatedFiles int
	var threshold time.Time

	// If no slots, return zero time
	if len(slots) == 0 {
		return time.Time{}, 0, 0
	}

	// Set initial threshold to the latest time + 1 second
	// (so nothing gets deleted by default)
	threshold = slots[len(slots)-1].time.Add(time.Second)

	for _, slot := range slots {
		accumulatedSize += slot.totalBlockSize
		accumulatedFiles += len(slot.files)

		if accumulatedSize >= targetSize {
			// We've reached the target size
			// Include all files up to and including this slot
			threshold = slot.time.Add(time.Second)
			break
		}
	}

	return threshold, accumulatedFiles, accumulatedSize
}

// getTotalSize calculates the total size from time slots
func getTotalSize(slots []*timeSlot) int64 {
	var total int64
	for _, slot := range slots {
		total += slot.totalSize
	}
	return total
}

// calculateThresholdForMaxSize calculates the time threshold when total size must be under maxSize
func calculateThresholdForMaxSize(slots []*timeSlot, maxSize int64) (time.Time, int, int64) {
	var totalSize int64
	var remainingSize int64
	var deleteFiles int
	var deleteSize int64

	// Calculate total size
	for _, slot := range slots {
		totalSize += slot.totalBlockSize
	}

	// If already under maxSize, no need to delete
	if totalSize <= maxSize {
		return time.Time{}, 0, 0
	}

	// Start from the newest files and work backwards
	// We want to keep as much as possible under maxSize
	remainingSize = totalSize

	// Find the cutoff point - delete old files until we're under maxSize
	for i := 0; i < len(slots); i++ {
		slot := slots[i]

		// Delete this entire slot
		remainingSize -= slot.totalBlockSize
		deleteFiles += len(slot.files)
		deleteSize += slot.totalBlockSize

		// Check if we've deleted enough
		if remainingSize <= maxSize {
			// We've reached our target - set threshold to include this slot.
			// NOTE: unlike calculateThreshold above (which adds +1s), this
			// path adds +1h. Both just need to land after the slot's
			// Truncate()-rounded time and before the next slot, but the
			// inconsistency is accidental, not intentional - keep in mind
			// if TimeWindow is ever configured larger than 1h.
			return slot.time.Add(time.Hour), deleteFiles, deleteSize
		}
	}

	// If we get here, we need to delete everything (shouldn't happen normally)
	if len(slots) > 0 {
		return time.Now().Add(time.Hour), deleteFiles, deleteSize
	}
	return time.Time{}, 0, 0
}
