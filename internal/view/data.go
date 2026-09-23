package view

import "net/http"

// Data — набор переменных, подставляемых в шаблон.
// Ключи доступны в шаблоне как {{ .Title }}, {{ .Users }} и т.д.
type Data map[string]any

// NewData создаёт набор переменных с полями, нужными почти каждой странице.
func NewData(r *http.Request, title string) Data {
	return Data{
		"Title": title,
		"Path":  r.URL.Path,
		"Query": r.URL.Query(),
	}
}

// With добавляет переменную и возвращает тот же набор — для цепочек вызовов:
//
//	view.NewData(r, "Пользователи").With("Users", users).With("Total", total)
func (d Data) With(key string, value any) Data {
	d[key] = value
	return d
}

// Merge добавляет сразу несколько переменных.
func (d Data) Merge(values map[string]any) Data {
	for k, v := range values {
		d[k] = v
	}
	return d
}
