package gobackupcleaner

import (
	"os"
	"testing"
)

func TestDefaultDiskInfoProvider(t *testing.T) {
	provider := &DefaultDiskInfoProvider{}

	// Тестируем на текущей директории
	usage, err := provider.GetDiskUsage(".")
	if err != nil {
		t.Fatalf("Failed to get disk usage: %v", err)
	}

	// Базовые проверки на здравый смысл
	if usage.Total == 0 {
		t.Error("Total disk size should not be 0")
	}
	if usage.Used > usage.Total {
		t.Error("Used space should not exceed total space")
	}
	if usage.Free > usage.Total {
		t.Error("Free space should not exceed total space")
	}
	if usage.Used+usage.Free > usage.Total {
		t.Error("Used + Free should not exceed Total")
	}
	if usage.UsedPercent < 0 || usage.UsedPercent > 100 {
		t.Errorf("UsedPercent should be between 0 and 100, got %f", usage.UsedPercent)
	}

	// Проверяем размер блока
	blockSize, err := provider.GetBlockSize(".")
	if err != nil {
		t.Fatalf("Failed to get block size: %v", err)
	}
	if blockSize <= 0 {
		t.Error("Block size should be positive")
	}
}

func TestCalculateBlockSize(t *testing.T) {
	tests := []struct {
		name         string
		fileSize     int64
		blockSize    int64
		expectedSize int64
	}{
		{
			name:         "Exact block size",
			fileSize:     4096,
			blockSize:    4096,
			expectedSize: 4096,
		},
		{
			name:         "Less than one block",
			fileSize:     100,
			blockSize:    4096,
			expectedSize: 4096,
		},
		{
			name:         "Multiple blocks",
			fileSize:     5000,
			blockSize:    4096,
			expectedSize: 8192, // 2 blocks
		},
		{
			name:         "Zero file size",
			fileSize:     0,
			blockSize:    4096,
			expectedSize: 0,
		},
		{
			name:         "Invalid block size",
			fileSize:     1000,
			blockSize:    0,
			expectedSize: 1000, // Falls back to file size
		},
		{
			name:         "Negative block size",
			fileSize:     1000,
			blockSize:    -4096,
			expectedSize: 1000, // Falls back to file size
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBlockSize(tt.fileSize, tt.blockSize)
			if result != tt.expectedSize {
				t.Errorf("Expected %d, got %d", tt.expectedSize, result)
			}
		})
	}
}

func TestDiskInfoProviderWithInvalidPath(t *testing.T) {
	provider := &DefaultDiskInfoProvider{}

	// Тестируем на несуществующем пути
	tmpFile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	nonExistentPath := tmpFile.Name() + "_nonexistent"
	if err := tmpFile.Close(); err != nil {
		t.Logf("tmpFile close failed: %v", err)
	}
	if err := os.Remove(tmpFile.Name()); err != nil {
		t.Logf("tmpFile remove failed: %v", err)
	}

	_, err = provider.GetDiskUsage(nonExistentPath)
	if err == nil {
		t.Error("Expected error for non-existent path")
	}

	_, err = provider.GetBlockSize(nonExistentPath)
	if err == nil {
		t.Error("Expected error for non-existent path")
	}
}
