package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// maxBodyBytes ограничивает размер JSON-тела — защита от «раздувания» памяти
// запросом на сотни мегабайт.
const maxBodyBytes = 1 << 20 // 1 MiB

var validate = sync.OnceValue(func() *validator.Validate {
	return validator.New(validator.WithRequiredStructEnabled())
})

// DecodeJSON читает и валидирует тело запроса в dst.
// Возвращает *Error, готовый к отдаче клиенту.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return NewError(http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Ожидается Content-Type: application/json")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // опечатка в имени поля не должна тихо игнорироваться

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	// В теле должен быть ровно один JSON-объект.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrBadRequest("Тело запроса должно содержать один JSON-объект")
	}

	return Validate(dst)
}

// Validate проверяет структуру по тегам `validate` и собирает ошибки по полям.
func Validate(dst any) error {
	if err := validate().Struct(dst); err != nil {
		var invalid *validator.InvalidValidationError
		if errors.As(err, &invalid) {
			return ErrInternal(err)
		}

		fields := make(map[string]any)
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			for _, fe := range verrs {
				fields[jsonFieldName(fe)] = validationMessage(fe)
			}
		}
		return ErrValidation("Данные запроса не прошли проверку").WithFields(fields)
	}
	return nil
}

func decodeError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var maxBytesErr *http.MaxBytesError

	switch {
	case errors.As(err, &syntaxErr):
		return ErrBadRequest(fmt.Sprintf("Некорректный JSON (позиция %d)", syntaxErr.Offset))
	case errors.As(err, &typeErr):
		return ErrBadRequest(fmt.Sprintf("Поле %q имеет неверный тип", typeErr.Field))
	case errors.As(err, &maxBytesErr):
		return NewError(http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("Тело запроса больше %d байт", maxBodyBytes))
	case errors.Is(err, io.EOF):
		return ErrBadRequest("Пустое тело запроса")
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		return ErrBadRequest(fmt.Sprintf("Неизвестное поле %s", field))
	default:
		return ErrBadRequest("Не удалось разобрать тело запроса")
	}
}

func jsonFieldName(fe validator.FieldError) string {
	name := fe.Field()
	if name == "" {
		return fe.Namespace()
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "обязательное поле"
	case "email":
		return "некорректный email"
	case "min":
		return fmt.Sprintf("минимум %s", fe.Param())
	case "max":
		return fmt.Sprintf("максимум %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("допустимые значения: %s", fe.Param())
	default:
		return fmt.Sprintf("не выполнено правило %q", fe.Tag())
	}
}

// Pagination — разобранные параметры постраничной выдачи.
type Pagination struct {
	Page    int
	PerPage int
}

// Offset — смещение для SQL (OFFSET ... ROWS).
func (p Pagination) Offset() int { return (p.Page - 1) * p.PerPage }

// Limit — размер страницы для SQL (FETCH NEXT ... ROWS ONLY).
func (p Pagination) Limit() int { return p.PerPage }

// ParsePagination читает ?page= и ?per_page=, подставляя безопасные значения.
// Верхняя граница perPage обязательна: иначе клиент может запросить весь стол.
func ParsePagination(r *http.Request, defaultPerPage, maxPerPage int) Pagination {
	p := Pagination{Page: 1, PerPage: defaultPerPage}

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.Page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.PerPage = min(n, maxPerPage)
		}
	}
	return p
}

// QueryString возвращает строковый параметр запроса со значением по умолчанию.
func QueryString(r *http.Request, key, def string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
		return v
	}
	return def
}

// ParseID разбирает числовой параметр пути (chi.URLParam).
func ParseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrBadRequest("Некорректный идентификатор")
	}
	return id, nil
}
