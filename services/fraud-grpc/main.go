package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	prommetrics "github.com/fluxa/fluxa/internal/adapters/prometheus"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/fraud"
	"github.com/fluxa/fluxa/internal/fraudeval"
	fraudv1 "github.com/fluxa/fluxa/internal/grpc/fraud/v1"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	grpcAddr    = ":9095"
	metricsAddr = ":9096"
	version     = "fluxa-rules-v1.0"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger("fraud-grpc", "init")

	dbClient, err := db.NewClient(cfg.DSN(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create database client: %v\n", err)
		os.Exit(1)
	}
	defer dbClient.Close()

	engine, err := fraud.NewEngine(cfg.RulesFile, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load fraud rules: %v\n", err)
		os.Exit(1)
	}

	metrics := prommetrics.NewMetrics("fraud-grpc")
	srv := fraudeval.NewServer(engine, dbClient, metrics, logger, version)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			fmt.Fprintf(os.Stderr, "Metrics server error: %v\n", err)
		}
	}()

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to listen on %s: %v\n", grpcAddr, err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(100),
		grpc.UnaryInterceptor(fraudeval.LoggingInterceptor(logger)),
	)
	fraudv1.RegisterFraudEvalServer(grpcServer, srv)
	reflection.Register(grpcServer)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("Shutdown signal received, draining gRPC server", nil)
		done := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			logger.Warn("GracefulStop timed out, forcing", nil)
			grpcServer.Stop()
		}
	}()

	logger.Info(fmt.Sprintf("fraud-grpc listening on %s (metrics %s)", grpcAddr, metricsAddr), nil)
	if err := grpcServer.Serve(lis); err != nil {
		fmt.Fprintf(os.Stderr, "gRPC serve error: %v\n", err)
		os.Exit(1)
	}
}
