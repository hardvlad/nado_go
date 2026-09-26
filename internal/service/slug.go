package service

import "strings"

// translit — карта кириллицы в латиницу для человекочитаемых slug витрины.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "",
	'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	// казахские буквы
	'ә': "a", 'ғ': "g", 'қ': "k", 'ң': "n", 'ө': "o", 'ұ': "u", 'ү': "u",
	'һ': "h", 'і': "i",
}

// Slugify превращает произвольный текст в slug из латиницы, цифр и дефисов.
// Кириллица транслитерируется. Пустой результат заменяется на "tovar".
func Slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r >= 'а' && r <= 'я', r == 'ё', r == 'ә', r == 'ғ', r == 'қ', r == 'ң', r == 'ө', r == 'ұ', r == 'ү', r == 'һ', r == 'і':
			if t, ok := translit[r]; ok && t != "" {
				b.WriteString(t)
				prevDash = false
			}
		default:
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-")
	}
	if out == "" {
		return "tovar"
	}
	return out
}
