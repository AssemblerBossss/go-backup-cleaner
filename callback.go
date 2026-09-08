package gobackupcleaner

import "time"

// Callbacks contains callback functions for monitoring the cleaning process
type Callbacks struct {
	OnStart        func(info StartInfo)
	OnScanComplete func(info ScanCompleteInfo)
	OnDeleteStart  func(info DeleteStartInfo)
	OnFileDeleted  func(info FileDeletedInfo)
	OnDirDeleted   func(info DirDeletedInfo)
	OnComplete     func(info CompleteInfo)
	OnError        func(info ErrorInfo)
}

// StartInfo contains information at the start of cleaning
type StartInfo struct {
	TargetDir    string
	CurrentUsage DiskUsage
	TargetSize   int64 // Size to be deleted in bytes
}

// ScanCompleteInfo contains information after file scanning is complete
type ScanCompleteInfo struct {
	ScannedFiles  int
	TotalSize     int64
	BlockSize     int64
	TimeThreshold time.Time // Deletion threshold
	ScanDuration  time.Duration
}

// DeleteStartInfo contains information at the start of deletion
type DeleteStartInfo struct {
	EstimatedFiles int
	EstimatedSize  int64
}

// FileDeletedInfo contains information about a deleted file
type FileDeletedInfo struct {
	Path      string
	Size      int64
	BlockSize int64
	ModTime   time.Time
}

// DirDeletedInfo contains information about a deleted directory
type DirDeletedInfo struct {
	Path string
}

// CompleteInfo contains information at the completion of cleaning
type CompleteInfo struct {
	DeletedFiles     int
	DeletedSize      int64
	DeletedBlockSize int64
	DeletedDirs      int
	DeleteDuration   time.Duration
}

// ErrorInfo contains error information
type ErrorInfo struct {
	Type  ErrorType
	Path  string
	Error error
}

// ErrorType represents the type of error
type ErrorType string

const (
	ErrorTypeScan   ErrorType = "scan"
	ErrorTypeDelete ErrorType = "delete"
	ErrorTypeDir    ErrorType = "dir"
)

// callSafe safely calls a callback function if it's not nil.
// Generic over the info payload so every OnXxx callback in Callbacks can
// share one nil-check instead of repeating "if cb != nil { cb(x) }" everywhere.
func callSafe[T any](fn func(T), info T) {
	if fn != nil {
		fn(info)
	}
}
