package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mssql "github.com/microsoft/go-mssqldb"
)

// Доменно-нейтральные ошибки уровня хранилища. Сервисы сравнивают через
// errors.Is и не зависят от кодов конкретной СУБД.
var (
	ErrNotFound = errors.New("database: запись не найдена")
	ErrConflict = errors.New("database: нарушение уникальности")
)

// Коды ошибок SQL Server, которые имеет смысл различать в бизнес-логике.
const (
	codeUniqueIndexViolation = 2601
	codeUniqueConstraint     = 2627
	codeForeignKeyViolation  = 547
)

// TxFunc — единица работы внутри транзакции.
type TxFunc func(ctx context.Context, tx *sql.Tx) error

// WithTx выполняет fn в транзакции: коммитит при успехе и откатывает при
// ошибке или панике. Панику пробрасывает дальше, предварительно сняв
// блокировки откатом — иначе соединение вернётся в пул с открытой транзакцией.
func (d *DB) WithTx(ctx context.Context, opts *sql.TxOptions, fn TxFunc) (err error) {
	tx, err := d.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("database: не удалось начать транзакцию: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("database: откат транзакции: %w", rbErr))
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			err = fmt.Errorf("database: коммит транзакции: %w", commitErr)
		}
	}()

	return fn(ctx, tx)
}

// MapError приводит ошибки драйвера к доменно-нейтральным значениям.
// Репозитории возвращают наружу только их, не утекая деталями SQL Server.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	var mssqlErr mssql.Error
	if errors.As(err, &mssqlErr) {
		switch mssqlErr.Number {
		case codeUniqueIndexViolation, codeUniqueConstraint:
			return fmt.Errorf("%w: %s", ErrConflict, mssqlErr.Message)
		case codeForeignKeyViolation:
			return fmt.Errorf("%w: нарушение внешнего ключа", ErrConflict)
		}
	}
	return err
}
