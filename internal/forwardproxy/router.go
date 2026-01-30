package forwardproxy

import (
	"fmt"
	"regexp"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/tokeninjector"
	"go.uber.org/zap"
)

// Router routes requests to the appropriate injector based on host patterns
type Router struct {
	defaultInjector tokeninjector.Injector
	rules           []routeRule
	logger          *zap.Logger
}

type routeRule struct {
	pattern  *regexp.Regexp
	injector tokeninjector.Injector
	mode     string
}

// NewRouter creates a new router
func NewRouter(cfg *config.ForwardProxyConfig, serverCfg *config.ServerConfig, logger *zap.Logger) (*Router, error) {
	// Create default injector
	defaultInjector, err := tokeninjector.NewInjector(&cfg.DefaultMode, serverCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create default injector: %w", err)
	}

	router := &Router{
		defaultInjector: defaultInjector,
		logger:          logger.With(zap.String("component", "forward_proxy_router")),
	}

	// Create injectors for host rules
	for _, rule := range cfg.HostRules {
		// Compile host pattern
		pattern, err := regexp.Compile(rule.HostPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid host pattern %s: %w", rule.HostPattern, err)
		}

		// Create injector for this rule
		injector, err := tokeninjector.NewInjector(&rule.Mode, serverCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create injector for pattern %s: %w", rule.HostPattern, err)
		}

		router.rules = append(router.rules, routeRule{
			pattern:  pattern,
			injector: injector,
			mode:     rule.Mode.Type,
		})
	}

	return router, nil
}

// GetInjector returns the injector for a given host
func (r *Router) GetInjector(host string) (tokeninjector.Injector, string) {
	// Check rules in order
	for _, rule := range r.rules {
		if rule.pattern.MatchString(host) {
			r.logger.Debug("matched host pattern",
				zap.String("host", host),
				zap.String("pattern", rule.pattern.String()),
				zap.String("mode", rule.mode),
			)
			return rule.injector, rule.mode
		}
	}

	// Use default injector
	r.logger.Debug("using default injector",
		zap.String("host", host),
	)
	return r.defaultInjector, "passthrough"
}

// Close closes all injectors
func (r *Router) Close() error {
	var firstErr error

	if err := r.defaultInjector.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	for _, rule := range r.rules {
		if err := rule.injector.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
