package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	cleaner "github.com/ideamans/go-backup-cleaner"
)

func main() {
	configPath := flag.String("config", "", "путь к YAML-файлу конфигурации (обязателен)")
	dryRunOverride := flag.Bool("dry-run", false, "форсировать dry-run для ВСЕХ targets, игнорируя dry_run из YAML")
	verbose := flag.Bool("verbose", false, "подробный вывод по каждому файлу/директории")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("нужен путь к конфигу: -config path/to/config.yaml")
	}

	cfg, err := loadYAMLConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	targets, errs := buildTargets(cfg)
	for _, e := range errs {
		// Ошибка валидации отдельного target не останавливает весь
		// запуск — только сообщается и пропускается (см. buildTargets).
		log.Printf("пропуск target: %v", e)
	}
	if len(targets) == 0 {
		log.Fatal("не осталось ни одного валидного target для очистки")
	}

	var totalFiles, totalDirs int
	var totalSize int64
	exitCode := 0

	for _, target := range targets {
		if *dryRunOverride {
			target.Config.DryRun = true
		}

		if *verbose {
			attachVerboseCallbacks(&target.Config, target.Path)
		}

		fmt.Printf("=== %s ===\n", target.Path)
		report, err := cleaner.CleanBackup(target.Path, target.Config)
		if err != nil {
			log.Printf("ошибка очистки %s: %v", target.Path, err)
			exitCode = 1
			continue
		}

		fmt.Printf("удалено файлов: %d, директорий: %d, освобождено: %s\n\n",
			report.DeletedFiles, report.DeletedDirs, formatBytes(report.DeletedSize))

		totalFiles += report.DeletedFiles
		totalDirs += report.DeletedDirs
		totalSize += report.DeletedSize
	}

	fmt.Printf("ИТОГО по всем targets: файлов %d, директорий %d, освобождено %s\n",
		totalFiles, totalDirs, formatBytes(totalSize))

	os.Exit(exitCode)
}

// attachVerboseCallbacks вешает коллбэки на config для подробного
// вывода. targetPath добавляется префиксом в лог, чтобы при обработке
// нескольких директорий за один запуск было понятно, где что удаляется.
func attachVerboseCallbacks(cfg *cleaner.CleaningConfig, targetPath string) {
	cfg.Callbacks = cleaner.Callbacks{
		OnFileDeleted: func(info cleaner.FileDeletedInfo) {
			verb := "удалён"
			if cfg.DryRun {
				verb = "был бы удалён"
			}
			fmt.Printf("[%s] %s: %s (%s)\n", targetPath, verb, info.Path, formatBytes(info.Size))
		},
		OnDirDeleted: func(info cleaner.DirDeletedInfo) {
			fmt.Printf("[%s] пустая директория удалена: %s\n", targetPath, info.Path)
		},
		OnError: func(info cleaner.ErrorInfo) {
			log.Printf("[%s] ошибка [%s]: %v", targetPath, info.Type, info.Error)
		},
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
