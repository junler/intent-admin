package config

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Initial returns a standalone configuration with independent random keys.
// It does not copy credentials from the process environment into a file.
func Initial(name string) ([]byte, error) {
	v := viper.New()
	setDefaults(v)
	key := func() string {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	v.Set("app.name", name)
	v.Set("app.encrypt_key", key())
	v.Set("jwt.secret", key())
	v.Set("db.password", "")
	v.Set("redis.password", "")
	v.Set("storage.bootstrap.base_url", "http://localhost:8080/api/public/files")
	return yaml.Marshal(v.AllSettings())
}
