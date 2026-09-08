package gobackupcleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanner(t *testing.T) {
	// Создаём временную директорию
	tmpDir, err := os.MkdirTemp("", "scanner-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
	}()

	// Создаём структуру тестовых файлов
	now := time.Now()
	testFiles := []struct {
		path    string
		size    int64
		modTime time.Time
	}{
		{"file1.txt", 1024, now.Add(-2 * time.Hour)},
		{"file2.txt", 2048, now.Add(-1 * time.Hour)},
		{"dir1/file3.txt", 512, now.Add(-30 * time.Minute)},
		{"dir1/dir2/file4.txt", 256, now},
	}

	// Создаём директории
	if err := os.Mkdir(filepath.Join(tmpDir, "dir1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, "dir1", "dir2"), 0755); err != nil {
		t.Fatal(err)
	}

	// Создаём файлы
	for _, tf := range testFiles {
		path := filepath.Join(tmpDir, tf.path)
		if err := createTestFile(t, path, tf.size, tf.modTime); err != nil {
			t.Fatal(err)
		}
	}

	// Тестируем сканер
	config := CleaningConfig{
		TimeWindow:  time.Hour,
		Concurrency: 2,
	}
	config.setDefaults()

	scanner := newScanner(&config, 4096)
	_ = scanner.scan(tmpDir)

	// Проверяем результаты
	totalFiles := scanner.getTotalFiles()
	if totalFiles != len(testFiles) {
		t.Errorf("Expected %d files, got %d", len(testFiles), totalFiles)
	}

	// Проверяем временные слоты
	slots := scanner.getTimeSlots()
	if len(slots) == 0 {
		t.Error("Expected at least one time slot")
	}

	// Проверяем, что слоты отсортированы (сначала старые)
	for i := 1; i < len(slots); i++ {
		if slots[i-1].time.After(slots[i].time) {
			t.Error("Time slots are not sorted correctly")
		}
	}
}

func TestScannerWithSymlinks(t *testing.T) {
	// Создаём временную директорию
	tmpDir, err := os.MkdirTemp("", "scanner-symlink-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
	}()

	// Создаём файл и символьную ссылку на него
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := createTestFile(t, testFile, 1024, time.Now()); err != nil {
		t.Fatal(err)
	}

	symlink := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(testFile, symlink); err != nil {
		t.Skip("Cannot create symlinks on this system")
	}

	// Тестируем сканер
	config := CleaningConfig{
		TimeWindow:  time.Hour,
		Concurrency: 1,
	}
	config.setDefaults()

	scanner := newScanner(&config, 4096)
	_ = scanner.scan(tmpDir)

	// Должны учитываться только обычные файлы, символьные ссылки — нет
	totalFiles := scanner.getTotalFiles()
	if totalFiles != 1 {
		t.Errorf("Expected 1 file (symlinks should be ignored), got %d", totalFiles)
	}
}

func TestScannerWithPermissionError(t *testing.T) {
	// Создаём временную директорию
	tmpDir, err := os.MkdirTemp("", "scanner-perm-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
	}()

	// Создаём директорию без прав на чтение
	restrictedDir := filepath.Join(tmpDir, "restricted")
	if err := os.Mkdir(restrictedDir, 0000); err != nil {
		t.Fatal(err)
	}

	// Создаём обычный файл
	if err := createTestFile(t, filepath.Join(tmpDir, "normal.txt"), 1024, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Тестируем сканер с коллбэком ошибок
	errorCount := 0
	config := CleaningConfig{
		TimeWindow:  time.Hour,
		Concurrency: 1,
		Callbacks: Callbacks{
			OnError: func(info ErrorInfo) {
				errorCount++
			},
		},
	}
	config.setDefaults()

	scanner := newScanner(&config, 4096)
	_ = scanner.scan(tmpDir)

	// Обработка должна продолжиться, несмотря на ошибку доступа
	totalFiles := scanner.getTotalFiles()
	if totalFiles != 1 {
		t.Errorf("Expected 1 file despite permission error, got %d", totalFiles)
	}

	// Восстанавливаем права для последующей очистки
	if err := os.Chmod(restrictedDir, 0755); err != nil {
		t.Logf("Warning: failed to restore permissions: %v", err)
	}
}

func TestTimeSlotAggregation(t *testing.T) {
	config := CleaningConfig{
		TimeWindow:  time.Hour,
		Concurrency: 1,
	}
	config.setDefaults()

	scanner := newScanner(&config, 4096)

	// Добавляем файлы с разными временными метками
	baseTime := time.Now().Truncate(time.Hour)

	// Файлы в одном временном окне
	scanner.addFile(fileInfo{
		path:      "file1.txt",
		size:      1000,
		blockSize: 4096,
		modTime:   baseTime.Add(10 * time.Minute),
	})
	scanner.addFile(fileInfo{
		path:      "file2.txt",
		size:      2000,
		blockSize: 4096,
		modTime:   baseTime.Add(30 * time.Minute),
	})

	// Файл в другом временном окне
	scanner.addFile(fileInfo{
		path:      "file3.txt",
		size:      3000,
		blockSize: 4096,
		modTime:   baseTime.Add(90 * time.Minute),
	})

	slots := scanner.getTimeSlots()
	if len(slots) != 2 {
		t.Errorf("Expected 2 time slots, got %d", len(slots))
	}

	// Проверяем первый слот
	if len(slots[0].files) != 2 {
		t.Errorf("Expected 2 files in first slot, got %d", len(slots[0].files))
	}
	if slots[0].totalSize != 3000 {
		t.Errorf("Expected total size 3000 in first slot, got %d", slots[0].totalSize)
	}
	if slots[0].totalBlockSize != 8192 {
		t.Errorf("Expected total block size 8192 in first slot, got %d", slots[0].totalBlockSize)
	}

	// Проверяем второй слот
	if len(slots[1].files) != 1 {
		t.Errorf("Expected 1 file in second slot, got %d", len(slots[1].files))
	}
}
