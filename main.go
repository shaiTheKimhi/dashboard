package main

import (
	"context"
	"crypto/tls"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed static/*
var embeddedAssets embed.FS

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	root, err := os.OpenRoot(cfg.FileRoot)
	if err != nil {
		return errors.New("cannot open shared directory; check DASHBOARD_FILE_ROOT and read/search permissions")
	}
	defer root.Close()
	assets, err := fs.Sub(embeddedAssets, "static")
	if err != nil {
		return err
	}
	collector := NewCollector("/proc", "/sys", "/etc", cfg.FileRoot)
	_, _ = collector.Sample(time.Now())
	samplerDone := make(chan struct{})
	go func() { defer close(samplerDone); collector.Run(ctx, 2*time.Second) }()
	defer func() { stop(); <-samplerDone }()
	server := &http.Server{Addr: cfg.ListenAddr, Handler: NewHandler(cfg, collector.Latest, root, assets), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	log.Printf("Jetson dashboard listening on %s", cfg.ListenAddr)
	if cfg.TLSCert != "" {
		err = server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	} else {
		err = server.ListenAndServe()
	}
	stop()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
