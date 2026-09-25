package web

// Цвет строки состояния браузера (<meta name="theme-color">) для светлой и
// тёмной темы. Совпадает с --color-bg из static/css/tokens.css — это
// проверяет тест, чтобы цвета не разошлись при правке токенов.
const (
	ThemeColorLight = "#f6f7f9"
	ThemeColorDark  = "#0f1115"
)
