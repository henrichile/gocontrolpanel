package provision

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLManager crea y elimina las bases de datos MySQL/MariaDB de los clientes.
// El panel se conecta con una cuenta administrativa; nunca expone esa conexión
// al usuario final.
type MySQLManager struct {
	db   *sql.DB
	host string
}

var reIdent = regexp.MustCompile(`^[a-z0-9_]{3,48}$`)

// NewMySQLManager devuelve nil si no hay DSN configurado (instalación sin
// servidor MySQL gestionado).
func NewMySQLManager(dsn, host string) (*MySQLManager, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, nil
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("MySQL no responde: %w", err)
	}
	return &MySQLManager{db: db, host: host}, nil
}

func (m *MySQLManager) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

func (m *MySQLManager) Host() string {
	if m == nil {
		return ""
	}
	return m.host
}

// CreateDatabase crea la base de datos y un usuario con permisos solo sobre ella.
// Los identificadores se validan con lista blanca porque no admiten parámetros.
func (m *MySQLManager) CreateDatabase(ctx context.Context, dbName, dbUser, password, charset string) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("no hay servidor MySQL configurado en el panel")
	}
	if !reIdent.MatchString(dbName) {
		return ValidationError{"db_name", "usa 3-48 caracteres [a-z0-9_]"}
	}
	if !reIdent.MatchString(dbUser) {
		return ValidationError{"db_user", "usa 3-48 caracteres [a-z0-9_]"}
	}
	if charset == "" {
		charset = "utf8mb4"
	}
	if !regexp.MustCompile(`^[a-z0-9_]{3,20}$`).MatchString(charset) {
		return ValidationError{"charset", "juego de caracteres no permitido"}
	}

	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET %s", dbName, charset),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY ?", dbUser),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", dbName, dbUser),
		"FLUSH PRIVILEGES",
	}
	for i, stmt := range stmts {
		var err error
		if i == 1 {
			_, err = m.db.ExecContext(ctx, stmt, password)
		} else {
			_, err = m.db.ExecContext(ctx, stmt)
		}
		if err != nil {
			return fmt.Errorf("creando la base de datos: %w", err)
		}
	}
	return nil
}

func (m *MySQLManager) DropDatabase(ctx context.Context, dbName, dbUser string) error {
	if m == nil || m.db == nil {
		return nil
	}
	if !reIdent.MatchString(dbName) || !reIdent.MatchString(dbUser) {
		return ValidationError{"db_name", "identificador inválido"}
	}
	if _, err := m.db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName)); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", dbUser)); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, "FLUSH PRIVILEGES")
	return err
}

func (m *MySQLManager) ChangePassword(ctx context.Context, dbUser, password string) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("no hay servidor MySQL configurado en el panel")
	}
	if !reIdent.MatchString(dbUser) {
		return ValidationError{"db_user", "identificador inválido"}
	}
	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY ?", dbUser), password)
	return err
}

// SizeMB devuelve el tamaño aproximado de una base de datos.
func (m *MySQLManager) SizeMB(ctx context.Context, dbName string) (int64, error) {
	if m == nil || m.db == nil {
		return 0, nil
	}
	var size sql.NullFloat64
	err := m.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(data_length + index_length) / 1024 / 1024, 0)
		FROM information_schema.TABLES WHERE table_schema = ?`, dbName).Scan(&size)
	if err != nil {
		return 0, err
	}
	return int64(size.Float64), nil
}
