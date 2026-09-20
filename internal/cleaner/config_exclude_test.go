package cleaner

import "testing"

// newExcludeConfig строит конфиг и прогоняет setDefaults(), который
// нормализует ExcludeExtensions / ExcludeDirs во внутренние множества.
func newExcludeConfig(exts, dirs []string) *CleaningConfig {
	c := &CleaningConfig{ExcludeExtensions: exts, ExcludeDirs: dirs}
	c.setDefaults()
	return c
}

func TestIsExcluded(t *testing.T) {
	tests := []struct {
		name string
		exts []string
		path string
		want bool
	}{
		{"нет исключений", nil, "/data/a.log", false},
		{"пустой список", []string{}, "/data/a.log", false},
		{"расширение с точкой", []string{".log"}, "/data/a.log", true},
		{"расширение без точки", []string{"log"}, "/data/a.log", true},
		{"регистр в конфиге", []string{"LOG"}, "/data/a.log", true},
		{"регистр в пути", []string{"log"}, "/data/A.LOG", true},
		{"пробелы вокруг расширения в конфиге", []string{"  .log  "}, "/data/a.log", true},
		{"второе из нескольких расширений", []string{"tmp", "log", "bak"}, "/data/a.log", true},
		{"другое расширение", []string{"log"}, "/data/a.txt", false},
		{"файл без расширения", []string{"log"}, "/data/Makefile", false},
		{"расширение — не подстрока", []string{"log"}, "/data/a.logx", false},
		{"имя файла содержит 'log', но не расширение", []string{"log"}, "/data/log", false},
		{"точка в имени директории — не расширение", []string{"d"}, "/data/a.d/file", false},
		{"только пустые записи в конфиге", []string{"", "   "}, "/data/a.log", false},
		{"пустые записи не мешают остальным", []string{"", "log"}, "/data/a.log", true},
		// Поведение filepath.Ext: берётся последний суффикс после точки.
		{"составное расширение: gz совпадает с a.tar.gz", []string{"gz"}, "/data/a.tar.gz", true},
		{"составное расширение: tar.gz НЕ совпадает с a.tar.gz", []string{"tar.gz"}, "/data/a.tar.gz", false},
		{"dotfile: .env считается расширением", []string{"env"}, "/data/.env", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newExcludeConfig(tt.exts, nil)
			if got := c.isExcluded(tt.path); got != tt.want {
				t.Errorf("isExcluded(%q) с ExcludeExtensions=%q = %v, want %v", tt.path, tt.exts, got, tt.want)
			}
		})
	}
}

func TestIsExcludedDir(t *testing.T) {
	tests := []struct {
		name string
		dirs []string
		dir  string
		want bool
	}{
		{"нет исключений", nil, "cache", false},
		{"пустой список", []string{}, "cache", false},
		{"точное совпадение", []string{"cache"}, "cache", true},
		{"одно из нескольких", []string{".git", "cache", "tmp"}, "cache", true},
		{"другое имя", []string{"cache"}, "data", false},
		{"префикс — не совпадение", []string{"cache"}, "cache2", false},
		{"суффикс — не совпадение", []string{"cache"}, "my-cache", false},
		{"имя, начинающееся с точки", []string{".git"}, ".git", true},
		{"пробелы вокруг имени в конфиге", []string{"  cache  "}, "cache", true},
		{"регистр в конфиге приводится к нижнему", []string{"Cache"}, "cache", true},
		{"только пустые записи в конфиге", []string{"", "   "}, "cache", false},
		{"пустое имя директории", []string{"cache"}, "", false},
		{"полный путь вместо имени не совпадает", []string{"cache"}, "/data/cache", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newExcludeConfig(nil, tt.dirs)
			if got := c.IsExcludedDir(tt.dir); got != tt.want {
				t.Errorf("IsExcludedDir(%q) с ExcludeDirs=%q = %v, want %v", tt.dir, tt.dirs, got, tt.want)
			}
		})
	}
}

func TestIsExcludedDir_CaseInsensitiveName(t *testing.T) {
	tests := []struct {
		name string
		dirs []string
		dir  string
	}{
		{"имя директории в верхнем регистре", []string{"cache"}, "CACHE"},
		{"имя с заглавной буквой", []string{"cache"}, "Cache"},
		{"оба с заглавной буквой", []string{"Cache"}, "Cache"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newExcludeConfig(nil, tt.dirs)
			if !c.IsExcludedDir(tt.dir) {
				t.Errorf("IsExcludedDir(%q) с ExcludeDirs=%q = false, want true", tt.dir, tt.dirs)
			}
		})
	}
}
