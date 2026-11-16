package postgres

import (
	"strings"
	"testing"
	"time"

	"cafe-persistence/internal/config"

	"github.com/spf13/viper"
)

const wantUnsetMessage = config.PostgreSQLPassword + " not set in config file or environment variable"

// credentialMarker is the marker of the marker pattern: a fake credential whose text
// must never appear in an error.
const credentialMarker = "credential-must-not-leak"

func baseSettings() connSettings {
	return connSettings{
		host:     "db.example.test",
		port:     "5432",
		user:     "app",
		password: "a-value-used-only-in-this-example",
		database: "appdb",
		sslMode:  "disable",
	}
}

// §5.19: an empty credential is refused with the fixed message that names the setting.
func TestConnConfigRequiresCredential(t *testing.T) {
	s := baseSettings()
	s.password = ""

	cfg, err := connConfig(s)

	if cfg != nil {
		t.Errorf("config = %v, want nil", cfg)
	}
	if err == nil || err.Error() != wantUnsetMessage {
		t.Fatalf("error = %v, want %q", err, wantUnsetMessage)
	}
}

// §5.19: values with spaces, quotes, backslashes, key=value text, or edge spaces reach the driver exactly.
func TestConnConfigKeepsValuesExact(t *testing.T) {
	tricky := []string{
		"with space",
		"it's",
		`back\slash`,
		"dbname=other",
		" leading",
		"trailing ",
		`mix 'of' \all `,
	}
	fields := map[string]func(*connSettings, string){
		"host":     func(s *connSettings, v string) { s.host = v },
		"user":     func(s *connSettings, v string) { s.user = v },
		"database": func(s *connSettings, v string) { s.database = v },
		"password": func(s *connSettings, v string) { s.password = v },
	}

	for name, set := range fields {
		for _, v := range tricky {
			t.Run(name+"/"+v, func(t *testing.T) {
				s := baseSettings()
				set(&s, v)

				cfg, err := connConfig(s)

				if err != nil || cfg == nil {
					t.Fatalf("connConfig = %v, %v; want a config", cfg, err)
				}
				if cfg.Host != s.host || cfg.User != s.user || cfg.Database != s.database || cfg.Password != s.password {
					t.Errorf("values not exact: host=%q user=%q database=%q credential-match=%t",
						cfg.Host, cfg.User, cfg.Database, cfg.Password == s.password)
				}
				if cfg.Port != 5432 {
					t.Errorf("port = %d, want 5432", cfg.Port)
				}
				if cfg.TLSConfig != nil {
					t.Error("TLSConfig is set, want nil with sslmode disable")
				}
			})
		}
	}
}

// §5.19: a parse error never carries the credential.
func TestConnConfigErrorCarriesNoCredential(t *testing.T) {
	s := baseSettings()
	s.password = credentialMarker
	s.sslMode = "not-a-valid-mode"

	cfg, err := connConfig(s)

	if cfg != nil {
		t.Errorf("config = %v, want nil", cfg)
	}
	if err == nil {
		t.Fatal("want a parse error for an invalid sslmode")
	}
	if !strings.Contains(err.Error(), "invalid PostgreSQL connection settings") {
		t.Errorf("error = %q, want the invalid-settings error", err.Error())
	}
	if strings.Contains(err.Error(), credentialMarker) {
		t.Error("the error carries the credential")
	}
}

// §5.19: Run refuses an unset credential at once, before any connection attempt.
func TestRunWithoutCredentialFailsBeforeConnecting(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set(config.PostgreSQLHost, "192.0.2.1") // unroutable test address
	viper.Set(config.PostgreSQLPort, "5432")
	viper.Set(config.PostgreSQLUser, "app")
	viper.Set(config.PostgreSQLDatabase, "appdb")
	viper.Set(config.PostgreSQLSSLMode, "disable")

	start := time.Now()
	err := New().Run()

	if err == nil || !strings.Contains(err.Error(), wantUnsetMessage) {
		t.Fatalf("error = %v, want it to contain %q", err, wantUnsetMessage)
	}
	if time.Since(start) >= 2*time.Second {
		t.Error("Run did not fail at once")
	}
}
