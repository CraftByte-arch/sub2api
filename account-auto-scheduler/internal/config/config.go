package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr           string
	Sub2APIBaseURL       string
	AdminAPIKey          string
	DataFile             string
	PublicURL            string
	UIOrigin             string
	AutoRegisterTab      bool
	TrustProxyHeaders    bool
	MaxConcurrency       int
	AuthCacheTTL         time.Duration
	CredentialKey        string
	UpstreamSyncInterval time.Duration
	NotificationInterval time.Duration
	ShutdownTimeout      time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:           envOrDefault("AUTO_SCHEDULER_LISTEN_ADDR", ":8091"),
		Sub2APIBaseURL:       strings.TrimRight(envOrDefault("SUB2API_BASE_URL", "http://127.0.0.1:8080"), "/"),
		AdminAPIKey:          strings.TrimSpace(os.Getenv("SUB2API_ADMIN_API_KEY")),
		DataFile:             envOrDefault("AUTO_SCHEDULER_DATA_FILE", "./data/state.json"),
		PublicURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("AUTO_SCHEDULER_PUBLIC_URL")), "/"),
		UIOrigin:             strings.TrimRight(strings.TrimSpace(os.Getenv("AUTO_SCHEDULER_UI_ORIGIN")), "/"),
		TrustProxyHeaders:    envBool("AUTO_SCHEDULER_TRUST_PROXY_HEADERS", false),
		MaxConcurrency:       envInt("AUTO_SCHEDULER_MAX_CONCURRENCY", 5),
		AuthCacheTTL:         time.Duration(envInt("AUTO_SCHEDULER_AUTH_CACHE_SECONDS", 30)) * time.Second,
		CredentialKey:        strings.TrimSpace(os.Getenv("AUTO_SCHEDULER_CREDENTIAL_KEY")),
		UpstreamSyncInterval: time.Duration(envInt("AUTO_SCHEDULER_UPSTREAM_SYNC_SECONDS", 600)) * time.Second,
		NotificationInterval: time.Duration(envInt("AUTO_SCHEDULER_NOTIFICATION_INTERVAL_SECONDS", 30)) * time.Second,
		ShutdownTimeout:      time.Duration(envInt("AUTO_SCHEDULER_SHUTDOWN_SECONDS", 15)) * time.Second,
	}
	cfg.AutoRegisterTab = envBool("AUTO_SCHEDULER_AUTO_REGISTER_TAB", cfg.PublicURL != "")

	if cfg.AdminAPIKey == "" {
		return Config{}, fmt.Errorf("SUB2API_ADMIN_API_KEY is required")
	}
	if err := validateHTTPURL("SUB2API_BASE_URL", cfg.Sub2APIBaseURL); err != nil {
		return Config{}, err
	}
	if cfg.PublicURL != "" {
		if err := validateHTTPURL("AUTO_SCHEDULER_PUBLIC_URL", cfg.PublicURL); err != nil {
			return Config{}, err
		}
	}
	if cfg.UIOrigin == "" {
		parsed, err := url.Parse(cfg.Sub2APIBaseURL)
		if err == nil {
			cfg.UIOrigin = parsed.Scheme + "://" + parsed.Host
		}
	} else if err := validateOrigin(cfg.UIOrigin); err != nil {
		return Config{}, err
	}
	if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > 50 {
		return Config{}, fmt.Errorf("AUTO_SCHEDULER_MAX_CONCURRENCY must be between 1 and 50")
	}
	if cfg.AuthCacheTTL < 0 || cfg.AuthCacheTTL > 5*time.Minute {
		return Config{}, fmt.Errorf("AUTO_SCHEDULER_AUTH_CACHE_SECONDS must be between 0 and 300")
	}
	if cfg.UpstreamSyncInterval < 0 || (cfg.UpstreamSyncInterval > 0 && cfg.UpstreamSyncInterval < time.Minute) || cfg.UpstreamSyncInterval > 24*time.Hour {
		return Config{}, fmt.Errorf("AUTO_SCHEDULER_UPSTREAM_SYNC_SECONDS must be 0 or between 60 and 86400")
	}
	if cfg.NotificationInterval < 15*time.Second || cfg.NotificationInterval > 24*time.Hour {
		return Config{}, fmt.Errorf("AUTO_SCHEDULER_NOTIFICATION_INTERVAL_SECONDS must be between 15 and 86400")
	}

	absDataFile, err := filepath.Abs(cfg.DataFile)
	if err != nil {
		return Config{}, fmt.Errorf("resolve AUTO_SCHEDULER_DATA_FILE: %w", err)
	}
	cfg.DataFile = absDataFile
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func validateHTTPURL(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http(s) URL", name)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not include credentials, query, or fragment", name)
	}
	return nil
}

func validateOrigin(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("AUTO_SCHEDULER_UI_ORIGIN must be an absolute http(s) origin")
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("AUTO_SCHEDULER_UI_ORIGIN must contain only scheme and host")
	}
	return nil
}
