// Package web содержит шаблоны страниц и статические файлы.
//
// В production они вкомпилированы в бинарник (embed.FS) — деплой сводится к
// копированию одного файла. В разработке читаются с диска, чтобы правка
// шаблона была видна без пересборки.
package web

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:static
var staticFS embed.FS

// Пути на диске относительно корня репозитория (режим разработки).
const (
	templatesDir = "web/templates"
	staticDir    = "web/static"
)

// Templates возвращает файловую систему с шаблонами.
// fromDisk=true — читать с диска (разработка), иначе — из бинарника.
func Templates(fromDisk bool) (fs.FS, error) {
	if fromDisk {
		return diskFS(templatesDir)
	}
	return fs.Sub(templatesFS, "templates")
}

// Static возвращает файловую систему со статикой.
func Static(fromDisk bool) (fs.FS, error) {
	if fromDisk {
		return diskFS(staticDir)
	}
	return fs.Sub(staticFS, "static")
}

// diskFS проверяет существование каталога до создания os.DirFS: иначе
// отсутствие каталога проявится только при первом запросе как 404.
func diskFS(dir string) (fs.FS, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}
	return os.DirFS(abs), nil
}
