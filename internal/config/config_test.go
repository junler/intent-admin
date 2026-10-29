package config

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDSNRoundTrip(t *testing.T) {
	for _, password := range []string{"", "a password with spaces", "s'ecret\\@:/?#&="} {
		t.Run(password, func(t *testing.T) {
			db := DB{
				Host: "127.0.0.1", Port: 5432, User: "postgres", Password: password,
				Name: "intent_admin", SSLMode: "disable", TimeZone: "Asia/Shanghai",
			}
			parsed, err := pgx.ParseConfig(db.DSN())
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Database != db.Name || parsed.User != db.User || parsed.Password != password || parsed.Host != db.Host || parsed.Port != 5432 {
				t.Fatal("database connection fields did not round trip")
			}
			if parsed.RuntimeParams["TimeZone"] != db.TimeZone {
				t.Fatalf("TimeZone = %q", parsed.RuntimeParams["TimeZone"])
			}
		})
	}
}

func TestEnvironmentOnlyBootstrap(t *testing.T) {
	t.Setenv("INTENT_ADMIN_JWT_SECRET", "environment-signing-secret")
	t.Setenv("INTENT_ADMIN_APP_ENCRYPT_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("INTENT_ADMIN_DB_PASSWORD", "environment-db-password")
	t.Setenv("INTENT_ADMIN_AI_BOOTSTRAP_VENDOR", "custom")
	t.Setenv("INTENT_ADMIN_AI_BOOTSTRAP_MODEL", "environment-model")
	t.Setenv("INTENT_ADMIN_STORAGE_BOOTSTRAP_SECRET_KEY", "environment-storage-secret")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.Secret != "environment-signing-secret" || cfg.DB.Password != "environment-db-password" || cfg.AI.Bootstrap.Vendor != "custom" || cfg.AI.Bootstrap.Model != "environment-model" || cfg.Storage.Bootstrap.SecretKey != "environment-storage-secret" {
		t.Fatal("environment-only configuration fields were not bound")
	}
}
