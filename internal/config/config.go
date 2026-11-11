package config

const (
	// Zerolog values from [trace, debug, info, warn, error, fatal, panic].
	LogLevel = "LOG_LEVEL"

	// PersistenceHealthPort is the HTTP port for /health and /ready probes (PERS-D2b).
	PersistenceHealthPort = "PERSISTENCE_HEALTH_PORT"

	// PersistenceInternalHTTPPort is the internal scan + CP API listener (PERS-D3a-impl / PERS-D4b).
	PersistenceInternalHTTPPort = "PERSISTENCE_INTERNAL_HTTP_PORT"

	// CafePersistenceServiceToken is the static bearer token for internal HTTP APIs (ADR §9.1 / §9.2).
	// #nosec G101 -- configuration key name, not a hardcoded credential
	CafePersistenceServiceToken = "CAFE_PERSISTENCE_SERVICE_TOKEN"

	// PostgreSQL configuration
	PostgreSQLHost = "POSTGRES_HOST"
	PostgreSQLPort = "POSTGRES_PORT"
	PostgreSQLUser = "POSTGRES_USER"
	// #nosec G101 -- This is a configuration key name, not a hardcoded credential
	PostgreSQLPassword = "POSTGRES_PASSWORD"
	PostgreSQLDatabase = "POSTGRES_DATABASE"
	PostgreSQLSSLMode  = "POSTGRES_SSLMODE"

	// NATS configuration
	NATSURL = "NATS_URL"

	// Redis configuration
	RedisURL = "REDIS_URL"

	defaultPostgreSQLHost     = "127.0.0.1"
	defaultPostgreSQLPort     = "5432"
	defaultPostgreSQLUser     = "cafe"
	defaultPostgreSQLDatabase = "cafe"
	defaultPostgreSQLSSLMode  = "disable"
	defaultNATSURL            = "nats://localhost:4222"
	defaultRedisURL           = "redis://localhost:6379"
	defaultPersistenceHealthPort     = "8081"
	defaultPersistenceInternalHTTPPort = "8082"
)

func GetDefaultConfigValues() map[string]any {
	return map[string]any{
		PostgreSQLHost:           defaultPostgreSQLHost,
		PostgreSQLPort:           defaultPostgreSQLPort,
		PostgreSQLUser:           defaultPostgreSQLUser,
		PostgreSQLDatabase:       defaultPostgreSQLDatabase,
		PostgreSQLSSLMode:        defaultPostgreSQLSSLMode,
		NATSURL:                  defaultNATSURL,
		RedisURL:                 defaultRedisURL,
		PersistenceHealthPort:        defaultPersistenceHealthPort,
		PersistenceInternalHTTPPort:  defaultPersistenceInternalHTTPPort,
	}
}
