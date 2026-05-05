// Package snapshot provides an adapter for creating and restoring server snapshots.
// Per AGENTS.md: this is an adapter that implements the backup.SnapshotDataProvider port.
// It captures the database, config, and SSH host keys for full server restore.
package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/store"
)

// ServerSnapshotProvider implements backup.SnapshotDataProvider using the
// local database and filesystem.
type ServerSnapshotProvider struct {
	dataPath string
	db       *db.DB
	store    store.Store
	dbPath   string // path to the SQLite database file
}

// NewServerSnapshotProvider creates a new ServerSnapshotProvider.
func NewServerSnapshotProvider(dataPath string, database *db.DB, dbPath string, s store.Store) *ServerSnapshotProvider {
	return &ServerSnapshotProvider{
		dataPath: dataPath,
		db:       database,
		store:    s,
		dbPath:   dbPath,
	}
}

// CreateSnapshotData creates a snapshot of server data (DB, config, SSH host keys, public keys).
// Returns the snapshot content as bytes.
func (s *ServerSnapshotProvider) CreateSnapshotData(ctx context.Context) ([]byte, error) {
	files := make(map[string][]byte)

	// Dump the database
	dbDump, err := s.dumpDatabase(ctx)
	if err != nil {
		return nil, fmt.Errorf("dumping database: %w", err)
	}
	files["database.dump"] = dbDump

	// Read config file
	configData, err := os.ReadFile(filepath.Join(s.dataPath, "config.yaml"))
	if err != nil {
		configData = []byte{} // Config may not exist if defaults are used
	}
	files["config.yaml"] = configData

	// Read SSH host keys
	sshKey, err := os.ReadFile(filepath.Join(s.dataPath, "ssh", "soft_serve_host_ed25519"))
	if err != nil {
		sshKey = []byte{}
	}
	files["ssh/soft_serve_host_ed25519"] = sshKey

	sshPubKey, err := os.ReadFile(filepath.Join(s.dataPath, "ssh", "soft_serve_host_ed25519.pub"))
	if err != nil {
		sshPubKey = []byte{}
	}
	files["ssh/soft_serve_host_ed25519.pub"] = sshPubKey

	// Build archive
	archive, err := buildArchive(files)
	if err != nil {
		return nil, fmt.Errorf("building snapshot archive: %w", err)
	}

	return archive, nil
}

// RestoreSnapshotData restores server data from a snapshot archive.
func (s *ServerSnapshotProvider) RestoreSnapshotData(ctx context.Context, content []byte) error {
	files, err := extractArchive(content)
	if err != nil {
		return fmt.Errorf("extracting snapshot archive: %w", err)
	}

	// Restore config
	if configData, ok := files["config.yaml"]; ok && len(configData) > 0 {
		configPath := filepath.Join(s.dataPath, "config.yaml")
		if err := os.WriteFile(configPath, configData, 0o644); err != nil {
			return fmt.Errorf("restoring config: %w", err)
		}
	}

	// Restore SSH host keys
	if sshKey, ok := files["ssh/soft_serve_host_ed25519"]; ok && len(sshKey) > 0 {
		sshDir := filepath.Join(s.dataPath, "ssh")
		if err := os.MkdirAll(sshDir, 0o700); err != nil {
			return fmt.Errorf("creating SSH directory: %w", err)
		}
		if err := os.WriteFile(filepath.Join(sshDir, "soft_serve_host_ed25519"), sshKey, 0o600); err != nil {
			return fmt.Errorf("restoring SSH host key: %w", err)
		}
	}
	if sshPubKey, ok := files["ssh/soft_serve_host_ed25519.pub"]; ok && len(sshPubKey) > 0 {
		sshDir := filepath.Join(s.dataPath, "ssh")
		if err := os.MkdirAll(sshDir, 0o700); err != nil {
			return fmt.Errorf("creating SSH directory: %w", err)
		}
		if err := os.WriteFile(filepath.Join(sshDir, "soft_serve_host_ed25519.pub"), sshPubKey, 0o644); err != nil {
			return fmt.Errorf("restoring SSH host public key: %w", err)
		}
	}

	// Restore database
	if dbDump, ok := files["database.dump"]; ok && len(dbDump) > 0 {
		if err := s.restoreDatabase(ctx, dbDump); err != nil {
			return fmt.Errorf("restoring database: %w", err)
		}
	}

	return nil
}

// dumpDatabase dumps the database content.
func (s *ServerSnapshotProvider) dumpDatabase(_ context.Context) ([]byte, error) {
	driverName := s.db.DriverName()
	switch driverName {
	case "sqlite3", "sqlite":
		if s.dbPath == "" {
			return nil, fmt.Errorf("cannot determine database path")
		}
		data, err := os.ReadFile(s.dbPath)
		if err != nil {
			return nil, fmt.Errorf("reading SQLite database file: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported database driver for snapshot: %s", driverName)
	}
}

// restoreDatabase restores the database from a dump.
func (s *ServerSnapshotProvider) restoreDatabase(_ context.Context, dump []byte) error {
	driverName := s.db.DriverName()
	switch driverName {
	case "sqlite3", "sqlite":
		if s.dbPath == "" {
			return fmt.Errorf("cannot determine database path")
		}
		tmpPath := s.dbPath + ".restoring"
		if err := os.WriteFile(tmpPath, dump, 0o644); err != nil {
			return fmt.Errorf("writing restored database: %w", err)
		}
		if err := os.Rename(tmpPath, s.dbPath); err != nil {
			return fmt.Errorf("swapping restored database: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported database driver for restore: %s", driverName)
	}
}

// buildArchive creates a simple binary archive from a map of filenames to content.
func buildArchive(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer

	// Write number of files
	count := uint16(len(files))
	if err := writeUint16(&buf, count); err != nil {
		return nil, err
	}

	for name, data := range files {
		if err := writeUint16(&buf, uint16(len(name))); err != nil {
			return nil, err
		}
		if _, err := buf.WriteString(name); err != nil {
			return nil, err
		}
		if err := writeUint32(&buf, uint32(len(data))); err != nil {
			return nil, err
		}
		if _, err := buf.Write(data); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// extractArchive extracts files from a simple binary archive.
func extractArchive(data []byte) (map[string][]byte, error) {
	reader := bytes.NewReader(data)
	files := make(map[string][]byte)

	count, err := readUint16(reader)
	if err != nil {
		return nil, err
	}

	for i := 0; i < int(count); i++ {
		nameLen, err := readUint16(reader)
		if err != nil {
			return nil, err
		}
		name := make([]byte, nameLen)
		if _, err := io.ReadFull(reader, name); err != nil {
			return nil, err
		}

		dataLen, err := readUint32(reader)
		if err != nil {
			return nil, err
		}
		fileData := make([]byte, dataLen)
		if _, err := io.ReadFull(reader, fileData); err != nil {
			return nil, err
		}

		files[string(name)] = fileData
	}

	return files, nil
}

func writeUint16(w io.Writer, v uint16) error {
	b := []byte{byte(v >> 8), byte(v)}
	_, err := w.Write(b)
	return err
}

func readUint16(r io.Reader) (uint16, error) {
	b := make([]byte, 2)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return uint16(b[0])<<8 | uint16(b[1]), nil
}

func writeUint32(w io.Writer, v uint32) error {
	b := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	_, err := w.Write(b)
	return err
}

func readUint32(r io.Reader) (uint32, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]), nil
}