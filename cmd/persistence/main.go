// Persistence service: single writer to Postgres and Redis for scan lifecycle events.
// Subscribes to scan.started, scan.completed, scan.failed and writes idempotently.
package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cafe-persistence/internal/config"
	"cafe-persistence/internal/health"
	"cafe-persistence/internal/persistence/handlers"
	"cafe-persistence/internal/scanddl"
	"cafe-persistence/internal/persistence/planlimit"
	persistenceNats "cafe-persistence/internal/persistence/nats"
	persistenceStorage "cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"
	"cafe-persistence/internal/scanapi"
	natsconn "cafe-persistence/pkg/nats"
	postgresdb "cafe-persistence/pkg/postgres"
	redisconn "cafe-persistence/pkg/redis"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

func main() {
	initConfig()
	initLogging()

	healthState := &health.State{}
	healthPort := viper.GetString(config.PersistenceHealthPort)
	if healthPort == "" {
		healthPort = "8081"
	}
	healthServer := health.NewServer("0.0.0.0", healthPort, healthState)
	healthServer.Start()
	defer func() {
		healthState.SetReady(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownCtx)
	}()

	// Postgres
	db := postgresdb.New()
	if err := db.Run(); err != nil {
		log.Fatal().Err(err).Msg("postgres run failed")
	}
	defer db.Shutdown()

	// Scan tables (persistence owns these). Dev: reset Postgres volume if schema changes brutally.
	if err := scanddl.MigrateScanSchema(db.GetDB()); err != nil {
		log.Fatal().Err(err).Msg("scan schema migration failed")
	}

	// Redis
	redis, err := redisconn.New()
	if err != nil {
		log.Fatal().Err(err).Msg("redis connect failed")
	}
	defer func() {
		if err := redis.Close(); err != nil {
			log.Warn().Err(err).Msg("redis close failed")
		}
	}()

	// NATS
	nc, err := natsconn.New()
	if err != nil {
		log.Fatal().Err(err).Msg("nats connect failed")
	}
	defer nc.Close()

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	cfgChain, err := config.LoadChainConfig(configPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", configPath).Msg("chain config load failed (need blockchains[].chain_id)")
	}

	// Storage and handlers
	tlsWriter := persistenceStorage.NewTLSWriter(db.GetDB())
	walletWriter := persistenceStorage.NewWalletWriter(db.GetDB())
	cache := persistenceStorage.NewRedisCache(redis)
	ledgerRepo := repository.NewScanUsageLedgerRepository(db.GetDB())
	planLimits := planlimit.NewResolver(
		repository.NewUserRepository(db.GetDB()),
		repository.NewPlanRepository(db.GetDB()),
	)
	scanHandler := handlers.NewScanEventHandler(
		tlsWriter, walletWriter, cache, nc, cfgChain.ChainIDByNetwork(),
		db.GetDB(), ledgerRepo, planLimits,
	)

	// Internal scan HTTP API (PERS-D3a-impl) — not exposed on public edge.
	internalPort := viper.GetString(config.PersistenceInternalHTTPPort)
	if internalPort == "" {
		internalPort = "8082"
	}
	serviceToken := strings.TrimSpace(viper.GetString(config.CafePersistenceServiceToken))
	pendingRepo, err := repository.NewRedisPendingV1ScanRepository(redis)
	if err != nil {
		log.Fatal().Err(err).Msg("pending v1 scan repository init failed")
	}
	scanAPIHandler := scanapi.NewHandler(
		pendingRepo,
		repository.NewScanResultRepository(db.GetDB()),
		repository.NewTLSScanResultRepository(db.GetDB()),
		ledgerRepo,
		cache,
		cfgChain,
	)
	internalScanServer := scanapi.NewServer("0.0.0.0", internalPort, serviceToken, scanAPIHandler)
	internalScanServer.Start()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = internalScanServer.Shutdown(shutdownCtx)
	}()
	if serviceToken == "" {
		log.Warn().Msg("CAFE_PERSISTENCE_SERVICE_TOKEN unset — internal scan API rejects all callers until configured")
	} else {
		log.Info().Str("port", internalPort).Msg("internal scan API listening")
	}

	subs, err := persistenceNats.SubscribeScanEvents(nc, scanHandler)
	if err != nil {
		log.Fatal().Err(err).Msg("subscribe scan events failed")
	}
	defer func() {
		healthState.SetReady(false)
		for _, sub := range subs {
			_ = sub.Unsubscribe()
		}
	}()

	if !nc.IsConnected() {
		log.Fatal().Msg("NATS not connected after subscribe")
	}
	healthState.SetReady(true)
	log.Info().Str("port", healthPort).Msg("persistence readiness OK (/ready returns 200)")

	// Signal to backend that persistence is ready; repeat for a while so backend can catch it when it starts after us
	go func() {
		payload := []byte("{}")
		for i := 0; i < 40; i++ {
			if err := nc.Publish(natsconn.SubjectPersistenceReady, payload); err != nil {
				log.Warn().Err(err).Msg("persistence.ready publish failed")
			}
			time.Sleep(3 * time.Second)
		}
	}()
	log.Info().Msg("persistence.ready will be published every 3s for 1 minute")

	log.Info().Msg("persistence-service running (scan.started / scan.completed / scan.failed)")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Info().Msg("shutting down persistence-service")
}

func initConfig() {
	for k, v := range config.GetDefaultConfigValues() {
		viper.SetDefault(k, v)
	}
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")
	_ = viper.ReadInConfig()
	viper.AutomaticEnv()
}

func initLogging() {
	logLevel := viper.GetString(config.LogLevel)
	if logLevel == "" {
		logLevel = "info"
	}
	var level zerolog.Level
	switch strings.ToLower(logLevel) {
	case "trace":
		level = zerolog.TraceLevel
	case "debug":
		level = zerolog.DebugLevel
	case "info":
		level = zerolog.InfoLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	case "fatal":
		level = zerolog.FatalLevel
	case "panic":
		level = zerolog.PanicLevel
	default:
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}
	log.Logger = zerolog.New(output).With().Timestamp().Logger()
}
