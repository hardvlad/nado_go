// Package httpx — общий транспортный слой: ошибки, ответы, middleware.
package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// Error — ошибка, которую можно безопасно показать клиенту.
// Внутренние детали остаются в поле err и попадают только в лог.
type Error struct {
	Status  int            // HTTP-статус ответа
	Code    string         // машиночитаемый код, напр. "validation_failed"
	Message string         // сообщение для пользователя
	Fields  map[string]any // детали (ошибки валидации по полям)
	err     error          // исходная ошибка, наружу не отдаётся
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.err)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.err }

// WithCause прикрепляет исходную ошибку для логирования.
func (e *Error) WithCause(err error) *Error {
	e.err = err
	return e
}

// WithFields прикрепляет детали (например, ошибки валидации).
func (e *Error) WithFields(fields map[string]any) *Error {
	e.Fields = fields
	return e
}

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Конструкторы типовых ошибок.
func ErrBadRequest(message string) *Error {
	return NewError(http.StatusBadRequest, "bad_request", message)
}

func ErrValidation(message string) *Error {
	return NewError(http.StatusUnprocessableEntity, "validation_failed", message)
}

func ErrUnauthorized(message string) *Error {
	return NewError(http.StatusUnauthorized, "unauthorized", message)
}

func ErrForbidden(message string) *Error {
	return NewError(http.StatusForbidden, "forbidden", message)
}

func ErrNotFound(message string) *Error {
	return NewError(http.StatusNotFound, "not_found", message)
}

func ErrConflict(message string) *Error {
	return NewError(http.StatusConflict, "conflict", message)
}

func ErrInternal(cause error) *Error {
	return NewError(http.StatusInternalServerError, "internal_error",
		"Внутренняя ошибка сервера").WithCause(cause)
}

// AsError извлекает *Error из цепочки ошибок; если ошибка не наша,
// считаем её внутренней и не раскрываем клиенту подробности.
func AsError(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return ErrInternal(err)
}
