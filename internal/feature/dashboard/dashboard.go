// Package dashboard supplies cached operational statistics with a last-good
// fallback. A temporary database outage must not blank the home screen.
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/task"
	"gorm.io/gorm"
)

type ActorCount struct {
	ActorType   string  `json:"actorType"`
	Count       int64   `json:"count"`
	Failed      int64   `json:"failed"`
	FailureRate float64 `json:"failureRate"`
}
type DayCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}
type OperationDay struct {
	Date      string `json:"date"`
	Human     int64  `json:"human"`
	Agent     int64  `json:"agent"`
	MCPClient int64  `json:"mcp_client"`
	System    int64  `json:"system"`
	Failed    int64  `json:"failed"`
}
type Stats struct {
	Days             int              `json:"days"`
	UserCount        int64            `json:"userCount"`
	ActiveUsers      int64            `json:"activeUsers"`
	DisabledUsers    int64            `json:"disabledUsers"`
	RoleCount        int64            `json:"roleCount"`
	OnlineCount      int64            `json:"onlineCount"`
	OnlineUsers      int64            `json:"onlineUsers"`
	TodayOperations  int64            `json:"todayOperations"`
	TodayLogins      int64            `json:"todayLogins"`
	ChangesetCount   int64            `json:"changesetCount"`
	RollbackCount    int64            `json:"rollbackCount"`
	RollbackRate     float64          `json:"rollbackRate"`
	QueueBacklog     int64            `json:"queueBacklog"`
	JobSuccess       int64            `json:"jobSuccess"`
	JobFailed        int64            `json:"jobFailed"`
	ByActor          []ActorCount     `json:"byActor"`
	LoginTrend       []DayCount       `json:"loginTrend"`
	OperationTrend   []OperationDay   `json:"operationTrend"`
	DailyOperations  []DayCount       `json:"dailyOperations"`
	RecentOperations []map[string]any `json:"recentOperations"`
	Degraded         bool             `json:"degraded"`
	CachedAt         time.Time        `json:"cachedAt"`
}
type OverviewIn struct {
	Days    int  `json:"days" form:"days"`
	Refresh bool `json:"refresh" form:"refresh"`
}
type Service struct {
	db       *gorm.DB
	cache    *cache.Cache
	tasks    *task.Runtime
	location *time.Location
}

func New(db *gorm.DB, c *cache.Cache, r *task.Runtime) *Service {
	return &Service{db, c, r, time.Local}
}
func Register(db *gorm.DB, c *cache.Cache, cfg *config.Config, r *task.Runtime) *Service {
	s := New(db, c, r)
	if loc, err := time.LoadLocation(cfg.DB.TimeZone); err == nil {
		s.location = loc
	}
	r.RefreshDashboard = s.Warm
	capability.Register(capability.Capability[OverviewIn, Stats]{Meta: capability.Meta{Name: "dashboard.overview", Summary: "查询首页大盘", Perm: "dashboard:view", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/dashboard/overview"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in OverviewIn) (Stats, error) {
		if in.Days == 0 {
			in.Days = 7
		}
		if in.Days != 7 && in.Days != 30 {
			return Stats{}, errcode.BadRequestf("统计范围只能为 7 或 30 天")
		}
		if in.Refresh {
			return s.RefreshDays(ctx, in.Days)
		}
		return s.GetDays(ctx, in.Days)
	}})
	capability.Register(capability.Capability[capability.Empty, Stats]{Meta: capability.Meta{Name: "dashboard.stats", Summary: "查询首页默认统计", Perm: "dashboard:view", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/dashboard"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, _ capability.Empty) (Stats, error) { return s.Get(ctx) }})
	capability.Register(capability.Capability[capability.Empty, Stats]{Meta: capability.Meta{Name: "dashboard.refresh", Summary: "刷新首页统计缓存", Perm: "dashboard:view", Risk: capability.RiskLow, Mutating: true, Idempotent: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/dashboard/refresh"}, AIEnabled: true, MCPEnabled: true, LogTitle: "首页大盘"}, Fn: func(ctx context.Context, _ capability.Empty) (Stats, error) { return s.Refresh(ctx) }, DryRun: func(context.Context, capability.Empty) (*capability.Preview, error) {
		return &capability.Preview{Summary: "重新计算首页统计并刷新缓存", Affected: 1}, nil
	}})
	return s
}
func (s *Service) Warm(ctx context.Context) error {
	for _, days := range []int{7, 30} {
		if _, err := s.RefreshDays(ctx, days); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Get(ctx context.Context) (Stats, error)     { return s.GetDays(ctx, 7) }
func (s *Service) Refresh(ctx context.Context) (Stats, error) { return s.RefreshDays(ctx, 7) }
func (s *Service) cacheKey(days int) string {
	return s.cache.Key("dashboard", "overview", fmt.Sprint(days), time.Now().In(s.location).Format("20060102"))
}
func (s *Service) GetDays(ctx context.Context, days int) (Stats, error) {
	var out Stats
	if raw, err := s.cache.Get(ctx, s.cacheKey(days)).Bytes(); err == nil && json.Unmarshal(raw, &out) == nil {
		return out, nil
	}
	return s.RefreshDays(ctx, days)
}
func (s *Service) RefreshDays(ctx context.Context, days int) (Stats, error) {
	out := Stats{Days: days, ByActor: []ActorCount{}, LoginTrend: []DayCount{}, DailyOperations: []DayCount{}, RecentOperations: []map[string]any{}, CachedAt: time.Now()}
	now := time.Now().In(s.location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	since := today.AddDate(0, 0, -days+1)
	q := s.db.WithContext(ctx)
	queries := []struct {
		table, where string
		args         []any
		dst          *int64
	}{{"sys_user", "del_flag='0'", nil, &out.UserCount}, {"sys_user", "del_flag='0' AND status='0'", nil, &out.ActiveUsers}, {"sys_user", "del_flag='0' AND status='1'", nil, &out.DisabledUsers}, {"sys_role", "del_flag='0'", nil, &out.RoleCount}, {"sys_oper_log", "oper_time>=?", []any{today}, &out.TodayOperations}, {"sys_login_log", "login_time>=? AND status='0'", []any{today}, &out.TodayLogins}, {"sys_changeset", "create_time>=?", []any{since}, &out.ChangesetCount}, {"sys_changeset", "create_time>=? AND status='rolled_back'", []any{since}, &out.RollbackCount}, {"sys_job_log", "create_time>=? AND status='0'", []any{since}, &out.JobSuccess}, {"sys_job_log", "create_time>=? AND status='1'", []any{since}, &out.JobFailed}}
	for _, x := range queries {
		if err := q.Table(x.table).Where(x.where, x.args...).Count(x.dst).Error; err != nil {
			return s.fallback(ctx, days), nil
		}
	}
	if out.ChangesetCount > 0 {
		out.RollbackRate = float64(out.RollbackCount) / float64(out.ChangesetCount)
	}
	if err := q.Table("sys_oper_log").Select("actor_type,count(*) AS count,sum(CASE WHEN status='1' THEN 1 ELSE 0 END) AS failed").Where("oper_time>=?", since).Group("actor_type").Scan(&out.ByActor).Error; err != nil {
		return s.fallback(ctx, days), nil
	}
	for i := range out.ByActor {
		if out.ByActor[i].Count > 0 {
			out.ByActor[i].FailureRate = float64(out.ByActor[i].Failed) / float64(out.ByActor[i].Count)
		}
	}
	if err := q.Table("sys_oper_log").Select("to_char(oper_time,'YYYY-MM-DD') AS date,count(*) AS count").Where("oper_time>=?", since).Group("date").Order("date ASC").Scan(&out.DailyOperations).Error; err != nil {
		return s.fallback(ctx, days), nil
	}
	if err := q.Table("sys_login_log").Select("to_char(login_time,'YYYY-MM-DD') AS date,count(*) AS count").Where("login_time>=? AND status='0'", since).Group("date").Order("date ASC").Scan(&out.LoginTrend).Error; err != nil {
		return s.fallback(ctx, days), nil
	}
	var recent []struct {
		OperID     int64 `gorm:"column:oper_id"`
		Title      string
		Capability string
		OperName   string
		ActorType  string
		Source     string
		Risk       string
		Status     string
		OperTime   time.Time
	}
	if err := q.Table("sys_oper_log").Select("oper_id,title,capability,oper_name,actor_type,source,risk,status,oper_time").Order("oper_time DESC").Limit(10).Scan(&recent).Error; err != nil {
		return s.fallback(ctx, days), nil
	}
	for _, r := range recent {
		out.RecentOperations = append(out.RecentOperations, map[string]any{"operId": r.OperID, "title": r.Title, "capability": r.Capability, "operName": r.OperName, "actorType": r.ActorType, "source": r.Source, "risk": r.Risk, "status": r.Status, "operTime": r.OperTime})
	}
	var cursor uint64
	for {
		keys, next, err := s.cache.Scan(ctx, cursor, s.cache.Key("login_token")+":*", 100).Result()
		if err != nil {
			break
		}
		out.OnlineUsers += int64(len(keys))
		cursor = next
		if cursor == 0 {
			break
		}
	}
	out.OnlineCount = out.OnlineUsers
	var operations []struct {
		Date      string
		ActorType string
		Count     int64
		Failed    int64
	}
	if err := q.Table("sys_oper_log").Select("to_char(oper_time,'YYYY-MM-DD') AS date,actor_type,count(*) AS count,sum(CASE WHEN status='1' THEN 1 ELSE 0 END) AS failed").Where("oper_time>=?", since).Group("date,actor_type").Order("date ASC").Scan(&operations).Error; err != nil {
		return s.fallback(ctx, days), nil
	}
	byDay := map[string]*OperationDay{}
	order := []string{}
	for _, op := range operations {
		day := byDay[op.Date]
		if day == nil {
			day = &OperationDay{Date: op.Date}
			byDay[op.Date] = day
			order = append(order, op.Date)
		}
		switch op.ActorType {
		case "human":
			day.Human += op.Count
		case "agent":
			day.Agent += op.Count
		case "mcp_client":
			day.MCPClient += op.Count
		case "system":
			day.System += op.Count
		}
		day.Failed += op.Failed
	}
	out.OperationTrend = []OperationDay{}
	for _, date := range order {
		out.OperationTrend = append(out.OperationTrend, *byDay[date])
	}
	if s.tasks != nil {
		out.QueueBacklog = s.tasks.Backlog(ctx)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	pipe := s.cache.Pipeline()
	pipe.Set(ctx, s.cacheKey(days), raw, time.Hour)
	pipe.Set(ctx, s.cache.Key("dashboard", "lastgood", fmt.Sprint(days)), raw, 24*time.Hour)
	_, _ = pipe.Exec(ctx)
	return out, nil
}
func (s *Service) fallback(ctx context.Context, days int) Stats {
	out := Stats{Days: days, ByActor: []ActorCount{}, LoginTrend: []DayCount{}, DailyOperations: []DayCount{}, RecentOperations: []map[string]any{}}
	if raw, err := s.cache.Get(ctx, s.cache.Key("dashboard", "lastgood", fmt.Sprint(days))).Bytes(); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	out.Degraded = true
	return out
}
