package provision

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// El panel corre en una imagen distroless (sin `tar`, `gzip` ni `mysqldump`
// disponibles como binarios locales), así que los backups de archivos se
// hacen con los paquetes de la librería estándar directamente sobre el
// filesystem que el panel ya tiene montado, y el dump de cada base de datos
// se ejecuta dentro del propio contenedor de MariaDB vía la API de Docker.

func (s *Service) accountBackupsDir(systemUser string) string {
	return filepath.Join(s.cfg.SitesRoot, systemUser, "backups")
}

// BackupAccountFiles empaqueta la carpeta "sites" de la cuenta (todo el
// código de sus sitios) en un .tar.gz dentro de su propia carpeta backups/.
func (s *Service) BackupAccountFiles(ctx context.Context, accountID uuid.UUID) (string, error) {
	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	srcRoot := filepath.Join(s.cfg.SitesRoot, acct.SystemUser, "sites")
	if _, err := os.Stat(srcRoot); os.IsNotExist(err) {
		return "", fmt.Errorf("la cuenta no tiene archivos que respaldar")
	}

	dstDir := s.accountBackupsDir(acct.SystemUser)
	if err := os.MkdirAll(dstDir, 0o750); err != nil {
		return "", err
	}
	name := fmt.Sprintf("files-%s.tar.gz", time.Now().UTC().Format("20060102T150405Z"))
	dst := filepath.Join(dstDir, name)

	if err := tarGzDir(srcRoot, dst); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

func tarGzDir(srcRoot, dstFile string) error {
	out, err := os.OpenFile(dstFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}

// BackupAccountDatabases hace un mysqldump de cada base de la cuenta,
// corriendo mysqldump DENTRO del contenedor de MariaDB (el panel no tiene el
// binario disponible localmente) y escribe la salida comprimida en backups/.
func (s *Service) BackupAccountDatabases(ctx context.Context, accountID uuid.UUID) ([]string, error) {
	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	dbs, err := s.st.ListDatabases(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if len(dbs) == 0 {
		return nil, nil
	}
	dstDir := s.accountBackupsDir(acct.SystemUser)
	if err := os.MkdirAll(dstDir, 0o750); err != nil {
		return nil, err
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	var paths []string
	for _, db := range dbs {
		// El dump se escribe a un archivo DENTRO del contenedor de MariaDB y
		// se trae con ReadFile (bytes crudos): capturar gzip binario por la
		// salida de Exec línea por línea lo corrompería.
		tmpPath := "/tmp/gocp-dump-" + db.DBName + ".sql.gz"
		cmd := fmt.Sprintf(
			`mysqldump -uroot -p"$MARIADB_ROOT_PASSWORD" --single-transaction --routines --events %s | gzip -9 > %s`,
			shellQuote(db.DBName), shellQuote(tmpPath))
		exit, out, err := s.docker.ExecAs(ctx, s.cfg.MySQLContainerName,
			[]string{"sh", "-c", cmd}, "", nil)
		if err != nil {
			return paths, fmt.Errorf("respaldando %s: %w", db.DBName, err)
		}
		if exit != 0 {
			return paths, fmt.Errorf("mysqldump de %s terminó con código %d: %s", db.DBName, exit, out)
		}
		data, err := s.docker.ReadFile(ctx, s.cfg.MySQLContainerName, tmpPath)
		_, _, _ = s.docker.ExecAs(ctx, s.cfg.MySQLContainerName, []string{"rm", "-f", tmpPath}, "", nil)
		if err != nil {
			return paths, fmt.Errorf("recuperando el dump de %s: %w", db.DBName, err)
		}
		dst := filepath.Join(dstDir, fmt.Sprintf("db-%s-%s.sql.gz", db.DBName, stamp))
		if err := os.WriteFile(dst, data, 0o640); err != nil {
			return paths, err
		}
		paths = append(paths, dst)
	}
	return paths, nil
}

// PruneOldBackups borra de backups/ todo lo más viejo que la retención
// configurada (GOCP_BACKUP_RETENTION_DAYS).
func (s *Service) PruneOldBackups(ctx context.Context, accountID uuid.UUID) error {
	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	dir := s.accountBackupsDir(acct.SystemUser)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -s.cfg.BackupRetentionDays)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// ListBackups devuelve los archivos de backups/ ordenados del más reciente
// al más antiguo.
type BackupFile struct {
	Name    string    `json:"name"`
	SizeB   int64     `json:"size_b"`
	ModTime time.Time `json:"mod_time"`
}

func (s *Service) ListBackups(ctx context.Context, accountID uuid.UUID) ([]BackupFile, error) {
	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.accountBackupsDir(acct.SystemUser))
	if os.IsNotExist(err) {
		return []BackupFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]BackupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: e.Name(), SizeB: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// BackupPath resuelve un nombre de archivo de backup a su ruta absoluta,
// rechazando cualquier intento de escapar la carpeta backups/ de la cuenta.
func (s *Service) BackupPath(ctx context.Context, accountID uuid.UUID, name string) (string, error) {
	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("nombre de backup inválido")
	}
	return filepath.Join(s.accountBackupsDir(acct.SystemUser), name), nil
}

// RunAccountBackup hace un backup completo (archivos + bases de datos) de
// una cuenta y purga los backups vencidos; lo usan tanto el disparo manual
// desde la API como el bucle diario del worker.
func (s *Service) RunAccountBackup(ctx context.Context, accountID uuid.UUID) error {
	if _, err := s.BackupAccountFiles(ctx, accountID); err != nil {
		return fmt.Errorf("respaldando archivos: %w", err)
	}
	if _, err := s.BackupAccountDatabases(ctx, accountID); err != nil {
		return fmt.Errorf("respaldando bases de datos: %w", err)
	}
	return s.PruneOldBackups(ctx, accountID)
}
