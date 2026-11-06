package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/testutil"
)

func TestPrivateConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw, err := config.Initial("Orders")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(path, raw); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("configuration is readable by other users")
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.App.Name != "Orders" || loaded.App.EncryptKey == loaded.JWT.Secret || len(loaded.JWT.Secret) < 40 {
		t.Fatal("invalid bootstrap configuration")
	}
	if err := writePrivate(path, []byte("overwritten")); err == nil {
		t.Fatal("overwrote an existing configuration")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(raw) {
		t.Fatal("existing configuration changed")
	}
}
func TestInitializeAdmin(t *testing.T) {
	db := testutil.Postgres(t)
	pool, _ := db.DB()
	if err := initializeAdmin(t.Context(), pool, "random-initial-password", false); err != nil {
		t.Fatal(err)
	}
	var user struct {
		Password           string
		MustChangePassword bool
	}
	if err := db.Table("sys_user").Where("user_name='admin'").Take(&user).Error; err != nil {
		t.Fatal(err)
	}
	if !user.MustChangePassword || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("random-initial-password")) != nil {
		t.Fatal("administrator not initialized")
	}
	var count int64
	db.Table("sys_menu").Where("perms LIKE 'demo:%' AND status='0'").Count(&count)
	if count != 0 {
		t.Fatal("demo enabled by default")
	}
	db.Table("demo_item").Count(&count)
	if count != 0 {
		t.Fatal("demo rows installed by default")
	}
}
