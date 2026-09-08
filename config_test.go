package gobackupcleaner

import (
	"runtime"
	"testing"
	"time"
)

// TestConfigConcurrencyDefaults тестирует настройки параллелизма по умолчанию
func TestConfigConcurrencyDefaults(t *testing.T) {
	tests := []struct {
		name                   string
		config                 CleaningConfig
		expectedWorkers        int
		expectedConcurrency    int
		expectedMaxConcurrency int
	}{
		{
			name:                   "All defaults",
			config:                 CleaningConfig{},
			expectedWorkers:        min(runtime.NumCPU(), 4),
			expectedConcurrency:    runtime.NumCPU(),
			expectedMaxConcurrency: 4,
		},
		{
			name: "Concurrency specified, under max",
			config: CleaningConfig{
				Concurrency: 2,
			},
			expectedWorkers:        2,
			expectedConcurrency:    2,
			expectedMaxConcurrency: 4,
		},
		{
			name: "Concurrency specified, over max",
			config: CleaningConfig{
				Concurrency: 8,
			},
			expectedWorkers:        4,
			expectedConcurrency:    8,
			expectedMaxConcurrency: 4,
		},
		{
			name: "Custom MaxConcurrency",
			config: CleaningConfig{
				Concurrency:    10,
				MaxConcurrency: 6,
			},
			expectedWorkers:        6,
			expectedConcurrency:    10,
			expectedMaxConcurrency: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.setDefaults()

			effectiveWorkers := tt.config.ActualWorkerCount()
			if effectiveWorkers != tt.expectedWorkers {
				t.Errorf("Expected ActualWorkerCount %d, got %d", tt.expectedWorkers, effectiveWorkers)
			}
			if tt.config.Concurrency != tt.expectedConcurrency {
				t.Errorf("Expected Concurrency %d, got %d", tt.expectedConcurrency, tt.config.Concurrency)
			}
			if tt.config.MaxConcurrency != tt.expectedMaxConcurrency {
				t.Errorf("Expected MaxConcurrency %d, got %d", tt.expectedMaxConcurrency, tt.config.MaxConcurrency)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestConfigTimeWindowDefault тестирует значение TimeWindow по умолчанию
func TestConfigTimeWindowDefault(t *testing.T) {
	config := CleaningConfig{}
	config.setDefaults()

	expectedWindow := 5 * time.Minute
	if config.TimeWindow != expectedWindow {
		t.Errorf("Expected TimeWindow %v, got %v", expectedWindow, config.TimeWindow)
	}

	// Проверяем, что явно заданное значение не перезаписывается
	customWindow := 10 * time.Minute
	config2 := CleaningConfig{
		TimeWindow: customWindow,
	}
	config2.setDefaults()

	if config2.TimeWindow != customWindow {
		t.Errorf("Expected TimeWindow %v, got %v", customWindow, config2.TimeWindow)
	}
}
