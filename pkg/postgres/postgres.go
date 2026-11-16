package postgres

import (
	"cafe-persistence/internal/config"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type PostgreSQLConnection interface {
	GetDB() *gorm.DB
	IsConnected() bool
	Run() error
	Shutdown()
}

// connSettings holds the six PostgreSQL connection settings.
type connSettings struct {
	host     string
	port     string
	user     string
	password string
	database string
	sslMode  string
}

type postgreSQLConnection struct {
	settings connSettings
	db       *gorm.DB
}

func New() PostgreSQLConnection {
	return &postgreSQLConnection{
		settings: connSettings{
			host:     viper.GetString(config.PostgreSQLHost),
			port:     viper.GetString(config.PostgreSQLPort),
			user:     viper.GetString(config.PostgreSQLUser),
			password: viper.GetString(config.PostgreSQLPassword),
			database: viper.GetString(config.PostgreSQLDatabase),
			sslMode:  viper.GetString(config.PostgreSQLSSLMode),
		},
	}
}

// connConfig builds the driver configuration from the settings. The credential is
// set on the parsed configuration and never goes through a connection string, so no
// parse error can carry it.
func connConfig(s connSettings) (*pgx.ConnConfig, error) {
	if s.password == "" {
		return nil, fmt.Errorf("%s not set in config file or environment variable", config.PostgreSQLPassword)
	}

	connString := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s",
		quoteConnValue(s.host), quoteConnValue(s.port), quoteConnValue(s.user),
		quoteConnValue(s.database), quoteConnValue(s.sslMode))

	cfg, err := pgx.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL connection settings: %w", err)
	}
	cfg.Password = s.password

	return cfg, nil
}

// quoteConnValue quotes and escapes a value of a keyword/value connection string.
func quoteConnValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}

func (c *postgreSQLConnection) GetDB() *gorm.DB {
	return c.db
}

func (c *postgreSQLConnection) IsConnected() bool {
	if c.db == nil {
		return false
	}

	dbSQL, errSQL := c.db.DB()
	if errSQL != nil {
		return false
	}

	if errPing := dbSQL.Ping(); errPing != nil {
		return false
	}

	return true
}

func (c *postgreSQLConnection) Run() error {
	// Log the connection attempt (without the credential).
	log.Info().
		Str("host", c.settings.host).
		Str("port", c.settings.port).
		Str("user", c.settings.user).
		Str("database", c.settings.database).
		Msg("Attempting to connect to PostgreSQL")

	cfg, err := connConfig(c.settings)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to PostgreSQL")
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	sqlDB := stdlib.OpenDB(*cfg)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		if errClose := sqlDB.Close(); errClose != nil {
			log.Error().Err(errClose).Msg("Failed to close database connection")
		}
		log.Error().Err(err).Msg("Failed to connect to PostgreSQL")
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	c.db = db
	log.Info().Msg("Connected to PostgreSQL")
	return nil
}

func (c *postgreSQLConnection) Shutdown() {
	log.Info().Msg("Shutting down PostgreSQL connection")
	dbSQL, err := c.db.DB()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get database connection for shutdown")
		return
	}

	if errClose := dbSQL.Close(); errClose != nil {
		log.Error().Err(errClose).Msg("Failed to close database connection")
	}
}
