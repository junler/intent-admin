package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

var CacheNames = []string{"login_token", "captcha", "dict", "config", "user_perms", "ai:session", "ratelimit", "dashboard", "knowledge"}

func CacheNameAllowed(name string) bool {
	for _, n := range CacheNames {
		if n == name {
			return true
		}
	}
	return false
}

type CacheIn struct {
	CacheName string `json:"cacheName" form:"cacheName"`
	CacheKey  string `json:"cacheKey" form:"cacheKey"`
}

func (s *Service) cachePrefix(name string) (string, error) {
	if !CacheNameAllowed(name) {
		return "", errcode.Forbiddenf("缓存前缀不在可管理白名单内")
	}
	return s.cache.Key(name) + ":", nil
}
func (s *Service) fullCacheKey(in CacheIn) (string, error) {
	prefix, err := s.cachePrefix(in.CacheName)
	if err != nil {
		return "", err
	}
	if in.CacheKey == "" || len(in.CacheKey) > 512 {
		return "", errcode.BadRequestf("缓存 key 必填，最多 512 字节")
	}
	key := in.CacheKey
	if !strings.HasPrefix(key, prefix) {
		if strings.HasPrefix(key, s.cache.Prefix()) {
			return "", errcode.Forbiddenf("缓存 key 与所选白名单不匹配")
		}
		key = prefix + key
	}
	return key, nil
}
func (s *Service) cacheKeys(ctx context.Context, name string, maximum int) ([]string, error) {
	prefix, err := s.cachePrefix(name)
	if err != nil {
		return nil, err
	}
	keys := []string{}
	var cursor uint64
	for {
		found, next, err := s.cache.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, found...)
		if maximum > 0 && len(keys) >= maximum {
			return keys[:maximum], nil
		}
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}
func (s *Service) clearName(ctx context.Context, name string) error {
	prefix, err := s.cachePrefix(name)
	if err != nil {
		return err
	}
	var cursor uint64
	for {
		keys, next, err := s.cache.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err = s.cache.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
func (s *Service) registerCache() {
	capability.Register(capability.Capability[capability.Empty, map[string]any]{Meta: capability.Meta{Name: "monitor.cache.info", Summary: "查询缓存概况", Perm: "monitor:cache:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/cache/info"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, _ capability.Empty) (map[string]any, error) {
		info, err := s.cache.Info(ctx, "memory", "stats", "server").Result()
		if err != nil {
			return nil, err
		}
		values := map[string]string{}
		for _, line := range strings.Split(info, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
				values[k] = v
			}
		}
		return map[string]any{"redisVersion": values["redis_version"], "usedMemory": values["used_memory_human"], "usedMemoryPeak": values["used_memory_peak_human"], "totalCommandsProcessed": values["total_commands_processed"], "keyspaceHits": values["keyspace_hits"], "keyspaceMisses": values["keyspace_misses"], "names": CacheNames}, nil
	}})
	capability.Register(capability.Capability[capability.Empty, []map[string]any]{Meta: capability.Meta{Name: "monitor.cache.names", Summary: "列出可管理缓存白名单", Perm: "monitor:cache:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/cache/names"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, _ capability.Empty) ([]map[string]any, error) {
		out := []map[string]any{}
		for _, name := range CacheNames {
			keys, err := s.cacheKeys(ctx, name, 500)
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{"cacheName": name, "count": len(keys), "truncated": len(keys) == 500})
		}
		return out, nil
	}})
	capability.Register(capability.Capability[CacheIn, []string]{Meta: capability.Meta{Name: "monitor.cache.keys", Summary: "查询白名单缓存 key", Perm: "monitor:cache:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/cache/keys"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in CacheIn) ([]string, error) { return s.cacheKeys(ctx, in.CacheName, 500) }})
	capability.Register(capability.Capability[CacheIn, map[string]any]{Meta: capability.Meta{Name: "monitor.cache.value", Summary: "查询白名单缓存值", Perm: "monitor:cache:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/cache/value"}, AIEnabled: false, MCPEnabled: false}, Fn: func(ctx context.Context, in CacheIn) (map[string]any, error) {
		key, err := s.fullCacheKey(in)
		if err != nil {
			return nil, err
		}
		kind, err := s.cache.Type(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		var value any
		switch kind {
		case "none":
			return nil, errcode.NotFoundf("缓存不存在")
		case "string":
			value, err = s.cache.GetRange(ctx, key, 0, 65535).Result()
		case "hash":
			value, _, err = s.cache.HScan(ctx, key, 0, "*", 100).Result()
		case "list":
			value, err = s.cache.LRange(ctx, key, 0, 99).Result()
		case "set":
			value, _, err = s.cache.SScan(ctx, key, 0, "*", 100).Result()
		case "zset":
			value, err = s.cache.ZRangeWithScores(ctx, key, 0, 99).Result()
		default:
			value = "该类型不支持直接显示"
		}
		if err != nil {
			return nil, err
		}
		ttl, err := s.cache.TTL(ctx, key).Result()
		return map[string]any{"cacheName": in.CacheName, "cacheKey": key, "type": kind, "ttl": int64(ttl.Seconds()), "value": redactCacheValue(in.CacheName, key, value), "bounded": true}, err
	}})
	for _, action := range []string{"clearName", "clearKey", "clearAll"} {
		action := action
		risk := capability.RiskMedium
		if action == "clearAll" {
			risk = capability.RiskHigh
		}
		validate := func(in CacheIn) error {
			if action == "clearAll" {
				return nil
			}
			if _, err := s.cachePrefix(in.CacheName); err != nil {
				return err
			}
			if action == "clearKey" {
				_, err := s.fullCacheKey(in)
				return err
			}
			return nil
		}
		capability.Register(capability.Capability[CacheIn, capability.Empty]{Meta: capability.Meta{Name: "monitor.cache." + action, Summary: "清理白名单缓存：" + action, Perm: "monitor:cache:remove", Risk: risk, Mutating: true, Idempotent: true, HTTP: &capability.HTTPBinding{Method: "DELETE", Path: "/monitor/cache/" + action}, AIEnabled: true, MCPEnabled: true, LogTitle: "缓存监控", BizType: capability.BizDelete}, Fn: func(ctx context.Context, in CacheIn) (capability.Empty, error) {
			if err := validate(in); err != nil {
				return capability.Empty{}, err
			}
			var err error
			switch action {
			case "clearKey":
				key, _ := s.fullCacheKey(in)
				err = s.cache.Del(ctx, key).Err()
			case "clearName":
				err = s.clearName(ctx, in.CacheName)
			case "clearAll":
				for _, name := range CacheNames {
					if err = s.clearName(ctx, name); err != nil {
						break
					}
				}
			}
			return capability.Empty{}, err
		}, DryRun: func(ctx context.Context, in CacheIn) (*capability.Preview, error) {
			if err := validate(in); err != nil {
				return nil, err
			}
			summary := fmt.Sprintf("清理缓存 %s %s", in.CacheName, in.CacheKey)
			if action == "clearAll" {
				summary = "清理全部白名单缓存，不涉及其他 Redis key"
			}
			return &capability.Preview{Summary: summary, Affected: 1, Warnings: []string{"清理会话缓存会使对应用户下线；此操作不可撤销"}}, nil
		}})
	}
}

// Metadata remains inspectable, but sessions, challenges and credentials are
// never exposed through a second, less restrictive cache-viewing API.
func redactCacheValue(namespace, key string, value any) any {
	if namespace == "login_token" || namespace == "captcha" || namespace == "ai:session" {
		return "[敏感缓存内容已隐藏]"
	}
	lower := strings.ToLower(key)
	if namespace == "config" {
		for _, marker := range []string{"password", "passwd", "pwd", "secret", "token", "privatekey", "private_key", "credential", "apikey", "api_key", "accesskey"} {
			if strings.Contains(lower, marker) {
				return "[敏感配置值已隐藏]"
			}
		}
	}
	if raw, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			return capability.SanitizeValue(decoded)
		}
	}
	return capability.SanitizeValue(value)
}
