package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/config"
)

func TestCacheWhitelistIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := &config.Config{Redis: config.Redis{Addr: mr.Addr(), KeyPrefix: "test:"}}
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := &Service{cache: c}
	ctx := context.Background()
	for _, key := range []string{"test:dict:visible", "test:login_token:session", "test:private:secret", "another:dict:hidden"} {
		if err := c.Set(ctx, key, "value", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.cacheKeys(ctx, "private", 500); err == nil {
		t.Fatal("private namespace exposed")
	}
	if _, err := s.fullCacheKey(CacheIn{CacheName: "dict", CacheKey: "test:private:secret"}); err == nil {
		t.Fatal("namespace mismatch accepted")
	}
	keys, err := s.cacheKeys(ctx, "dict", 500)
	if err != nil || len(keys) != 1 || keys[0] != "test:dict:visible" {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	for _, name := range CacheNames {
		if err := s.clearName(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if !mr.Exists("test:private:secret") || !mr.Exists("another:dict:hidden") {
		t.Fatal("cache clearing crossed namespace boundary")
	}
	if mr.Exists("test:dict:visible") {
		t.Fatal("allowed cache not cleared")
	}
}

func TestSensitiveCacheValueRedaction(t *testing.T) {
	for _, name := range []string{"login_token", "captcha", "ai:session"} {
		if value := redactCacheValue(name, "test:"+name+":id", "private-value"); value == "private-value" {
			t.Fatalf("sensitive namespace %s leaked", name)
		}
	}
	if value := redactCacheValue("config", "test:config:sys.user.initPassword", "123456"); value == "123456" {
		t.Fatal("password configuration leaked")
	}
	for _, key := range []string{"sys.webhook.token", "sys.provider.private_key", "sys.legacy.passwd"} {
		if value := redactCacheValue("config", "test:config:"+key, "credential"); value == "credential" {
			t.Fatalf("credential config %s leaked", key)
		}
	}
	value := redactCacheValue("knowledge", "test:knowledge:enums", `{"password":"private","status":"0"}`).(map[string]any)
	if value["password"] == "private" || value["status"] != "0" {
		t.Fatalf("bad nested redaction: %v", value)
	}
	if value := redactCacheValue("dict", "test:dict:status", "plain-label"); value != "plain-label" {
		t.Fatalf("nonsensitive value changed: %v", value)
	}
}
