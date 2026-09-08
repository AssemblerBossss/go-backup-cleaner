package gobackupcleaner

import "time"

// Callbacks содержит функции обратного вызова для отслеживания процесса очистки
type Callbacks struct {
	OnStart        func(info StartInfo)
	OnScanComplete func(info ScanCompleteInfo)
	OnDeleteStart  func(info DeleteStartInfo)
	OnFileDeleted  func(info FileDeletedInfo)
	OnDirDeleted   func(info DirDeletedInfo)
	OnComplete     func(info CompleteInfo)
	OnError        func(info ErrorInfo)
}

// StartInfo содержит информацию на момент начала очистки
type StartInfo struct {
	TargetDir    string
	CurrentUsage DiskUsage
	TargetSize   int64 // Размер, который нужно удалить, в байтах
}

// ScanCompleteInfo содержит информацию после завершения сканирования файлов
type ScanCompleteInfo struct {
	ScannedFiles  int
	TotalSize     int64
	BlockSize     int64
	TimeThreshold time.Time // Порог удаления
	ScanDuration  time.Duration
}

// DeleteStartInfo содержит информацию на момент начала удаления
type DeleteStartInfo struct {
	EstimatedFiles int
	EstimatedSize  int64
}

// FileDeletedInfo содержит информацию об удалённом файле
type FileDeletedInfo struct {
	Path      string
	Size      int64
	BlockSize int64
	ModTime   time.Time
}

// DirDeletedInfo содержит информацию об удалённой директории
type DirDeletedInfo struct {
	Path string
}

// CompleteInfo содержит информацию на момент завершения очистки
type CompleteInfo struct {
	DeletedFiles     int
	DeletedSize      int64
	DeletedBlockSize int64
	DeletedDirs      int
	DeleteDuration   time.Duration
}

// ErrorInfo содержит информацию об ошибке
type ErrorInfo struct {
	Type  ErrorType
	Path  string
	Error error
}

// ErrorType представляет тип ошибки
type ErrorType string

const (
	ErrorTypeScan   ErrorType = "scan"
	ErrorTypeDelete ErrorType = "delete"
	ErrorTypeDir    ErrorType = "dir"
)

// callSafe безопасно вызывает функцию обратного вызова, если она не nil.
// Обобщён по типу полезной нагрузки info, чтобы все OnXxx-коллбэки в Callbacks
// использовали одну общую проверку на nil вместо повторения
// "if cb != nil { cb(x) }" в каждом месте.
func callSafe[T any](fn func(T), info T) {
	if fn != nil {
		fn(info)
	}
}
