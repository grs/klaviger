package tokeninjector

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"go.uber.org/zap"
)

// FileInjector injects tokens from a file
type FileInjector struct {
	path            string
	refreshInterval time.Duration
	token           string
	tokenMu         sync.RWMutex
	watcher         *fsnotify.Watcher
	done            chan struct{}
	logger          *zap.Logger
}

// NewFileInjector creates a new file injector
func NewFileInjector(cfg *config.FileConfig, logger *zap.Logger) (*FileInjector, error) {
	injector := &FileInjector{
		path:            cfg.Path,
		refreshInterval: time.Duration(cfg.RefreshInterval),
		done:            make(chan struct{}),
		logger:          logger.With(zap.String("component", "file_injector")),
	}

	// Load initial token
	if err := injector.loadToken(); err != nil {
		return nil, fmt.Errorf("failed to load initial token: %w", err)
	}

	// Setup file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	injector.watcher = watcher

	// Add file to watcher
	if err := watcher.Add(cfg.Path); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch file: %w", err)
	}

	// Start watch loop
	go injector.watchFile()

	// Start periodic refresh
	go injector.periodicRefresh()

	return injector, nil
}

// loadToken loads the token from file
func (i *FileInjector) loadToken() error {
	data, err := os.ReadFile(i.path)
	if err != nil {
		return fmt.Errorf("failed to read token file: %w", err)
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return fmt.Errorf("token file is empty")
	}

	i.tokenMu.Lock()
	i.token = token
	i.tokenMu.Unlock()

	i.logger.Info("token loaded from file",
		zap.String("path", i.path),
	)

	return nil
}

// watchFile watches for file changes
func (i *FileInjector) watchFile() {
	for {
		select {
		case event, ok := <-i.watcher.Events:
			if !ok {
				return
			}

			// Reload on write or create events
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				i.logger.Debug("token file changed, reloading",
					zap.String("event", event.Op.String()),
				)

				if err := i.loadToken(); err != nil {
					i.logger.Error("failed to reload token", zap.Error(err))
				}
			}

		case err, ok := <-i.watcher.Errors:
			if !ok {
				return
			}
			i.logger.Error("file watcher error", zap.Error(err))

		case <-i.done:
			return
		}
	}
}

// periodicRefresh periodically reloads the token
func (i *FileInjector) periodicRefresh() {
	ticker := time.NewTicker(i.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			i.logger.Debug("periodic token refresh")
			if err := i.loadToken(); err != nil {
				i.logger.Error("failed to refresh token", zap.Error(err))
			}

		case <-i.done:
			return
		}
	}
}

// Inject injects the token from file
func (i *FileInjector) Inject(ctx context.Context, req *http.Request) error {
	start := time.Now()
	result := "success"
	defer func() {
		observability.TokenInjectionDuration.WithLabelValues("file", result).Observe(time.Since(start).Seconds())
	}()

	i.tokenMu.RLock()
	token := i.token
	i.tokenMu.RUnlock()

	if token == "" {
		result = "error"
		return fmt.Errorf("no token available")
	}

	// Set Authorization header
	req.Header.Set("Authorization", "Bearer "+token)

	i.logger.Debug("token injected from file",
		zap.String("host", req.Host),
	)

	return nil
}

// Close closes the injector and stops watching
func (i *FileInjector) Close() error {
	close(i.done)
	if i.watcher != nil {
		return i.watcher.Close()
	}
	return nil
}
