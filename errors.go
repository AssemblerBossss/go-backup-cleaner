package gobackupcleaner

import "errors"

var (
	// ErrNoCapacitySpecified возвращается, если не указан ни один лимит ёмкости
	ErrNoCapacitySpecified = errors.New("no capacity limit specified")

	// ErrInvalidConfig возвращается, если конфигурация некорректна
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrDirectoryNotFound возвращается, если целевая директория не найдена
	ErrDirectoryNotFound = errors.New("directory not found")

	// ErrInsufficientSpace возвращается, если не удалось освободить достаточно места
	ErrInsufficientSpace = errors.New("cannot free enough space")
)
