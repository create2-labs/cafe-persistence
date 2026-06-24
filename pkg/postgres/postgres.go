package postgres

import (
	"cafe-persistence/internal/config"
	"fmt"

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

type postgreSQLConnection struct {
	dsn string
	db  *gorm.DB
}

func New() PostgreSQLConnection {
	dbHost := viper.GetString(config.PostgreSQLHost)
	dbPort := viper.GetString(config.PostgreSQLPort)
	dbUser := viper.GetString(config.PostgreSQLUser)
	dbPassword := viper.GetString(config.PostgreSQLPassword)
	dbName := viper.GetString(config.PostgreSQLDatabase)
	dbSSLMode := viper.GetString(config.PostgreSQLSSLMode)

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		dbHost, dbPort, dbUser, dbPassword, dbName, dbSSLMode)

	return &postgreSQLConnection{
		dsn: dsn,
	}
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
	// Log connection attempt (without password)
	dbHost := viper.GetString(config.PostgreSQLHost)
	dbPort := viper.GetString(config.PostgreSQLPort)
	dbName := viper.GetString(config.PostgreSQLDatabase)
	dbUser := viper.GetString(config.PostgreSQLUser)

	log.Info().
		Str("host", dbHost).
		Str("port", dbPort).
		Str("user", dbUser).
		Str("database", dbName).
		Msg("Attempting to connect to PostgreSQL")

	db, err := gorm.Open(postgres.Open(c.dsn), &gorm.Config{})
	if err != nil {
		log.Error().
			Err(err).
			Str("dsn", maskDSN(c.dsn)).
			Msg("Failed to connect to PostgreSQL")
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	c.db = db
	log.Info().Msg("Connected to PostgreSQL")
	return nil
}

// maskDSN masks the password in the DSN for logging
func maskDSN(dsn string) string {
	// Simple masking - replace password=*** with password=***
	// Format: host=... port=... user=... password=... dbname=... sslmode=...
	parts := dsn
	if len(parts) > 0 {
		// Find password= and mask everything until next space
		for i := 0; i < len(parts)-8; i++ {
			if parts[i:i+9] == "password=" {
				for j := i + 9; j < len(parts); j++ {
					if parts[j] == ' ' {
						return parts[:i+9] + "***" + parts[j:]
					}
				}
				return parts[:i+9] + "***"
			}
		}
	}
	return "***"
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
