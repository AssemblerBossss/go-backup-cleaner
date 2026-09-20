package cleaner

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type ageDiskInfoProvider struct {
	blockSizeErr error

	mu         sync.Mutex
	usageCalls int
}

func (p *ageDiskInfoProvider) GetDiskUsage(_ string) (*DiskUsage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usageCalls++
	return nil, errors.New("age mode must not query disk usage")
}

func (p *ageDiskInfoProvider) GetBlockSize(_ string) (int64, error) {
	if p.blockSizeErr != nil {
		return 0, p.blockSizeErr
	}
	return 4096, nil
}

func (p *ageDiskInfoProvider) getUsageCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usageCalls
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// mustCreateFile создаёт файл с заданным размером и возрастом (относительно now)
func mustCreateFile(t *testing.T, path string, size int64, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := createTestFile(t, path, size, time.Now().Add(-age)); err != nil {
		t.Fatal(err)
	}
}

// ageConfig — минимальная валидная конфигурация age-режима
func ageConfig(maxAge time.Duration) (CleaningConfig, *ageDiskInfoProvider) {
	disk := &ageDiskInfoProvider{}
	return CleaningConfig{
		MaxAge:          durationPtr(maxAge),
		RemoveEmptyDirs: true,
		Concurrency:     2,
		DiskInfo:        disk,
	}, disk
}

// createOldFiles создаёт n файлов по 1024 байта, каждый старше любого разумного MaxAge
func createOldFiles(t *testing.T, dir string, n int) []string {
	t.Helper()
	var paths []string
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".dat")
		mustCreateFile(t, p, 1024, 48*time.Hour)
		paths = append(paths, p)
	}
	return paths
}

func TestCleanByAge_DeletesOnlyFilesOlderThanMaxAge(t *testing.T) {
	dir := t.TempDir()

	mustCreateFile(t, filepath.Join(dir, "old1.txt"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "old2.txt"), 2048, 30*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "edge_old.txt"), 100, 25*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "edge_young.txt"), 100, 23*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "fresh.txt"), 512, time.Hour)

	config, disk := ageConfig(24 * time.Hour)

	before := time.Now()
	report, err := CleanBackup(dir, config)
	after := time.Now()

	if err != nil {
		t.Fatal(err)
	}

	if report.DeletedFiles != 3 {
		t.Errorf("DeletedFiles = %d, want 3", report.DeletedFiles)
	}
	if report.DeletedSize != 1024+2048+100 {
		t.Errorf("DeletedSize = %d, want %d", report.DeletedSize, 1024+2048+100)
	}
	if report.DeletedBlockSize != 4096+4096+4096 {
		t.Errorf("DeletedBlockSize = %d, want %d", report.DeletedBlockSize, 3*4096)
	}
	if report.BlockSize != 4096 {
		t.Errorf("BlockSize = %d, want 4096", report.BlockSize)
	}

	for _, name := range []string{"old1.txt", "old2.txt", "edge_old.txt"} {
		if pathExists(filepath.Join(dir, name)) {
			t.Errorf("%s должен быть удалён", name)
		}
	}

	for _, name := range []string{"edge_young.txt", "fresh.txt"} {
		if !pathExists(filepath.Join(dir, name)) {
			t.Errorf("%s не должен удаляться", name)
		}
	}

	// TimeThreshold = now - MaxAge
	lo, hi := before.Add(-24*time.Hour), after.Add(-24*time.Hour)
	if report.TimeThreshold.Before(lo) || report.TimeThreshold.After(hi) {
		t.Errorf("TimeThreshold = %v, want в диапазоне [%v, %v]", report.TimeThreshold, lo, hi)
	}

	// без триггеров сканирование не выполняется
	if report.ScannedFiles != 0 {
		t.Errorf("ScannedFiles = %d, want 0 (без триггеров скан не нужен)", report.ScannedFiles)
	}
	if n := disk.getUsageCalls(); n != 0 {
		t.Errorf("GetDiskUsage вызван %d раз, want 0", n)
	}
}

func TestCleanByAge_DryRunDoesNotRemoveFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	mustCreateFile(t, old, 1024, 48*time.Hour)

	config, _ := ageConfig(24 * time.Hour)
	config.DryRun = true

	report, err := CleanBackup(dir, config)
	if err != nil {
		t.Fatal(err)
	}

	if report.DeletedFiles != 1 || report.DeletedSize != 1024 {
		t.Errorf("отчёт DryRun: DeletedFiles=%d DeletedSize=%d, want 1 / 1024", report.DeletedFiles, report.DeletedSize)
	}
	if !pathExists(old) {
		t.Error("в DryRun файл не должен удаляться с диска")
	}
}

func TestCleanByAge_ZeroMaxAgeDeletesEverythingExistingFiles(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "a.txt"), 10, time.Minute)
	mustCreateFile(t, filepath.Join(dir, "b.txt"), 10, time.Second)

	config, _ := ageConfig(0)

	report, err := CleanBackup(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedFiles != 2 {
		t.Errorf("DeletedFiles = %d, want 2", report.DeletedFiles)
	}
}

func TestCleanByAge_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	config, _ := ageConfig(time.Hour)

	report, err := CleanBackup(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedFiles != 0 || report.DeletedDirs != 0 {
		t.Errorf("ожидали пустой результат, got %+v", report)
	}
	if !pathExists(dir) {
		t.Error("корневая директория не должна удаляться")
	}
}

func TestCleanByAge_DirectoryNotFound(t *testing.T) {
	config, _ := ageConfig(time.Hour)

	_, err := CleanBackup(filepath.Join(t.TempDir(), "missing"), config)
	if !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("err = %v, want ErrDirectoryNotFound", err)
	}
}

func TestCleanByAge_BlockSizeErrorIsReturned(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	mustCreateFile(t, old, 1024, 48*time.Hour)

	wantErr := errors.New("block size unavailable")
	config, disk := ageConfig(24 * time.Hour)
	disk.blockSizeErr = wantErr

	_, err := CleanBackup(dir, config)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if !pathExists(old) {
		t.Error("при ошибке GetBlockSize ничего не должно удаляться")
	}
}

func TestCleanByAge_Callbacks(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "old1.txt"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "old2.txt"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "fresh.txt"), 1024, time.Minute)

	var (
		mu           sync.Mutex
		startInfos   []StartInfo
		scanCalls    int
		deleteStarts int
		fileDeleted  int
		completeInfo []CompleteInfo
	)

	config, _ := ageConfig(24 * time.Hour)
	config.Callbacks = Callbacks{
		OnStart: func(info StartInfo) {
			mu.Lock()
			defer mu.Unlock()
			startInfos = append(startInfos, info)
		},
		OnScanComplete: func(ScanCompleteInfo) {
			mu.Lock()
			defer mu.Unlock()
			scanCalls++
		},
		OnDeleteStart: func(DeleteStartInfo) {
			mu.Lock()
			defer mu.Unlock()
			deleteStarts++
		},
		OnFileDeleted: func(FileDeletedInfo) {
			mu.Lock()
			defer mu.Unlock()
			fileDeleted++
		},
		OnComplete: func(info CompleteInfo) {
			mu.Lock()
			defer mu.Unlock()
			completeInfo = append(completeInfo, info)
		},
	}

	if _, err := CleanBackup(dir, config); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(startInfos) != 1 {
		t.Fatalf("OnStart вызван %d раз, want 1", len(startInfos))
	}
	if startInfos[0].TargetDir != dir {
		t.Errorf("StartInfo.TargetDir = %q, want %q", startInfos[0].TargetDir, dir)
	}
	if startInfos[0].CurrentUsage != (DiskUsage{}) {
		t.Errorf("StartInfo.CurrentUsage должен быть пустым в age-режиме, got %+v", startInfos[0].CurrentUsage)
	}
	if scanCalls != 0 {
		t.Errorf("OnScanComplete вызван %d раз без триггеров, want 0", scanCalls)
	}
	if deleteStarts != 0 {
		t.Errorf("OnDeleteStart вызван %d раз, want 0 (в age-режиме не вызывается)", deleteStarts)
	}
	if fileDeleted != 2 {
		t.Errorf("OnFileDeleted вызван %d раз, want 2", fileDeleted)
	}
	if len(completeInfo) != 1 || completeInfo[0].DeletedFiles != 2 {
		t.Errorf("OnComplete = %+v, want один вызов с DeletedFiles=2", completeInfo)
	}
}

func TestCleanByAge_MinTriggerSize(t *testing.T) {
	// 3 файла по 1024 байта → размер папки 3072 (по реальному размеру, не по блокам)
	tests := []struct {
		name        string
		trigger     int64
		wantDeleted int
	}{
		{"порог ровно равен размеру — срабатывает (>=)", 3072, 3},
		{"порог на 1 байт больше — не срабатывает", 3073, 0},
		{"нулевой порог — срабатывает всегда", 0, 3},
		{"огромный порог — не срабатывает", 1 << 40, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := createOldFiles(t, dir, 3)

			config, _ := ageConfig(24 * time.Hour)
			config.MinTriggerSize = int64Ptr(tt.trigger)

			var scanInfo *ScanCompleteInfo
			config.Callbacks.OnScanComplete = func(info ScanCompleteInfo) { scanInfo = &info }

			report, err := CleanBackup(dir, config)
			if err != nil {
				t.Fatal(err)
			}

			if report.DeletedFiles != tt.wantDeleted {
				t.Errorf("DeletedFiles = %d, want %d", report.DeletedFiles, tt.wantDeleted)
			}
			if report.ScannedFiles != 3 {
				t.Errorf("ScannedFiles = %d, want 3", report.ScannedFiles)
			}
			if report.BlockSize != 4096 {
				t.Errorf("BlockSize = %d, want 4096", report.BlockSize)
			}
			if report.TimeThreshold.IsZero() {
				t.Error("TimeThreshold должен быть заполнен и когда триггер не сработал")
			}
			if scanInfo == nil {
				t.Fatal("OnScanComplete должен вызываться при заданном триггере")
			}
			if scanInfo.TotalSize != 3072 || scanInfo.ScannedFiles != 3 {
				t.Errorf("ScanCompleteInfo = %+v, want TotalSize=3072 ScannedFiles=3", *scanInfo)
			}

			for _, p := range paths {
				exists := pathExists(p)
				if tt.wantDeleted == 0 && !exists {
					t.Errorf("%s удалён, хотя триггер не сработал", p)
				}
				if tt.wantDeleted > 0 && exists {
					t.Errorf("%s должен быть удалён", p)
				}
			}
		})
	}
}

func TestCleanByAge_MinTriggerFileCount(t *testing.T) {
	tests := []struct {
		name        string
		trigger     int64
		wantDeleted int
	}{
		{"файлов ровно столько, сколько порог — срабатывает (>=)", 3, 3},
		{"порог на 1 больше — не срабатывает", 4, 0},
		{"нулевой порог — срабатывает", 0, 3},
		{"порог 1 — срабатывает", 1, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := createOldFiles(t, dir, 3)

			config, _ := ageConfig(24 * time.Hour)
			config.MinTriggerFileCount = int64Ptr(tt.trigger)

			report, err := CleanBackup(dir, config)
			if err != nil {
				t.Fatal(err)
			}

			if report.DeletedFiles != tt.wantDeleted {
				t.Errorf("DeletedFiles = %d, want %d", report.DeletedFiles, tt.wantDeleted)
			}
			if report.ScannedFiles != 3 {
				t.Errorf("ScannedFiles = %d, want 3", report.ScannedFiles)
			}
			for _, p := range paths {
				if exists := pathExists(p); exists == (tt.wantDeleted > 0) {
					t.Errorf("%s: exists=%v при wantDeleted=%d", p, exists, tt.wantDeleted)
				}
			}
		})
	}
}

// Триггеры size и count комбинируются по ИЛИ: достаточно одного сработавшего.
func TestCleanByAge_TriggersCombinedWithOr(t *testing.T) {
	const (
		sizeHit   = int64(3072)    // размер папки = 3072
		sizeMiss  = int64(1 << 40) //
		countHit  = int64(3)
		countMiss = int64(1000)
	)

	tests := []struct {
		name        string
		size, count int64
		wantDeleted int
	}{
		{"size сработал, count нет", sizeHit, countMiss, 3},
		{"count сработал, size нет", sizeMiss, countHit, 3},
		{"сработали оба", sizeHit, countHit, 3},
		{"не сработал ни один", sizeMiss, countMiss, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			createOldFiles(t, dir, 3)

			config, _ := ageConfig(24 * time.Hour)
			config.MinTriggerSize = int64Ptr(tt.size)
			config.MinTriggerFileCount = int64Ptr(tt.count)

			report, err := CleanBackup(dir, config)
			if err != nil {
				t.Fatal(err)
			}
			if report.DeletedFiles != tt.wantDeleted {
				t.Errorf("DeletedFiles = %d, want %d", report.DeletedFiles, tt.wantDeleted)
			}
		})
	}
}

// Файлы из ExcludeExtensions / ExcludeDirs не учитываются в триггерах.
func TestCleanByAge_TriggerIgnoresExcludedFiles(t *testing.T) {
	setup := func(t *testing.T) string {
		dir := t.TempDir()
		mustCreateFile(t, filepath.Join(dir, "a.dat"), 1024, 48*time.Hour)
		mustCreateFile(t, filepath.Join(dir, "b.dat"), 1024, 48*time.Hour)
		mustCreateFile(t, filepath.Join(dir, "skip.bak"), 1024, 48*time.Hour)
		for i := 0; i < 5; i++ {
			mustCreateFile(t, filepath.Join(dir, "cache", string(rune('a'+i))+".dat"), 1024, 48*time.Hour)
		}
		return dir
	}

	t.Run("триггер по количеству: считаются 2 файла, а не 8", func(t *testing.T) {
		dir := setup(t)
		config, _ := ageConfig(24 * time.Hour)
		config.ExcludeExtensions = []string{"bak"}
		config.ExcludeDirs = []string{"cache"}
		config.MinTriggerFileCount = int64Ptr(3)

		report, err := CleanBackup(dir, config)
		if err != nil {
			t.Fatal(err)
		}
		if report.ScannedFiles != 2 {
			t.Errorf("ScannedFiles = %d, want 2", report.ScannedFiles)
		}
		if report.DeletedFiles != 0 {
			t.Errorf("DeletedFiles = %d, want 0 (триггер не должен сработать)", report.DeletedFiles)
		}
	})

	t.Run("триггер по размеру: считается 2048, а не 8192", func(t *testing.T) {
		dir := setup(t)
		config, _ := ageConfig(24 * time.Hour)
		config.ExcludeExtensions = []string{"bak"}
		config.ExcludeDirs = []string{"cache"}
		config.MinTriggerSize = int64Ptr(3000)

		report, err := CleanBackup(dir, config)
		if err != nil {
			t.Fatal(err)
		}
		if report.DeletedFiles != 0 {
			t.Errorf("DeletedFiles = %d, want 0 (триггер не должен сработать)", report.DeletedFiles)
		}
	})

	t.Run("триггер сработал: исключённые файлы всё равно не удаляются", func(t *testing.T) {
		dir := setup(t)
		config, _ := ageConfig(24 * time.Hour)
		config.ExcludeExtensions = []string{"bak"}
		config.ExcludeDirs = []string{"cache"}
		config.MinTriggerFileCount = int64Ptr(2)

		report, err := CleanBackup(dir, config)
		if err != nil {
			t.Fatal(err)
		}
		if report.DeletedFiles != 2 {
			t.Errorf("DeletedFiles = %d, want 2", report.DeletedFiles)
		}
		for _, keep := range []string{"skip.bak", filepath.Join("cache", "a.dat")} {
			if !pathExists(filepath.Join(dir, keep)) {
				t.Errorf("%s не должен удаляться", keep)
			}
		}
	})
}

func TestCleanByAge_ExcludedNotDeletedWithoutTriggers(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "old.dat"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "old.KEEP"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "protected", "old.dat"), 1024, 48*time.Hour)

	config, _ := ageConfig(24 * time.Hour)
	config.ExcludeExtensions = []string{"keep"}
	config.ExcludeDirs = []string{"protected"}

	report, err := CleanBackup(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedFiles != 1 {
		t.Errorf("DeletedFiles = %d, want 1", report.DeletedFiles)
	}
	if pathExists(filepath.Join(dir, "old.dat")) {
		t.Error("old.dat должен быть удалён")
	}
	if !pathExists(filepath.Join(dir, "old.KEEP")) {
		t.Error("old.KEEP исключён по расширению, удалять нельзя")
	}
	if !pathExists(filepath.Join(dir, "protected", "old.dat")) {
		t.Error("protected/ исключена, удалять содержимое нельзя")
	}
}

func TestCleanByAge_RemovesEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "sub", "nested", "old.txt"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "keep", "old.txt"), 1024, 48*time.Hour)
	mustCreateFile(t, filepath.Join(dir, "keep", "fresh.txt"), 1024, time.Minute)
	if err := os.MkdirAll(filepath.Join(dir, "already_empty"), 0755); err != nil {
		t.Fatal(err)
	}

	config, _ := ageConfig(24 * time.Hour)
	config.RemoveEmptyDirs = true

	report, err := CleanBackup(dir, config)
	if err != nil {
		t.Fatal(err)
	}

	if report.DeletedFiles != 2 {
		t.Errorf("DeletedFiles = %d, want 2", report.DeletedFiles)
	}
	// sub/nested, sub, already_empty
	if report.DeletedDirs != 3 {
		t.Errorf("DeletedDirs = %d, want 3", report.DeletedDirs)
	}
	for _, gone := range []string{"sub", "already_empty"} {
		if pathExists(filepath.Join(dir, gone)) {
			t.Errorf("%s должна быть удалена как пустая", gone)
		}
	}
	if !pathExists(filepath.Join(dir, "keep", "fresh.txt")) {
		t.Error("keep/fresh.txt не должен удаляться")
	}
	if !pathExists(dir) {
		t.Error("корень target не должен удаляться")
	}
}

func TestCleanByAge_KeepsEmptyDirsWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "sub", "old.txt"), 1024, 48*time.Hour)

	config, _ := ageConfig(24 * time.Hour)
	config.RemoveEmptyDirs = false

	report, err := CleanBackup(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedFiles != 1 {
		t.Errorf("DeletedFiles = %d, want 1", report.DeletedFiles)
	}
	if report.DeletedDirs != 0 {
		t.Errorf("DeletedDirs = %d, want 0", report.DeletedDirs)
	}
	if !pathExists(filepath.Join(dir, "sub")) {
		t.Error("sub должна остаться при RemoveEmptyDirs=false")
	}
}

func TestConfigValidation_AgeMode(t *testing.T) {
	tests := []struct {
		name    string
		config  CleaningConfig
		wantErr error
	}{
		{
			name:   "MaxAge один — валидно",
			config: CleaningConfig{MaxAge: durationPtr(time.Hour)},
		},
		{
			name:   "MaxAge = 0 — валидно",
			config: CleaningConfig{MaxAge: durationPtr(0)},
		},
		{
			name:    "MaxAge < 0",
			config:  CleaningConfig{MaxAge: durationPtr(-time.Hour)},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "MaxAge + MinTriggerSize + MinTriggerFileCount — валидно",
			config: CleaningConfig{
				MaxAge:              durationPtr(time.Hour),
				MinTriggerSize:      int64Ptr(1),
				MinTriggerFileCount: int64Ptr(1),
			},
		},
		{
			name: "MinTriggerSize без MaxAge",
			config: CleaningConfig{
				MaxSize:        int64Ptr(1),
				MinTriggerSize: int64Ptr(1),
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "MinTriggerFileCount без MaxAge",
			config: CleaningConfig{
				MaxSize:             int64Ptr(1),
				MinTriggerFileCount: int64Ptr(1),
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "MinTriggerSize < 0",
			config: CleaningConfig{
				MaxAge:         durationPtr(time.Hour),
				MinTriggerSize: int64Ptr(-1),
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "MinTriggerFileCount < 0",
			config: CleaningConfig{
				MaxAge:              durationPtr(time.Hour),
				MinTriggerFileCount: int64Ptr(-1),
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name:    "ничего не задано",
			config:  CleaningConfig{},
			wantErr: ErrNoCapacitySpecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
