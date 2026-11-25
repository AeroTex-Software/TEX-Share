package app

import (
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/config"
	"github.com/AeroTex-Software/TEX-Share/internal/db"
	"github.com/AeroTex-Software/TEX-Share/internal/secrets"
	"github.com/AeroTex-Software/TEX-Share/internal/settings"
	"github.com/AeroTex-Software/TEX-Share/internal/storage"
)

// App wires all services and owns the database and background jobs.
type App struct {
	cfg      *config.Config
	db       *sql.DB
	secrets  *secrets.Manager
	settings *settings.Store
	store    *storage.Manager
	log      *slog.Logger

	startedAt time.Time
	dummyHash string

	loginLimiter *rateLimiter
	apiLimiter   *rateLimiter
	gates        *concurrencyGate
	zipGate      *concurrencyGate

	activeUploads   int64
	activeDownloads int64
	activeZips      int64
	errorCount      int64
	uploadQueue     int64

	auditMu       sync.Mutex
	lastAuditHash string

	uploadLocks sync.Map // upload ID -> *sync.Mutex

	shutdownOnce sync.Once
	stopCh       chan struct{}
	jobWG        sync.WaitGroup

	backupMu  sync.Mutex
	restoreMu sync.RWMutex
	restoring bool

	// Optional integrations, initialised in New.
	clamav     *clamAV
	cloudflare *cloudflareClient
	tunnel     *cloudflaredTunnel

	// domain cache for Host validation
	hostMu      sync.RWMutex
	publicHosts map[string]bool
	adminHosts  map[string]bool

	notifyMu sync.Mutex
}

// New opens all resources. The caller must call Close.
func New(cfg *config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	database, err := db.Open(cfg.DataDir + "/db/texshare.db")
	if err != nil {
		return nil, err
	}
	sec, err := secrets.LoadOrCreate(cfg.DataDir)
	if err != nil {
		database.Close()
		return nil, err
	}
	set, err := settings.New(database)
	if err != nil {
		database.Close()
		return nil, err
	}
	a := &App{
		cfg:          cfg,
		db:           database,
		secrets:      sec,
		settings:     set,
		store:        storage.New(cfg.DataDir),
		log:          logger,
		startedAt:    time.Now(),
		loginLimiter: newRateLimiter(),
		apiLimiter:   newRateLimiter(),
		gates:        newConcurrencyGate(),
		zipGate:      newConcurrencyGate(),
		stopCh:       make(chan struct{}),
		publicHosts:  map[string]bool{},
		adminHosts:   map[string]bool{},
	}
	if err := a.initAudit(); err != nil {
		database.Close()
		return nil, err
	}
	a.dummyHash, _ = hashDummy()
	a.refreshHostCache()
	if err := a.initClamAV(); err != nil {
		logger.Warn("clamav init failed", "error", err)
	}
	if err := a.initCloudflare(); err != nil {
		logger.Warn("cloudflare init failed", "error", err)
	}
	a.startJobs()
	return a, nil
}

// DB exposes the database for tests and backup tooling.
func (a *App) DB() *sql.DB { return a.db }

// Settings exposes the settings store.
func (a *App) Settings() *settings.Store { return a.settings }

// Config exposes process configuration.
func (a *App) Config() *config.Config { return a.cfg }

// Logger exposes the structured logger.
func (a *App) Logger() *slog.Logger { return a.log }

// StartedAt returns process start time.
func (a *App) StartedAt() time.Time { return a.startedAt }

// Close shuts down background work and child processes.
func (a *App) Close() error {
	var err error
	a.shutdownOnce.Do(func() {
		close(a.stopCh)
		a.jobWG.Wait()
		if a.clamav != nil {
			a.clamav.Stop()
		}
		if a.tunnel != nil {
			a.tunnel.Stop()
		}
		err = a.db.Close()
	})
	return err
}

// initAudit loads the last audit hash. An empty chain is valid on first start.
func (a *App) initAudit() error {
	err := a.db.QueryRow(`SELECT entry_hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&a.lastAuditHash)
	if errors.Is(err, sql.ErrNoRows) {
		a.lastAuditHash = ""
		return nil
	}
	return err
}

func (a *App) initClamAV() error {
	a.clamav = newClamAV(a)
	return a.clamav.Start()
}

func (a *App) initCloudflare() error {
	a.cloudflare = newCloudflare(a)
	a.tunnel = newTunnel(a)
	a.tunnel.Start()
	return nil
}

// clamavEnabled reports whether antivirus scanning is currently switched on.
func (a *App) clamavEnabled() bool {
	return a.clamav != nil && a.clamav.enabled()
}

// queueScan schedules an asynchronous scan for a staged upload.
func (a *App) queueScan(uploadID string) {
	if a.clamav != nil && a.clamavEnabled() {
		a.clamav.queueScan(uploadID)
	}
}
