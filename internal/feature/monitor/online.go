package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
)

type OnlineIn struct {
	paginate.Query
	UserName string `json:"userName" form:"userName"`
	IPAddr   string `json:"ipaddr" form:"ipaddr"`
}
type TokenID struct {
	TokenID string `json:"tokenId" uri:"tokenId" validate:"required,uuid"`
}

func (s *Service) registerOnline() {
	capability.Register(capability.Capability[OnlineIn, Page[map[string]any]]{Meta: capability.Meta{Name: "monitor.online.list", Summary: "查询在线会话", Perm: "monitor:online:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/online/list"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in OnlineIn) (Page[map[string]any], error) {
		prefix := s.cache.Key("login_token") + ":"
		rows := []map[string]any{}
		var cursor uint64
		scanned := 0
		for {
			keys, next, err := s.cache.Scan(ctx, cursor, prefix+"*", 100).Result()
			if err != nil {
				return Page[map[string]any]{}, err
			}
			for _, key := range keys {
				scanned++
				raw, err := s.cache.Get(ctx, key).Bytes()
				if err != nil {
					continue
				}
				var session map[string]any
				if json.Unmarshal(raw, &session) != nil {
					continue
				}
				if in.UserName != "" && !strings.Contains(fmt.Sprint(session["userName"]), in.UserName) {
					continue
				}
				if in.IPAddr != "" && !strings.Contains(fmt.Sprint(session["ip"]), in.IPAddr) {
					continue
				}
				session["tokenId"] = strings.TrimPrefix(key, prefix)
				delete(session, "userId")
				rows = append(rows, session)
			}
			cursor = next
			if cursor == 0 || scanned >= 5000 {
				break
			}
		}
		total := len(rows)
		offset, limit := in.Normalize()
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		return Page[map[string]any]{rows[offset:end], int64(total), in.PageNum, in.PageSize}, nil
	}})
	capability.Register(capability.Capability[TokenID, capability.Empty]{Meta: capability.Meta{Name: "monitor.online.forceLogout", Summary: "强制退出在线会话", Perm: "monitor:online:forceLogout", Risk: capability.RiskHigh, Mutating: true, Idempotent: true, HTTP: &capability.HTTPBinding{Method: "DELETE", Path: "/monitor/online/:tokenId"}, AIEnabled: true, MCPEnabled: true, LogTitle: "在线用户", BizType: capability.BizForceOut}, Fn: func(ctx context.Context, in TokenID) (capability.Empty, error) {
		if _, err := uuid.Parse(in.TokenID); err != nil {
			return capability.Empty{}, errcode.BadRequestf("会话编号无效")
		}
		return capability.Empty{}, s.cache.Del(ctx, s.cache.Key("login_token", in.TokenID)).Err()
	}, DryRun: func(ctx context.Context, in TokenID) (*capability.Preview, error) {
		if _, err := uuid.Parse(in.TokenID); err != nil {
			return nil, errcode.BadRequestf("会话编号无效")
		}
		raw, err := s.cache.Get(ctx, s.cache.Key("login_token", in.TokenID)).Bytes()
		if err != nil {
			return nil, errcode.NotFoundf("会话已离线")
		}
		var session map[string]any
		_ = json.Unmarshal(raw, &session)
		return &capability.Preview{Summary: fmt.Sprintf("强制退出 %v 的会话（%v）", session["userName"], session["ip"]), Affected: 1, Warnings: []string{"此操作立即生效且不可撤销"}}, nil
	}})
}
