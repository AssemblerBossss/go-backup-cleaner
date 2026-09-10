package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	cleaner "github.com/ideamans/go-backup-cleaner"
)

// yamlConfig — корневая структура YAML-файла конфигурации.
// Поля Global* задают значения по умолчанию для всех targets, каждый
// target может их переопределить своими собственными.
type yamlConfig struct {
	Concurrency    int          `yaml:"concurrency"`
	MaxConcurrency int          `yaml:"max_concurrency"`
	TimeWindow     string       `yaml:"time_window"` // строка вида "5m", парсим через time.ParseDuration
	Targets        []yamlTarget `yaml:"targets"`
}

// yamlTarget — правила очистки для одной директории. Указатели там, где нужно отличать
// "не задано" от "задано нулём" (например, MaxUsagePercent: 0 — валидное, но
// бессмысленное значение, поэтому используем *float64, а не float64).
type yamlTarget struct {
	Path string `yaml:"path"`

	MinFreeSpaceGB  *int64   `yaml:"min_free_space_gb"`
	MaxUsagePercent *float64 `yaml:"max_usage_percent"`
	MaxSizeGB       *int64   `yaml:"max_size_gb"`

	TimeWindow      string `yaml:"time_window"`       // переопределение глобального, необязательно
	RemoveEmptyDirs *bool  `yaml:"remove_empty_dirs"` // *bool, чтобы отличить "не указано" от false
	DryRun          bool   `yaml:"dry_run"`

	MaxAge string `yaml:"max_age"` // "720h", "30d", "4w"

	ExcludeDirs       []string `yaml:"exclude_dirs"`
	ExcludeExtensions []string `yaml:"exclude_extensions"` // имена директорий, любая глубина
}

// loadYAMLConfig читает и парсит YAML-файл конфигурации по пути path.
func loadYAMLConfig(path string) (*yamlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение конфига %s: %w", path, err)
	}

	var cfg yamlConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разбор YAML %s: %w", path, err)
	}

	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("в конфиге %s не указано ни одного target", path)
	}

	return &cfg, nil
}

// resolvedTarget — то, что реально пойдёт в cleaner.CleanBackup:
// путь к директории + готовый CleaningConfig.
type resolvedTarget struct {
	Path   string
	Config cleaner.CleaningConfig
}

// buildTargets превращает распарсенный YAML в список задач на очистку,
// применяя глобальные настройки как дефолт и позволяя каждому target
// их переопределить. Возвращает также список ошибок валидации,
// которые НЕ прерывают весь запуск — битый target пропускается, чтобы
// один опечатанный путь не остановил очистку остальных директорий
func buildTargets(cfg *yamlConfig) ([]resolvedTarget, []error) {
	globalWindow := 5 * time.Minute
	if cfg.TimeWindow != "" {
		if d, err := time.ParseDuration(cfg.TimeWindow); err == nil {
			globalWindow = d
		}
	}

	var targets []resolvedTarget
	var errs []error

	for i, t := range cfg.Targets {
		if t.Path == "" {
			errs = append(errs, fmt.Errorf("targets[%d]: не указан path", i))
			continue
		}

		info, err := os.Stat(t.Path)
		if err != nil || !info.IsDir() {
			errs = append(errs, fmt.Errorf("targets[%d] (%s): директория недоступна: %v", i, t.Path, err))
			continue
		}

		window := globalWindow
		if t.TimeWindow != "" {
			if d, err := time.ParseDuration(t.TimeWindow); err == nil {
				window = d
			} else {
				errs = append(errs, fmt.Errorf("targets[%d] (%s): некорректный time_window: %v", i, t.Path, err))
			}
		}

		removeEmptyDirs := true // дефолт библиотеки
		if t.RemoveEmptyDirs != nil {
			removeEmptyDirs = *t.RemoveEmptyDirs
		}

		conf := cleaner.CleaningConfig{
			MaxUsagePercent:   t.MaxUsagePercent,
			TimeWindow:        window,
			RemoveEmptyDirs:   removeEmptyDirs,
			Concurrency:       cfg.Concurrency,
			MaxConcurrency:    cfg.MaxConcurrency,
			DryRun:            t.DryRun,
			ExcludeExtensions: t.ExcludeExtensions,
		}

		// GB -> байты переводим тут, а не в CleaningConfig — библиотека
		// работает в байтах, GB — удобство только для человека,
		// читающего YAML.
		if t.MinFreeSpaceGB != nil {
			bytes := *t.MinFreeSpaceGB * 1024 * 1024 * 1024
			conf.MinFreeSpace = &bytes
		}
		if t.MaxSizeGB != nil {
			bytes := *t.MaxSizeGB * 1024 * 1024 * 1024
			conf.MaxSize = &bytes
		}

		if conf.MinFreeSpace == nil && conf.MaxUsagePercent == nil && conf.MaxSize == nil {
			errs = append(errs, fmt.Errorf("targets[%d] (%s): не задано ни одно из min_free_space_gb / max_usage_percent / max_size_gb", i, t.Path))
			continue
		}

		targets = append(targets, resolvedTarget{
			Path:   filepath.Clean(t.Path),
			Config: conf,
		})
	}

	return targets, errs
}
