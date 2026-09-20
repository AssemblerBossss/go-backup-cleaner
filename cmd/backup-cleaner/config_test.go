package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestParseFlexibleDuration(t *testing.T) {
	const day = 24 * time.Hour

	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		// стандартный time.ParseDuration
		{"720h", 720 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1h30m", 90 * time.Minute, false},
		{"1.5h", 90 * time.Minute, false},
		{"0", 0, false},
		{"-1h", -time.Hour, false},

		// дни
		{"1d", day, false},
		{"30d", 30 * day, false},
		{"1.5d", 36 * time.Hour, false},
		{"0d", 0, false},
		{"-2d", -2 * day, false},

		// недели
		{"1w", 7 * day, false},
		{"4w", 28 * day, false},
		{"0.5w", 84 * time.Hour, false},
		{"2w", 336 * time.Hour, false},

		// пробелы по краям для d/w допустимы
		{"  30d  ", 30 * day, false},

		// ошибки
		{"", 0, true},
		{"   ", 0, true},
		{"d", 0, true},
		{"w", 0, true},
		{"30", 0, true},         // нет единицы измерения
		{"1mo", 0, true},        // месяцы намеренно не поддерживаются
		{"1y", 0, true},         // годы не поддерживаются
		{"30D", 0, true},        // суффикс только в нижнем регистре
		{"1W", 0, true},         //
		{"1d2h", 0, true},       // комбинации с d/w не поддерживаются
		{"1.d", 0, true},        //
		{".5d", 0, true},        // нужна целая часть
		{"abc", 0, true},        //
		{"30 d", 0, true},       // пробел между числом и суффиксом
		{"--1d", 0, true},       //
		{"1e3d", 0, true},       // экспонента не поддерживается
		{"+1d", 0, true},        // знак + не поддерживается в d/w
		{"1d ", 1 * day, false}, // хвостовой пробел обрезается
		{" 1w", 7 * day, false}, // начальный пробел обрезается
		{"\t2d\t", 2 * day, false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseFlexibleDuration(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseFlexibleDuration(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseFlexibleDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseFlexibleDuration_ErrorMentionsInput(t *testing.T) {
	_, err := parseFlexibleDuration("1mo")
	if err == nil {
		t.Fatal("ожидали ошибку")
	}
	if !strings.Contains(err.Error(), "1mo") {
		t.Errorf("сообщение об ошибке должно содержать исходную строку: %v", err)
	}
}

// writeYAML пишет содержимое во временный файл и возвращает путь к нему
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadYAMLConfig_FullConfig(t *testing.T) {
	path := writeYAML(t, `
concurrency: 3
max_concurrency: 6
time_window: 10m
targets:
  - path: /data/a
    min_free_space_gb: 50
    max_usage_percent: 85.5
    max_size_gb: 100
    time_window: 1h
    remove_empty_dirs: false
    dry_run: true
    exclude_dirs: [cache, .git]
    exclude_extensions: [log, .tmp]
  - path: /data/b
    max_age: 30d
    age_trigger_size_gb: 1.5
    age_trigger_file_count: 1000
`)

	cfg, err := loadYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Concurrency != 3 || cfg.MaxConcurrency != 6 || cfg.TimeWindow != "10m" {
		t.Errorf("глобальные поля: %+v", cfg)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(cfg.Targets))
	}

	a := cfg.Targets[0]
	if a.Path != "/data/a" {
		t.Errorf("a.Path = %q", a.Path)
	}
	if a.MinFreeSpaceGB == nil || *a.MinFreeSpaceGB != 50 {
		t.Errorf("a.MinFreeSpaceGB = %v, want 50", a.MinFreeSpaceGB)
	}
	if a.MaxUsagePercent == nil || *a.MaxUsagePercent != 85.5 {
		t.Errorf("a.MaxUsagePercent = %v, want 85.5", a.MaxUsagePercent)
	}
	if a.MaxSizeGB == nil || *a.MaxSizeGB != 100 {
		t.Errorf("a.MaxSizeGB = %v, want 100", a.MaxSizeGB)
	}
	if a.TimeWindow != "1h" {
		t.Errorf("a.TimeWindow = %q, want 1h", a.TimeWindow)
	}
	if a.RemoveEmptyDirs == nil || *a.RemoveEmptyDirs != false {
		t.Errorf("a.RemoveEmptyDirs = %v, want указатель на false", a.RemoveEmptyDirs)
	}
	if !a.DryRun {
		t.Error("a.DryRun = false, want true")
	}
	if strings.Join(a.ExcludeDirs, ",") != "cache,.git" {
		t.Errorf("a.ExcludeDirs = %v", a.ExcludeDirs)
	}
	if strings.Join(a.ExcludeExtensions, ",") != "log,.tmp" {
		t.Errorf("a.ExcludeExtensions = %v", a.ExcludeExtensions)
	}

	b := cfg.Targets[1]
	if b.MaxAge != "30d" {
		t.Errorf("b.MaxAge = %q, want 30d", b.MaxAge)
	}
	if b.AgeTriggerSizeGB == nil || *b.AgeTriggerSizeGB != 1.5 {
		t.Errorf("b.AgeTriggerSizeGB = %v, want 1.5", b.AgeTriggerSizeGB)
	}
	if b.AgeTriggerFileCount == nil || *b.AgeTriggerFileCount != 1000 {
		t.Errorf("b.AgeTriggerFileCount = %v, want 1000", b.AgeTriggerFileCount)
	}
	// незаданные поля должны остаться nil, чтобы отличать "не задано" от нуля
	if b.MinFreeSpaceGB != nil || b.MaxUsagePercent != nil || b.MaxSizeGB != nil || b.RemoveEmptyDirs != nil {
		t.Errorf("незаданные указатели должны быть nil: %+v", b)
	}
}

func TestLoadYAMLConfig_ExplicitZeroDiffersFromUnset(t *testing.T) {
	path := writeYAML(t, `
targets:
  - path: /data
    max_usage_percent: 0
    min_free_space_gb: 0
`)
	cfg, err := loadYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	tg := cfg.Targets[0]
	if tg.MaxUsagePercent == nil || *tg.MaxUsagePercent != 0 {
		t.Errorf("MaxUsagePercent = %v, want указатель на 0", tg.MaxUsagePercent)
	}
	if tg.MinFreeSpaceGB == nil || *tg.MinFreeSpaceGB != 0 {
		t.Errorf("MinFreeSpaceGB = %v, want указатель на 0", tg.MinFreeSpaceGB)
	}
}

func TestLoadYAMLConfig_Errors(t *testing.T) {
	t.Run("файл не существует", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nope.yaml")
		_, err := loadYAMLConfig(path)
		if err == nil {
			t.Fatal("ожидали ошибку")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ошибка должна оборачивать os.ErrNotExist: %v", err)
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("ошибка должна содержать путь: %v", err)
		}
	})

	tests := []struct {
		name    string
		content string
	}{
		{"невалидный YAML", "targets: [unclosed"},
		{"неверный тип поля", "concurrency: abc\ntargets:\n  - path: /x\n"},
		{"неверный тип элемента targets", "targets: 42"},
		{"пустой файл", ""},
		{"только комментарий", "# nothing here\n"},
		{"targets пустой список", "targets: []"},
		{"targets отсутствует", "concurrency: 2\n"},
		{"targets: null", "targets:\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeYAML(t, tt.content)
			cfg, err := loadYAMLConfig(path)
			if err == nil {
				t.Fatalf("ожидали ошибку, got cfg=%+v", cfg)
			}
			if cfg != nil {
				t.Errorf("при ошибке cfg должен быть nil, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("ошибка должна содержать путь к конфигу: %v", err)
			}
		})
	}
}

func hasErrContaining(errs []error, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), substr) {
			return true
		}
	}
	return false
}

func TestBuildTargets_CapacityFields(t *testing.T) {
	dir := t.TempDir()

	cfg := &yamlConfig{
		Concurrency:    3,
		MaxConcurrency: 6,
		Targets: []yamlTarget{{
			Path:              dir,
			MinFreeSpaceGB:    ptr(int64(10)),
			MaxUsagePercent:   ptr(85.5),
			MaxSizeGB:         ptr(int64(2)),
			DryRun:            true,
			ExcludeDirs:       []string{"cache"},
			ExcludeExtensions: []string{"log"},
		}},
	}

	targets, errs := buildTargets(cfg)
	if len(errs) != 0 {
		t.Fatalf("неожиданные ошибки: %v", errs)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}

	got := targets[0]
	if got.Path != filepath.Clean(dir) {
		t.Errorf("Path = %q, want %q", got.Path, filepath.Clean(dir))
	}

	c := got.Config
	if c.MinFreeSpace == nil || *c.MinFreeSpace != 10<<30 {
		t.Errorf("MinFreeSpace = %v, want %d", c.MinFreeSpace, int64(10<<30))
	}
	if c.MaxSize == nil || *c.MaxSize != 2<<30 {
		t.Errorf("MaxSize = %v, want %d", c.MaxSize, int64(2<<30))
	}
	if c.MaxUsagePercent == nil || *c.MaxUsagePercent != 85.5 {
		t.Errorf("MaxUsagePercent = %v, want 85.5", c.MaxUsagePercent)
	}
	if c.MaxAge != nil || c.MinTriggerSize != nil || c.MinTriggerFileCount != nil {
		t.Errorf("age-поля должны быть nil: %+v", c)
	}
	if c.Concurrency != 3 || c.MaxConcurrency != 6 {
		t.Errorf("Concurrency=%d MaxConcurrency=%d, want 3 / 6", c.Concurrency, c.MaxConcurrency)
	}
	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !c.RemoveEmptyDirs {
		t.Error("RemoveEmptyDirs по умолчанию должен быть true")
	}
	if c.TimeWindow != 5*time.Minute {
		t.Errorf("TimeWindow = %v, want 5m по умолчанию", c.TimeWindow)
	}
	if len(c.ExcludeDirs) != 1 || c.ExcludeDirs[0] != "cache" {
		t.Errorf("ExcludeDirs = %v", c.ExcludeDirs)
	}
	if len(c.ExcludeExtensions) != 1 || c.ExcludeExtensions[0] != "log" {
		t.Errorf("ExcludeExtensions = %v", c.ExcludeExtensions)
	}
}

func TestBuildTargets_PathIsCleaned(t *testing.T) {
	dir := t.TempDir()
	cfg := &yamlConfig{Targets: []yamlTarget{{
		Path:      dir + string(os.PathSeparator) + "." + string(os.PathSeparator),
		MaxSizeGB: ptr(int64(1)),
	}}}

	targets, errs := buildTargets(cfg)
	if len(errs) != 0 || len(targets) != 1 {
		t.Fatalf("targets=%v errs=%v", targets, errs)
	}
	if targets[0].Path != filepath.Clean(dir) {
		t.Errorf("Path = %q, want %q", targets[0].Path, filepath.Clean(dir))
	}
}

func TestBuildTargets_AgeMode(t *testing.T) {
	dir := t.TempDir()

	t.Run("только max_age", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{Path: dir, MaxAge: "30d"}}})
		if len(errs) != 0 || len(targets) != 1 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		c := targets[0].Config
		if c.MaxAge == nil || *c.MaxAge != 30*24*time.Hour {
			t.Errorf("MaxAge = %v, want 720h", c.MaxAge)
		}
		if c.MinTriggerSize != nil || c.MinTriggerFileCount != nil {
			t.Errorf("триггеры должны быть nil: %+v", c)
		}
		if c.MinFreeSpace != nil || c.MaxSize != nil || c.MaxUsagePercent != nil {
			t.Errorf("capacity-поля должны быть nil: %+v", c)
		}
	})

	t.Run("max_age в неделях и часах", func(t *testing.T) {
		for in, want := range map[string]time.Duration{
			"4w":   28 * 24 * time.Hour,
			"720h": 720 * time.Hour,
		} {
			targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{Path: dir, MaxAge: in}}})
			if len(errs) != 0 || len(targets) != 1 {
				t.Fatalf("max_age=%s: targets=%v errs=%v", in, targets, errs)
			}
			if got := *targets[0].Config.MaxAge; got != want {
				t.Errorf("max_age=%s: MaxAge = %v, want %v", in, got, want)
			}
		}
	})

	t.Run("триггеры", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
			Path:                dir,
			MaxAge:              "30d",
			AgeTriggerSizeGB:    ptr(1.5),
			AgeTriggerFileCount: ptr(int64(1000)),
		}}})
		if len(errs) != 0 || len(targets) != 1 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		c := targets[0].Config
		if c.MinTriggerSize == nil || *c.MinTriggerSize != int64(1.5*1024*1024*1024) {
			t.Errorf("MinTriggerSize = %v, want %d", c.MinTriggerSize, int64(1.5*1024*1024*1024))
		}
		if c.MinTriggerFileCount == nil || *c.MinTriggerFileCount != 1000 {
			t.Errorf("MinTriggerFileCount = %v, want 1000", c.MinTriggerFileCount)
		}
	})

	t.Run("только триггер по количеству", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
			Path: dir, MaxAge: "7d", AgeTriggerFileCount: ptr(int64(5)),
		}}})
		if len(errs) != 0 || len(targets) != 1 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		c := targets[0].Config
		if c.MinTriggerSize != nil {
			t.Errorf("MinTriggerSize должен быть nil, got %v", *c.MinTriggerSize)
		}
		if c.MinTriggerFileCount == nil || *c.MinTriggerFileCount != 5 {
			t.Errorf("MinTriggerFileCount = %v, want 5", c.MinTriggerFileCount)
		}
	})

	t.Run("max_age взаимоисключим с capacity-полями", func(t *testing.T) {
		capacity := map[string]yamlTarget{
			"min_free_space_gb": {MinFreeSpaceGB: ptr(int64(1))},
			"max_usage_percent": {MaxUsagePercent: ptr(80.0)},
			"max_size_gb":       {MaxSizeGB: ptr(int64(1))},
		}
		for name, tg := range capacity {
			tg.Path = dir
			tg.MaxAge = "30d"
			targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{tg}})
			if len(targets) != 0 {
				t.Errorf("%s + max_age: target должен быть пропущен, got %v", name, targets)
			}
			if len(errs) != 1 || !hasErrContaining(errs, "max_age") {
				t.Errorf("%s + max_age: errs = %v", name, errs)
			}
		}
	})

	t.Run("некорректный max_age", func(t *testing.T) {
		for _, bad := range []string{"1mo", "abc", "30"} {
			targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{Path: dir, MaxAge: bad}}})
			if len(targets) != 0 {
				t.Errorf("max_age=%q: target должен быть пропущен", bad)
			}
			if len(errs) != 1 || !hasErrContaining(errs, "max_age") {
				t.Errorf("max_age=%q: errs = %v", bad, errs)
			}
		}
	})

	t.Run("age_trigger_size_gb без max_age", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
			Path: dir, AgeTriggerSizeGB: ptr(1.0),
		}}})
		// ошибка про триггер + ошибка "не задан ни один режим"; target пропущен
		if len(targets) != 0 {
			t.Errorf("target должен быть пропущен, got %v", targets)
		}
		if !hasErrContaining(errs, "age_trigger_size_gb") {
			t.Errorf("ожидали ошибку про age_trigger_size_gb: %v", errs)
		}
	})

	t.Run("age_trigger_file_count без max_age", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
			Path: dir, AgeTriggerFileCount: ptr(int64(10)),
		}}})
		if len(targets) != 0 {
			t.Errorf("target должен быть пропущен, got %v", targets)
		}
		if !hasErrContaining(errs, "age_trigger_file_count") {
			t.Errorf("ожидали ошибку про age_trigger_file_count: %v", errs)
		}
	})

	t.Run("триггер вместе с capacity-полем без max_age — ошибка про триггер", func(t *testing.T) {
		_, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
			Path: dir, MaxSizeGB: ptr(int64(1)), AgeTriggerFileCount: ptr(int64(10)),
		}}})
		if !hasErrContaining(errs, "age_trigger_file_count") {
			t.Errorf("ожидали ошибку про age_trigger_file_count: %v", errs)
		}
	})
}

func TestBuildTargets_InvalidTargets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		target  yamlTarget
		wantErr string
	}{
		{"пустой path", yamlTarget{MaxSizeGB: ptr(int64(1))}, "не указан path"},
		{"path не существует", yamlTarget{Path: filepath.Join(dir, "missing"), MaxSizeGB: ptr(int64(1))}, "директория недоступна"},
		{"path — файл, а не директория", yamlTarget{Path: file, MaxSizeGB: ptr(int64(1))}, "директория недоступна"},
		{"не задан ни один режим", yamlTarget{Path: dir}, "не задано ни одно"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{tt.target}})
			if len(targets) != 0 {
				t.Errorf("target должен быть пропущен, got %v", targets)
			}
			if len(errs) != 1 || !hasErrContaining(errs, tt.wantErr) {
				t.Errorf("errs = %v, want одну ошибку с %q", errs, tt.wantErr)
			}
			if !hasErrContaining(errs, "targets[0]") {
				t.Errorf("ошибка должна содержать номер target: %v", errs)
			}
		})
	}
}

// Битый target пропускается, но остальные обрабатываются.
func TestBuildTargets_BrokenTargetDoesNotStopOthers(t *testing.T) {
	good1 := t.TempDir()
	good2 := t.TempDir()

	cfg := &yamlConfig{Targets: []yamlTarget{
		{Path: good1, MaxSizeGB: ptr(int64(1))},
		{Path: filepath.Join(good1, "missing"), MaxSizeGB: ptr(int64(1))},
		{Path: good2, MaxAge: "7d"},
		{Path: ""},
	}}

	targets, errs := buildTargets(cfg)

	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}
	if targets[0].Path != filepath.Clean(good1) || targets[1].Path != filepath.Clean(good2) {
		t.Errorf("пути targets: %q, %q", targets[0].Path, targets[1].Path)
	}
	if len(errs) != 2 {
		t.Fatalf("len(errs) = %d, want 2: %v", len(errs), errs)
	}
	if !hasErrContaining(errs, "targets[1]") || !hasErrContaining(errs, "targets[3]") {
		t.Errorf("ошибки должны ссылаться на targets[1] и targets[3]: %v", errs)
	}
}

func TestBuildTargets_NoTargets(t *testing.T) {
	targets, errs := buildTargets(&yamlConfig{})
	if len(targets) != 0 || len(errs) != 0 {
		t.Errorf("targets=%v errs=%v, want пусто", targets, errs)
	}
}

func TestBuildTargets_TimeWindow(t *testing.T) {
	dir := t.TempDir()
	base := yamlTarget{Path: dir, MaxSizeGB: ptr(int64(1))}

	withWindow := func(w string) yamlTarget {
		tg := base
		tg.TimeWindow = w
		return tg
	}

	t.Run("по умолчанию 5m", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{base}})
		if len(errs) != 0 || len(targets) != 1 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		if got := targets[0].Config.TimeWindow; got != 5*time.Minute {
			t.Errorf("TimeWindow = %v, want 5m", got)
		}
	})

	t.Run("глобальное значение", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{TimeWindow: "15m", Targets: []yamlTarget{base}})
		if len(errs) != 0 || len(targets) != 1 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		if got := targets[0].Config.TimeWindow; got != 15*time.Minute {
			t.Errorf("TimeWindow = %v, want 15m", got)
		}
	})

	t.Run("глобальное значение в днях", func(t *testing.T) {
		targets, _ := buildTargets(&yamlConfig{TimeWindow: "1d", Targets: []yamlTarget{base}})
		if len(targets) != 1 {
			t.Fatalf("targets=%v", targets)
		}
		if got := targets[0].Config.TimeWindow; got != 24*time.Hour {
			t.Errorf("TimeWindow = %v, want 24h", got)
		}
	})

	t.Run("target переопределяет глобальное", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{
			TimeWindow: "15m",
			Targets:    []yamlTarget{withWindow("1h"), base},
		})
		if len(errs) != 0 || len(targets) != 2 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		if got := targets[0].Config.TimeWindow; got != time.Hour {
			t.Errorf("target[0] TimeWindow = %v, want 1h", got)
		}
		if got := targets[1].Config.TimeWindow; got != 15*time.Minute {
			t.Errorf("target[1] TimeWindow = %v, want глобальное 15m", got)
		}
	})

	// Текущее поведение: некорректный глобальный time_window молча игнорируется
	t.Run("некорректное глобальное значение — используется 5m", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{TimeWindow: "garbage", Targets: []yamlTarget{base}})
		if len(errs) != 0 || len(targets) != 1 {
			t.Fatalf("targets=%v errs=%v", targets, errs)
		}
		if got := targets[0].Config.TimeWindow; got != 5*time.Minute {
			t.Errorf("TimeWindow = %v, want 5m", got)
		}
	})

	// Текущее поведение: ошибка репортится, но target не пропускается
	// и использует глобальное значение
	t.Run("некорректное значение target — ошибка, откат на глобальное", func(t *testing.T) {
		targets, errs := buildTargets(&yamlConfig{
			TimeWindow: "15m",
			Targets:    []yamlTarget{withWindow("garbage")},
		})
		if len(errs) != 1 || !hasErrContaining(errs, "time_window") {
			t.Fatalf("errs = %v, want одну ошибку про time_window", errs)
		}
		if len(targets) != 1 {
			t.Fatalf("len(targets) = %d, want 1", len(targets))
		}
		if got := targets[0].Config.TimeWindow; got != 15*time.Minute {
			t.Errorf("TimeWindow = %v, want глобальное 15m", got)
		}
	})
}

func TestBuildTargets_RemoveEmptyDirs(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		val  *bool
		want bool
	}{
		{"не задано — true по умолчанию", nil, true},
		{"явно true", ptr(true), true},
		{"явно false", ptr(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
				Path: dir, MaxSizeGB: ptr(int64(1)), RemoveEmptyDirs: tt.val,
			}}})
			if len(errs) != 0 || len(targets) != 1 {
				t.Fatalf("targets=%v errs=%v", targets, errs)
			}
			if got := targets[0].Config.RemoveEmptyDirs; got != tt.want {
				t.Errorf("RemoveEmptyDirs = %v, want %v", got, tt.want)
			}
		})
	}
}

// Явно заданный ноль отличается от "не задано": target с max_usage_percent: 0
// считается заданным режимом и не отбрасывается.
func TestBuildTargets_ExplicitZeroCountsAsSet(t *testing.T) {
	dir := t.TempDir()

	targets, errs := buildTargets(&yamlConfig{Targets: []yamlTarget{{
		Path: dir, MaxUsagePercent: ptr(0.0),
	}}})
	if len(errs) != 0 || len(targets) != 1 {
		t.Fatalf("targets=%v errs=%v", targets, errs)
	}
	if p := targets[0].Config.MaxUsagePercent; p == nil || *p != 0 {
		t.Errorf("MaxUsagePercent = %v, want указатель на 0", p)
	}
}

// Сквозной тест: YAML-файл на диске → loadYAMLConfig → buildTargets
func TestLoadAndBuild_EndToEnd(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	path := writeYAML(t, "concurrency: 2\ntime_window: 30m\ntargets:\n"+
		"  - path: "+dirA+"\n    max_size_gb: 5\n    exclude_extensions: [log]\n"+
		"  - path: "+dirB+"\n    max_age: 2w\n    age_trigger_file_count: 100\n    time_window: 1d\n")

	cfg, err := loadYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	targets, errs := buildTargets(cfg)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}

	a, b := targets[0].Config, targets[1].Config
	if a.MaxSize == nil || *a.MaxSize != 5<<30 || a.TimeWindow != 30*time.Minute || a.Concurrency != 2 {
		t.Errorf("target A: %+v", a)
	}
	if b.MaxAge == nil || *b.MaxAge != 14*24*time.Hour {
		t.Errorf("target B MaxAge = %v, want 336h", b.MaxAge)
	}
	if b.MinTriggerFileCount == nil || *b.MinTriggerFileCount != 100 || b.TimeWindow != 24*time.Hour {
		t.Errorf("target B: %+v", b)
	}
}
