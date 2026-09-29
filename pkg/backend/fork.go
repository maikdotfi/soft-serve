package backend

import (
	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/ci"
)

// forkServices holds the services this fork adds to the backend. It is
// embedded in Backend so the upstream struct only carries a single line.
type forkServices struct {
	backup *backup.BackupService
	ci     *ci.Service
}

// SetBackupService sets the backup service on the backend.
func (b *Backend) SetBackupService(svc *backup.BackupService) {
	b.backup = svc
}

// BackupService returns the backup service.
func (b *Backend) BackupService() *backup.BackupService {
	return b.backup
}

// SetCIService sets the CI service on the backend. Until this is
// called CIService() returns nil and the push and webhook hooks no-op
// the CI integration.
func (b *Backend) SetCIService(svc *ci.Service) {
	b.ci = svc
}

// CIService returns the CI service, or nil if not configured.
func (b *Backend) CIService() *ci.Service {
	return b.ci
}
