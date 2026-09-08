# go-backup-cleaner

English | [日本語](README.ja.md)

[![Test](https://github.com/ideamans/go-backup-cleaner/actions/workflows/test.yml/badge.svg)](https://github.com/ideamans/go-backup-cleaner/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ideamans/go-backup-cleaner.svg)](https://pkg.go.dev/github.com/ideamans/go-backup-cleaner)

A Go package designed to efficiently maintain disk free space by removing old backup files from large file collections. It ensures a specified amount of free disk space is always available by intelligently deleting the oldest backup files when storage capacity limits are reached.

## Features

- **Efficient disk space management** - Maintains specified free space by removing old backups
- **Optimized for large file collections** - Handles millions of files with memory-efficient algorithms
- **Parallel processing** - Concurrent file scanning and deletion for maximum performance
- **Smart deletion** - Deletes oldest files first to preserve recent backups
- **Block-size aware** - Accurately calculates actual disk space that will be freed
- **Flexible constraints** - MinFreeSpace (recommended), MaxUsagePercent, or MaxSize
- **Progress monitoring** - Real-time callbacks for tracking operations
- **Cross-platform** - Works on Linux, macOS, and Windows

## Installation

```bash
go get github.com/ideamans/go-backup-cleaner
```

## Usage

```go
package main

import (
    "log"
    cleaner "github.com/ideamans/go-backup-cleaner"
)

func main() {
    // Set minimum free space requirement (recommended approach)
    minFree := int64(10 * 1024 * 1024 * 1024) // 10GB
    config := cleaner.CleaningConfig{
        MinFreeSpace:    &minFree,
        RemoveEmptyDirs: true,
        Callbacks: cleaner.Callbacks{
            OnFileDeleted: func(info cleaner.FileDeletedInfo) {
                log.Printf("Deleted: %s (%d bytes)", info.Path, info.Size)
            },
        },
    }

    report, err := cleaner.CleanBackup("/path/to/backup", config)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Deleted %d files, freed %d bytes in %v",
        report.DeletedFiles, report.DeletedSize, report.TotalDuration)
}
```

## Configuration Options

### Capacity Constraints (at least one required)

- `MinFreeSpace`: Minimum free space in bytes (recommended primary option)
- `MaxUsagePercent`: Maximum disk usage percentage (0-100)
- `MaxSize`: Maximum total size in bytes (alternative when disk info is unavailable)

### Optional Settings

- `TimeWindow`: Time interval for file aggregation (default: 5 minutes)
- `RemoveEmptyDirs`: Whether to remove empty directories (default: true)
- `Concurrency`: Level of concurrency (default: runtime.NumCPU())
- `MaxConcurrency`: Maximum level of concurrency (default: 4)

#### Concurrency Settings

The package uses parallel processing for scanning and deleting files. You can control the level of parallelism:

- `Concurrency`: Specifies the desired level of concurrency. If set to 0, it defaults to the number of CPU cores.
- `MaxConcurrency`: Limits the maximum level of concurrency. Defaults to 4.
- The actual concurrency can be obtained via `config.ActualWorkerCount()`, which returns `min(Concurrency, MaxConcurrency)`.

The reason for limiting `MaxConcurrency` to 4:

- Benchmarks show diminishing returns beyond 4 parallel workers
- Disk I/O becomes the bottleneck, making excessive parallelization ineffective
- This value provides optimal resource utilization for most systems

#### Block Size

The cleaner considers "block size" when calculating disk space. Block size refers to the minimum allocation unit used by the file system. When a file is stored on disk, it occupies space in multiples of the block size, even if the actual file size is smaller. For example:

- A 1KB file on a file system with 4KB blocks will actually use 4KB of disk space
- A 5KB file on the same system will use 8KB (2 blocks)

This package accurately tracks both the file size and the actual disk space that will be freed when files are deleted, ensuring precise capacity management.

### Callbacks

Monitor the cleaning process with callbacks:

- `OnStart`: Called when cleaning starts
- `OnScanComplete`: Called after file scanning completes
- `OnDeleteStart`: Called before deletion begins
- `OnFileDeleted`: Called for each deleted file
- `OnDirDeleted`: Called for each deleted directory
- `OnComplete`: Called when cleaning completes
- `OnError`: Called on non-fatal errors

## How It Works

1. **Scans** the backup directory to catalog all files
2. **Calculates** how much space needs to be freed based on constraints
3. **Determines** a time threshold - files older than this will be deleted
4. **Deletes** files in parallel, starting with the oldest
5. **Cleans up** empty directories (if enabled)

### Checking Disk Space Before Cleanup

The package provides a convenience function `GetDiskFreeSpace` to quickly check available disk space before performing cleanup operations:

```go
// Check if cleanup is needed before running the full operation
freeSpace, err := cleaner.GetDiskFreeSpace("/path/to/backup")
if err != nil {
    log.Fatal(err)
}

// Only run cleanup if free space is below threshold
if freeSpace < requiredFreeSpace {
    report, err := cleaner.CleanBackup("/path/to/backup", config)
    // ...
}
```

This allows for efficient pre-checks to avoid unnecessary file scanning when disk space is already sufficient.

### Choosing the Right Capacity Constraint

**MinFreeSpace (Recommended)**: This is the most straightforward and recommended option for most use cases. It ensures a specific amount of free disk space is always available, which is typically what backup systems need to guarantee successful operation.

**MaxUsagePercent**: Useful when you want to maintain a percentage-based disk usage policy across different sized volumes.

**MaxSize**: Best used as a fallback option when disk usage information is not available (e.g., due to permissions or OS limitations). In this mode, the cleaner will delete old files until the total size is under the specified limit. This is useful for:

- Environments with restricted disk access
- Network storage where disk usage APIs are not available
- Simplified quota-based cleanup

Note: `MaxUsagePercent` and `MinFreeSpace` require disk usage information and cannot be used when disk usage is unavailable.

## Testing

Run tests:

```bash
go test -v ./...
```

Run tests with coverage:

```bash
go test -v -cover ./...
```

## CLI

Собрать инструмент:

```bash
go build -o backup-cleaner ./cmd/backup-cleaner
```

Запуск по YAML-конфигу (см. config.example.yaml):

```bash
./backup-cleaner -config config.yaml -verbose
```

Принудительный dry-run для всех targets сразу, независимо от того,
что указано в самом YAML:

```bash
./backup-cleaner -config config.yaml -dry-run -verbose
```


## License

MIT License - see LICENSE file for details.