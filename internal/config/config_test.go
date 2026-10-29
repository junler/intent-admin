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
