package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/config"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/engine"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/notify"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/onlineusers"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	stateStore, err := store.Open(cfg.DataFile)
	if err != nil {
		logger.Error("open state store", "error", err)
		os.Exit(1)
	}
	coreClient, err := core.NewClient(cfg.Sub2APIBaseURL, cfg.AdminAPIKey)
	if err != nil {
		logger.Error("create Sub2API client", "error", err)
		os.Exit(1)
	}
	onlineUsers, onlineUsersErr := onlineusers.Open(cfg.OnlineDatabaseURL, logger)
	if onlineUsersErr != nil {
		logger.Warn("online aggregate database disabled", "error", onlineUsersErr)
		onlineUsers, _ = onlineusers.Open("", logger)
	}
	if onlineUsers.Configured() {
		coreClient.SetGroupAccessReader(onlineUsers)
	}
	defer func() {
		if err := onlineUsers.Close(); err != nil {
			logger.Warn("close online aggregate database", "error", err)
		}
	}()

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	credentialBox, credentialErr := upstream.NewCredentialBox(cfg.CredentialKey)
	if credentialErr != nil {
		logger.Warn("upstream credential encryption disabled", "error", credentialErr)
		credentialBox, _ = upstream.NewCredentialBox("")
	}
	upstreamManager := upstream.NewManager(stateStore, coreClient, credentialBox, cfg.UpstreamSyncInterval, logger)
	upstreamManager.Start(rootCtx)

	scheduler := engine.New(
		stateStore,
		coreClient,
		cfg.MaxConcurrency,
		logger,
		engine.WithDirectProbeCredentials(credentialBox),
	)
	if err := scheduler.EnterPassiveMode(rootCtx); err != nil {
		logger.Warn("active probing disabled with account restoration failures", "error", err)
	} else {
		logger.Info("active account probing disabled; passive success metrics enabled")
	}
	notificationCoordinator := notify.NewCoordinator(
		stateStore,
		coreClient,
		upstreamManager,
		credentialBox,
		cfg.NotificationInterval,
		logger,
	)
	notificationCoordinator.Start(rootCtx)
	webServer := web.NewServer(scheduler, coreClient, web.Options{
		UIOrigin:          cfg.UIOrigin,
		PublicURL:         cfg.PublicURL,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
		AuthCacheTTL:      cfg.AuthCacheTTL,
		Upstreams:         upstreamManager,
		Notifications:     notificationCoordinator,
		OnlineUsers:       onlineUsers,
		AccountSuccess:    onlineUsers,
	}, logger)

	if cfg.AutoRegisterTab {
		registrationCtx, cancel := context.WithTimeout(rootCtx, 30*time.Second)
		if err := webServer.RegisterTab(registrationCtx); err != nil {
			logger.Error("register admin tab", "error", err)
		} else {
			logger.Info("admin tab registered", "public_url", cfg.PublicURL)
		}
		cancel()
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           webServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("account auto scheduler started", "listen_addr", cfg.ListenAddr, "sub2api_base_url", cfg.Sub2APIBaseURL, "online_database_configured", onlineUsers.Configured())
		serverErr <- httpServer.ListenAndServe()
	}()

	select {
	case <-rootCtx.Done():
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped unexpectedly", "error", err)
			stopSignals()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown http server", "error", err)
	}
	notificationCoordinator.Stop()
	logger.Info("account auto scheduler stopped")
}
