// Package changeset persists the business writes in their ambient transaction
// and compensates them only while the recorded changed fields still match.
package changeset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/pkg/response"
)

type Service struct {
	db            *gorm.DB
	Invalidate    func(context.Context, string, string) error
	AuthorizeUndo func(context.Context, undo.Payload) error
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

type Set struct {
	ID            string          `gorm:"column:changeset_id;primaryKey" json:"changesetId"`
	ActorType     string          `json:"actorType"`
	ActorID       int64           `json:"actorId"`
	ActorName     string          `json:"actorName"`
	Source        string          `json:"source"`
	Intent        string          `json:"intent"`
	PlanID        string          `json:"planId,omitempty"`
	Status        string          `json:"status"`
	Risk          capability.Risk `json:"risk"`
	Summary       string          `json:"summary"`
	AffectedCount int             `json:"affectedCount"`
	ApprovalMode  string          `json:"approvalMode"`
	ApprovedBy    string          `json:"approvedBy"`
	CreateTime    time.Time       `json:"createTime"`
	RolledBackAt  *time.Time      `json:"rolledBackAt,omitempty"`
	RolledBackBy  string          `json:"rolledBackBy,omitempty"`
	RollbackOf    string          `json:"rollbackOf,omitempty"`
	Items         []Item          `gorm:"-" json:"items,omitempty"`
	RollbackID    string          `gorm:"-" json:"rollbackChangesetId,omitempty"`
}

func (Set) TableName() string { return "sys_changeset" }

type Item struct {
	ID          int64           `gorm:"column:item_id;primaryKey" json:"itemId"`
	ChangesetID string          `json:"changesetId"`
	Seq         int             `json:"seq"`
	Capability  string          `json:"capability"`
	Entity      string          `json:"entity"`
	EntityID    string          `json:"entityId"`
	EntityLabel string          `json:"entityLabel"`
	Op          capability.Op   `json:"op"`
	Args        json.RawMessage `gorm:"type:jsonb" json:"args,omitempty"`
	Before      json.RawMessage `gorm:"type:jsonb" json:"before,omitempty"`
	After       json.RawMessage `gorm:"type:jsonb" json:"after,omitempty"`
	Undo        json.RawMessage `gorm:"type:jsonb" json:"-"`
	Status      string          `json:"status"`
	ErrorMsg    string          `json:"errorMsg,omitempty"`
	CreateTime  time.Time       `json:"createTime"`
}

func (Item) TableName() string  { return "sys_changeset_item" }
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fields(v json.RawMessage) map[string]any {
	m := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(v))
	decoder.UseNumber()
	_ = decoder.Decode(&m)
	return m
}
func safeFields(v map[string]any) map[string]any {
	m := map[string]any{}
	for k, x := range v {
		if !capability.IsSensitive(k) {
			m[k] = x
		}
	}
	return m
}
func actor(ctx context.Context) (*capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return nil, errcode.Unauthorizedf("请先登录")
	}
	return a, nil
}

// Begin creates a group that subsequent invocations can join via Actor.ChangesetID.
func (s *Service) Begin(ctx context.Context, a *capability.Actor, summary string) (string, error) {
	if runes := []rune(summary); len(runes) > 500 {
		summary = string(runes[:500])
	}
	id := uuid.NewString()
	row := Set{ID: id, ActorType: a.Type, ActorID: a.UserID, ActorName: a.UserName, Source: string(a.Source), Intent: a.Intent, PlanID: a.PlanID, Status: "applied", Risk: capability.RiskLow, Summary: summary, ApprovalMode: a.ApprovalMode, CreateTime: time.Now()}
	return id, dbx.DBFrom(ctx, s.db).Create(&row).Error
}

// Persist is the capability executor hook. All snapshots and compensation data
// are written through DBFrom, atomically with the corresponding business rows.
func (s *Service) Persist(ctx context.Context, e *capability.Entry, a *capability.Actor, records []capability.RecordedItem, override json.RawMessage) (string, error) {
	db := dbx.DBFrom(ctx, s.db)
	id := a.ChangesetID
	if id == "" {
		var err error
		id, err = s.Begin(ctx, a, e.Summary)
		if err != nil {
			return "", err
		}
	}
	var set Set
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("changeset_id = ?", id).First(&set).Error; err != nil {
		return "", err
	}
	if set.ActorID != a.UserID || set.PlanID != a.PlanID {
		return "", errcode.Forbiddenf("不能将变更写入其他主体的变更集")
	}
	var seq int
	if err := db.Model(&Item{}).Where("changeset_id = ?", id).Select("COALESCE(MAX(seq),0)").Scan(&seq).Error; err != nil {
		return "", err
	}
	group := uuid.NewString()
	affected := 0
	for i, r := range records {
		before, after := safeFields(r.Before), safeFields(r.After)
		// Soft deletes record the flag explicitly so later edits cannot be
		// overwritten by a rollback of an older deletion.
		if r.Op == capability.OpDelete {
			current, err := s.snapshot(ctx, r.Entity, r.ID, false)
			if err == nil {
				if flag, ok := current["del_flag"]; ok {
					before["del_flag"] = "0"
					after = map[string]any{"del_flag": flag}
					for k := range before {
						if x, yes := current[k]; yes {
							after[k] = x
						}
					}
				}
			}
		}
		args := map[string]any{"input": capability.InvocationArguments(ctx)}
		payload := raw(undo.Payload{Entity: r.Entity, ID: r.ID, Op: r.Op, Before: before, After: after})
		if len(override) > 0 {
			args["undoGroup"] = group
			args["custom"] = true
			payload = nil
			if i == 0 {
				payload = raw(capability.SanitizeValue(json.RawMessage(override)))
			}
		}
		status := "applied"
		if r.Op == capability.OpSkip {
			status = "skipped"
		} else {
			affected++
		}
		item := Item{ChangesetID: id, Seq: seq + i + 1, Capability: e.Name, Entity: r.Entity, EntityID: r.ID, EntityLabel: r.Label, Op: r.Op, Args: raw(args), Before: raw(capability.SanitizeMap(r.Before)), After: raw(after), Undo: payload, Status: status, ErrorMsg: r.Reason, CreateTime: time.Now()}
		// The reversible snapshot must include the soft-delete flag too.
		item.Before = raw(before)
		if err := db.Create(&item).Error; err != nil {
			return "", err
		}
	}
	risk := set.Risk
	if e.Risk.Level() > risk.Level() {
		risk = e.Risk
	}
	if err := db.Model(&Set{}).Where("changeset_id = ?", id).Updates(map[string]any{"affected_count": gorm.Expr("affected_count + ?", affected), "risk": risk}).Error; err != nil {
		return "", err
	}
	return id, nil
}

// WriteAudit is deliberately independent of the business transaction: denied
// and failed attempts remain visible after their business work rolls back.
func (s *Service) WriteAudit(ctx context.Context, r capability.AuditRecord) {
	if !r.Success {
		// Failed inner attempts also wait for the surrounding transaction to
		// release its connection; otherwise a saturated pool can deadlock the
		// transaction on its own independent audit insert.
		dbx.AfterCommit(ctx, func() { s.persistAudit(ctx, r) })
		dbx.AfterRollback(ctx, func(error) { s.persistAudit(ctx, r) })
		return
	}
	dbx.AfterCommit(ctx, func() { s.persistAudit(ctx, r) })
	dbx.AfterRollback(ctx, func(cause error) {
		failed := r
		failed.Success = false
		failed.Result = ""
		failed.ChangesetID = ""
		failed.ErrorMsg = "外层事务已回滚：" + errcode.From(cause).Msg
		s.persistAudit(ctx, failed)
	})
}
func (s *Service) persistAudit(ctx context.Context, r capability.AuditRecord) {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	status := "0"
	if !r.Success {
		status = "1"
	}
	row := map[string]any{"title": r.Title, "business_type": r.BizType, "capability": r.Capability, "oper_param": r.Param, "json_result": r.Result, "status": status, "error_msg": r.ErrorMsg, "cost_time": r.CostMS, "oper_name": r.UserName, "oper_ip": r.IP, "oper_time": r.OccurredAt, "actor_type": r.ActorType, "source": r.Source, "intent": r.Intent, "risk": r.Risk, "approval_mode": r.ApprovalMode, "approved_by": r.ApprovedBy, "plan_id": r.PlanID, "changeset_id": r.ChangesetID}
	if r.TokenID > 0 {
		row["token_id"] = r.TokenID
	}
	if err := s.db.WithContext(auditCtx).Table("sys_oper_log").Create(row).Error; err != nil {
		slog.Error("审计记录写入失败", "error", err, "capability", r.Capability)
	}
}

type ListIn struct {
	paginate.Query
	ActorType string `json:"actorType" form:"actorType"`
	Source    string `json:"source" form:"source"`
	Status    string `json:"status" form:"status"`
	Intent    string `json:"intent" form:"intent"`
	Risk      string `json:"risk" form:"risk"`
}
type IDIn struct {
	ID string `json:"id" uri:"id" validate:"required"`
}

func (s *Service) List(ctx context.Context, in ListIn) (response.Page, error) {
	a, err := actor(ctx)
	if err != nil {
		return response.Page{}, err
	}
	if !a.Can("system:changeset:list") {
		return response.Page{}, errcode.Forbiddenf("没有查询变更集的权限")
	}
	q := dbx.DBFrom(ctx, s.db).Model(&Set{})
	if in.ActorType != "" {
		q = q.Where("actor_type = ?", in.ActorType)
	}
	if in.Source != "" {
		q = q.Where("source = ?", in.Source)
	}
	if in.Status != "" {
		q = q.Where("status = ?", in.Status)
	}
	if in.Intent != "" {
		q = q.Where("intent ILIKE ?", "%"+in.Intent+"%")
	}
	if in.Risk != "" {
		q = q.Where("risk = ?", in.Risk)
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return response.Page{}, err
	}
	rows := []Set{}
	offset, limit := in.Normalize()
	err = q.Order("create_time DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return response.Page{Rows: rows, Total: total, PageNum: in.PageNum, PageSize: in.PageSize}, err
}
func (s *Service) Get(ctx context.Context, id string) (*Set, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Can("system:changeset:query") && !a.Can("system:changeset:rollback") {
		return nil, errcode.Forbiddenf("没有查看变更集的权限")
	}
	return s.load(ctx, id, false)
}
func (s *Service) load(ctx context.Context, id string, lock bool) (*Set, error) {
	q := dbx.DBFrom(ctx, s.db)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var set Set
	if err := q.Where("changeset_id = ?", id).First(&set).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFoundf("变更集不存在")
		}
		return nil, err
	}
	err := dbx.DBFrom(ctx, s.db).Where("changeset_id = ?", id).Order("seq ASC").Find(&set.Items).Error
	return &set, err
}

var identifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// PrimaryKey discovers a single-column key only in the active schema. Names
// are database metadata, never caller-controlled SQL fragments.
func (s *Service) PrimaryKey(ctx context.Context, entity string) (string, error) {
	if !identifier.MatchString(entity) {
		return "", errcode.BadRequestf("实体名称无效")
	}
	var keys []string
	err := dbx.DBFrom(ctx, s.db).Raw(`SELECT a.attname FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=ANY(i.indkey) WHERE i.indisprimary AND c.relname=? AND n.nspname=current_schema() ORDER BY a.attnum`, entity).Scan(&keys).Error
	if err != nil {
		return "", err
	}
	if len(keys) != 1 {
		return "", errcode.Conflictf("实体 %s 缺少单字段主键，不能自动回滚", entity)
	}
	return keys[0], nil
}
func (s *Service) snapshot(ctx context.Context, entity, id string, lock bool) (map[string]any, error) {
	switch entity {
	case "sys_user_role", "sys_role_menu", "sys_user_perm":
		owner, column, field := "user_id", "role_id", "role_ids"
		if entity == "sys_role_menu" {
			owner, column, field = "role_id", "menu_id", "menu_ids"
		}
		if entity == "sys_user_perm" {
			owner, column, field = "user_id", "perm", "permissions"
			values := []string{}
			err := dbx.DBFrom(ctx, s.db).Table(entity).Where(owner+" = ?", id).Order(column).Pluck(column, &values).Error
			return map[string]any{field: values}, err
		}
		values := []int64{}
		err := dbx.DBFrom(ctx, s.db).Table(entity).Where(owner+" = ?", id).Order(column).Pluck(column, &values).Error
		return map[string]any{field: values}, err
	}

	key, err := s.PrimaryKey(ctx, entity)
	if err != nil {
		return nil, err
	}
	q := dbx.DBFrom(ctx, s.db).Table(entity)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row map[string]any
	tx := q.Where(clause.Eq{Column: clause.Column{Name: key}, Value: id}).Take(&row)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if entity == "sys_config" {
		if key, ok := row["config_key"].(string); ok && capability.IsSensitive(key) {
			delete(row, "config_value")
		}
	}
	return row, nil
}
func same(a, b any) bool {
	asTime := func(v any) (time.Time, bool) {
		switch x := v.(type) {
		case time.Time:
			return x, true
		case *time.Time:
			if x != nil {
				return *x, true
			}
		}
		return time.Time{}, false
	}
	if t, ok := asTime(a); ok {
		if u, ok := asTime(b); ok {
			return t.Equal(u)
		}
		if text, ok := b.(string); ok {
			u, err := time.Parse(time.RFC3339Nano, text)
			return err == nil && t.Equal(u)
		}
	}
	if _, ok := asTime(b); ok {
		return same(b, a)
	}
	number := func(v any) bool {
		if _, ok := v.(json.Number); ok {
			return true
		}
		if v == nil {
			return false
		}
		k := reflect.TypeOf(v).Kind()
		return (k >= reflect.Int && k <= reflect.Float64)
	}
	if number(a) || number(b) {
		numeric := func(v any) (*big.Rat, bool) {
			var text string
			if x, ok := v.(string); ok {
				text = x
			} else {
				encoded, err := json.Marshal(v)
				if err != nil {
					return nil, false
				}
				text = string(encoded)
			}
			r, ok := new(big.Rat).SetString(text)
			return r, ok
		}
		left, lok := numeric(a)
		right, rok := numeric(b)
		if lok && rok {
			return left.Cmp(right) == 0
		}
	}
	var normalize func(any) any
	normalize = func(v any) any {
		var encoded []byte
		switch x := v.(type) {
		case json.RawMessage:
			encoded = x
		case []byte:
			encoded = x
		case string:
			if strings.HasPrefix(x, "{") || strings.HasPrefix(x, "[") {
				encoded = []byte(x)
			}
		case map[string]any:
			out := map[string]any{}
			for k, value := range x {
				out[k] = normalize(value)
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, value := range x {
				out[i] = normalize(value)
			}
			return out
		}
		if len(encoded) > 0 {
			var decoded any
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.UseNumber()
			if decoder.Decode(&decoded) == nil {
				return normalize(decoded)
			}
		}
		return v
	}
	left, _ := json.Marshal(normalize(a))
	right, _ := json.Marshal(normalize(b))
	return string(left) == string(right)
}

func (s *Service) check(ctx context.Context, item Item, lock bool) error {
	e, ok := capability.Lookup(item.Capability)
	if !ok || !e.Reversible || !e.HasUndo() {
		return errcode.Conflictf("原操作不可回滚")
	}
	a, _ := actor(ctx)
	if (a.Source == capability.SourceAI && !e.AIEnabled) || (a.Source == capability.SourceMCP && !e.MCPEnabled) {
		return errcode.NotFoundf("原操作不能通过此入口回滚")
	}
	if (a.Source == capability.SourceAI || a.Source == capability.SourceMCP) && (!e.Risk.AtMost(a.MaxRisk) || e.Risk == capability.RiskCritical) {
		return errcode.New(errcode.RiskExceeded, "原操作风险超过当前主体的上限")
	}
	if !a.Can(e.Perm) {
		return errcode.Forbiddenf("缺少原操作权限 %s", e.Perm)
	}
	current, err := s.snapshot(ctx, item.Entity, item.EntityID, lock)
	if errors.Is(err, gorm.ErrRecordNotFound) && item.Op == capability.OpDelete {
		return nil
	}
	if err != nil {
		return err
	}
	expected := fields(item.After)
	if item.Op == capability.OpCreate && len(expected) == 0 {
		return errcode.Conflictf("新增记录缺少校验快照")
	}
	for k, value := range expected {
		if capability.IsSensitive(k) {
			continue
		}
		if !same(current[k], value) {
			return errcode.New(errcode.StaleData, "数据已变化，请重新核对后操作").WithDetail(map[string]any{"entity": item.Entity, "id": item.EntityID, "field": k})
		}
	}
	// Restoring identity data must not become a route around superadmin rules.
	if item.Entity == "sys_user" {
		var count int64
		if err := dbx.DBFrom(ctx, s.db).Raw(`SELECT COUNT(*) FROM (
SELECT 1 FROM sys_user_perm p WHERE p.user_id=? AND p.perm='*:*:*'
UNION ALL SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.role_id=ur.role_id WHERE ur.user_id=? AND r.role_key='admin' AND r.status='0' AND r.del_flag='0'
UNION ALL SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.role_id=ur.role_id JOIN sys_role_menu rm ON rm.role_id=r.role_id JOIN sys_menu m ON m.menu_id=rm.menu_id WHERE ur.user_id=? AND r.status='0' AND r.del_flag='0' AND m.status='0' AND m.del_flag='0' AND m.perms='*:*:*') grants`, item.EntityID, item.EntityID, item.EntityID).Scan(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			before := fields(item.Before)
			if !a.IsSuper() || before["status"] == "1" || before["del_flag"] == "2" {
				return errcode.Forbiddenf("不能通过回滚修改或停用超级管理员")
			}
		}
	}
	return nil
}
func (s *Service) PreviewRollback(ctx context.Context, id string) (*capability.Preview, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Can("system:changeset:rollback") {
		return nil, errcode.Forbiddenf("没有回滚权限")
	}
	set, err := s.load(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if set.Status == "rolled_back" {
		return nil, errcode.Conflictf("变更集已回滚")
	}
	p := &capability.Preview{Summary: "回滚变更集 " + id, Reversible: true, Changes: []capability.Change{}}
	virtual := map[string]map[string]any{}
	for i := len(set.Items) - 1; i >= 0; i-- {
		item := set.Items[i]
		if item.Status != "applied" && item.Status != "failed" {
			continue
		}
		change := capability.Change{Entity: item.Entity, ID: item.EntityID, Label: item.EntityLabel, Op: capability.OpUpdate}
		key := item.Entity + "#" + item.EntityID
		checkErr := s.check(ctx, item, false)
		current, exists := virtual[key]
		if exists && (checkErr == nil || errcode.From(checkErr).Code == errcode.StaleData) {
			checkErr = nil
			for field, value := range fields(item.After) {
				if !same(current[field], value) {
					checkErr = errcode.New(errcode.StaleData, "当前字段与变更记录不一致")
					break
				}
			}
		}
		if checkErr != nil {
			change.Op = capability.OpSkip
			change.Reason = checkErr.Error()
			p.Reversible = false
		} else {
			before, after := fields(item.Before), fields(item.After)
			keys := []string{}
			for k := range before {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if !same(after[k], before[k]) {
					change.Fields = append(change.Fields, capability.FieldDiff{Field: k, Before: after[k], After: before[k]})
				}
			}
			if !exists {
				current, _ = s.snapshot(ctx, item.Entity, item.EntityID, false)
			}
			if current == nil {
				current = map[string]any{}
			}
			if item.Op == capability.OpCreate {
				current = map[string]any{}
			} else {
				for k, v := range before {
					current[k] = v
				}
			}
			virtual[key] = current
		}
		p.AddChange(change)
	}
	return p, nil
}
func (s *Service) Rollback(ctx context.Context, id string) (*Set, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Can("system:changeset:rollback") {
		return nil, errcode.Forbiddenf("没有回滚权限")
	}
	var original *Set
	inverseID := ""
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		var e error
		original, e = s.load(txctx, id, false)
		if e != nil {
			return e
		}
		entities := []string{}
		for _, item := range original.Items {
			if entry, ok := capability.Lookup(item.Capability); ok && entry.Reversible && entry.HasUndo() {
				entities = append(entities, item.Entity)
			}
		}
		if e = s.LockEntities(txctx, entities); e != nil {
			return e
		}
		original, e = s.load(txctx, id, true)
		if e != nil {
			return e
		}
		if original.Status == "rolled_back" {
			return errcode.Conflictf("变更集已回滚")
		}
		if a.ChangesetID == "" {
			if a.Intent == "" {
				a.Intent = "回滚 #" + id
			}
			a.ChangesetID, e = s.Begin(txctx, a, "回滚 #"+id)
			if e != nil {
				return e
			}
		}
		inverseID = a.ChangesetID
		if e := dbx.DBFrom(txctx, s.db).Model(&Set{}).Where("changeset_id=?", inverseID).Update("rollback_of", id).Error; e != nil {
			return e
		}
		customDone := map[string]bool{}
		failed := 0
		skipped := 0
		rec := capability.RecorderFrom(txctx)
		actions := []rollbackAction{}
		for n := len(original.Items) - 1; n >= 0; n-- {
			item := &original.Items[n]
			if item.Status != "applied" && item.Status != "failed" {
				continue
			}
			entry, exists := capability.Lookup(item.Capability)
			if !exists || !entry.Reversible || !entry.HasUndo() {
				item.Status = "skipped"
				item.ErrorMsg = "原操作不可回滚"
				skipped++
				if e := dbx.DBFrom(txctx, s.db).Model(item).Updates(map[string]any{"status": item.Status, "error_msg": item.ErrorMsg}).Error; e != nil {
					return e
				}
				continue
			}
			args := fields(item.Args)
			group, _ := args["undoGroup"].(string)
			if group != "" && customDone[group] {
				item.Status = "rolled_back"
				if e := dbx.DBFrom(txctx, s.db).Model(item).Update("status", item.Status).Error; e != nil {
					return e
				}
				continue
			}
			oneRecords := []capability.RecordedItem{}
			oneActions := []rollbackAction{}
			itemErr := dbx.NewTxRunner(s.db)(txctx, func(oneCtx context.Context) error {
				if group != "" {
					for _, peer := range original.Items {
						peerGroup, _ := fields(peer.Args)["undoGroup"].(string)
						if peerGroup == group {
							if e := s.check(oneCtx, peer, true); e != nil {
								return e
							}
						}
					}
				} else {
					if e := s.check(oneCtx, *item, true); e != nil {
						return e
					}
				}
				peers := []Item{*item}
				if group != "" {
					peers = nil
					for _, peer := range original.Items {
						peerGroup, _ := fields(peer.Args)["undoGroup"].(string)
						if peerGroup == group {
							peers = append(peers, peer)
						}
					}
				}
				snapshots := map[int64]map[string]any{}
				for _, peer := range peers {
					current, err := s.snapshot(oneCtx, peer.Entity, peer.EntityID, false)
					if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
					snapshots[peer.ID] = current
				}
				payload := item.Undo
				if group != "" {
					for _, peer := range peers {
						if len(peer.Undo) > 0 {
							payload = peer.Undo
							break
						}
					}
				}
				if len(payload) == 0 {
					return errcode.Conflictf("缺少回滚数据")
				}
				inverseRecorder := capability.NewRecorder()
				if err := entry.Undo(capability.WithRecorder(oneCtx, inverseRecorder), payload); err != nil {
					return err
				}
				for _, peer := range peers {
					beforeUndo := snapshots[peer.ID]
					afterUndo, snapshotErr := s.snapshot(oneCtx, peer.Entity, peer.EntityID, false)
					if snapshotErr != nil && !errors.Is(snapshotErr, gorm.ErrRecordNotFound) {
						return snapshotErr
					}
					inverse := capability.RecordedItem{Entity: peer.Entity, ID: peer.EntityID, Label: peer.EntityLabel}
					if snapshotErr != nil {
						inverse.Op = capability.OpDelete
						inverse.Before = safeFields(beforeUndo)
					} else if beforeUndo == nil {
						inverse.Op = capability.OpCreate
						inverse.After = safeFields(afterUndo)
					} else {
						inverse.Op = capability.OpUpdate
						inverse.Before = map[string]any{}
						inverse.After = map[string]any{}
						for k, v := range beforeUndo {
							if !capability.IsSensitive(k) && !same(v, afterUndo[k]) {
								inverse.Before[k] = v
								inverse.After[k] = afterUndo[k]
							}
						}
					}
					oneRecords = append(oneRecords, inverse)
					if s.Invalidate != nil {
						entity, key := peer.Entity, peer.EntityID
						dbx.AfterCommit(oneCtx, func() {
							if err := s.Invalidate(context.WithoutCancel(ctx), entity, key); err != nil {
								slog.Error("回滚后权限缓存清理失败", "error", err)
							}
						})
					}
				}
				if inverse, ok := inverseRecorder.UndoOverride(); ok {
					oneActions = append(oneActions, rollbackAction{Capability: entry.Name, Guard: entry.Name, Payload: inverse})
				} else {
					for _, inverse := range oneRecords {
						oneActions = append(oneActions, rollbackAction{Capability: "system.changeset.rollback", Guard: entry.Name, Payload: raw(undo.Payload{Entity: inverse.Entity, ID: inverse.ID, Op: inverse.Op, Before: inverse.Before, After: inverse.After})})
					}
				}
				return nil
			})
			if itemErr != nil {
				failed++
				item.Status = "failed"
				item.ErrorMsg = itemErr.Error()
			} else {
				for _, record := range oneRecords {
					recordInto(rec, record)
				}
				actions = append(actions, oneActions...)
				item.Status = "rolled_back"
				item.ErrorMsg = ""
				if group != "" {
					customDone[group] = true
				}
			}
			if e := dbx.DBFrom(txctx, s.db).Model(item).Updates(map[string]any{"status": item.Status, "error_msg": item.ErrorMsg}).Error; e != nil {
				return e
			}
		}
		status := "rolled_back"
		if failed > 0 || skipped > 0 {
			status = "rollback_failed"
		}
		if len(actions) > 0 {
			rec.SetUndo(rollbackUndo{Actions: actions})
		}
		now := time.Now()
		return dbx.DBFrom(txctx, s.db).Model(original).Updates(map[string]any{"status": status, "rolled_back_at": now, "rolled_back_by": a.UserName}).Error
	})
	if err != nil {
		return nil, err
	}
	result, err := s.load(ctx, id, false)
	if result != nil {
		result.RollbackID = inverseID
	}
	return result, err
}

// Undo compensates the inverse records generated by Rollback.
func (s *Service) Undo(ctx context.Context, payload json.RawMessage) error {
	var wrapped rollbackUndo
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return err
	}
	if len(wrapped.Actions) > 0 {
		return s.undoActions(ctx, wrapped)
	}
	var p undo.Payload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&p); err != nil {
		return err
	}
	if s.AuthorizeUndo != nil {
		if err := s.AuthorizeUndo(ctx, p); err != nil {
			return err
		}
	}
	if p.Entity == "sys_user_role" || p.Entity == "sys_role_menu" || p.Entity == "sys_user_perm" {
		return s.undoAssociation(ctx, p)
	}
	key, err := s.PrimaryKey(ctx, p.Entity)
	if err != nil {
		return err
	}
	db := dbx.DBFrom(ctx, s.db).Table(p.Entity)
	where := clause.Eq{Column: clause.Column{Name: key}, Value: p.ID}
	switch p.Op {
	case capability.OpUpdate:
		return db.Where(where).Updates(safeFields(p.Before)).Error
	case capability.OpCreate:
		return db.Where(where).Delete(nil).Error
	case capability.OpDelete:
		row := safeFields(p.Before)
		if _, exists := row[key]; !exists {
			row[key] = p.ID
		}
		return db.Create(&row).Error
	default:
		return errcode.BadRequestf("不支持回滚此变更类型")
	}
}
func (s *Service) Finish(ctx context.Context, id, status string) error {
	return dbx.DBFrom(ctx, s.db).Model(&Set{}).Where("changeset_id=?", id).Update("status", status).Error
}
func (s *Service) Register() {
	capability.Register(capability.Capability[ListIn, response.Page]{Meta: capability.Meta{Name: "system.changeset.list", Summary: "查询变更历史", Perm: "system:changeset:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/system/changeset/list"}, AIEnabled: true, MCPEnabled: true}, Fn: s.List})
	capability.Register(capability.Capability[IDIn, *Set]{Meta: capability.Meta{Name: "system.changeset.get", Summary: "变更集详情", Perm: "system:changeset:query", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/system/changeset/:id"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in IDIn) (*Set, error) { return s.Get(ctx, in.ID) }})
	capability.Register(capability.Capability[IDIn, *Set]{Meta: capability.Meta{Name: "system.changeset.rollback", Summary: "回滚变更集", Perm: "system:changeset:rollback", Risk: capability.RiskHigh, Mutating: true, Reversible: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/system/changeset/:id/rollback"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in IDIn) (*Set, error) { return s.Rollback(ctx, in.ID) }, DryRun: func(ctx context.Context, in IDIn) (*capability.Preview, error) { return s.PreviewRollback(ctx, in.ID) }, Undo: s.Undo})
}

// LockEntities serialises preview rechecks with concurrent writes, including
// inserts that could otherwise change a filtered affected set after checking.
func (s *Service) LockEntities(ctx context.Context, entities []string) error {
	sort.Strings(entities)
	last := ""
	for _, name := range entities {
		if name == last {
			continue
		}
		last = name
		if !identifier.MatchString(name) {
			return errcode.BadRequestf("实体名称无效")
		}
		var count int64
		if err := dbx.DBFrom(ctx, s.db).Raw("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relname=? AND n.nspname=current_schema() AND c.relkind='r'", name).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errcode.BadRequestf("实体不存在")
		}
		if err := dbx.DBFrom(ctx, s.db).Exec("LOCK TABLE \"" + strings.ReplaceAll(name, "\"", "\"\"") + "\" IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) undoAssociation(ctx context.Context, p undo.Payload) error {
	a, err := actor(ctx)
	if err != nil {
		return err
	}
	owner, column, field := "user_id", "role_id", "role_ids"
	if p.Entity == "sys_role_menu" {
		owner, column, field = "role_id", "menu_id", "menu_ids"
	}
	if p.Entity == "sys_user_perm" {
		owner, column, field = "user_id", "perm", "permissions"
	}
	values, ok := p.Before[field]
	if !ok {
		return errcode.BadRequestf("关联回滚数据缺少 %s", field)
	}
	if !a.IsSuper() {
		var count int64
		q := dbx.DBFrom(ctx, s.db).Table("sys_role")
		if p.Entity == "sys_role_menu" {
			q = q.Where("role_id=? AND role_key='admin'", p.ID)
		} else {
			q = q.Joins("JOIN sys_user_role ur ON ur.role_id=sys_role.role_id").Where("ur.user_id=? AND role_key='admin'", p.ID)
		}
		if err := q.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errcode.Forbiddenf("不能回滚超级管理员的关联授权")
		}
	}
	db := dbx.DBFrom(ctx, s.db)
	if err := db.Table(p.Entity).Where(owner+"=?", p.ID).Delete(nil).Error; err != nil {
		return err
	}
	var list []any
	if err := json.Unmarshal(raw(values), &list); err != nil {
		return err
	}
	for _, value := range list {
		if p.Entity == "sys_user_role" && !a.IsSuper() {
			var count int64
			if err := db.Table("sys_role").Where("role_id=? AND role_key='admin'", value).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errcode.Forbiddenf("不能授予超级管理员角色")
			}
		}
		row := map[string]any{owner: p.ID, column: value}
		if err := db.Table(p.Entity).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

type rollbackAction struct {
	Capability string          `json:"capability"`
	Guard      string          `json:"guard,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}
type rollbackUndo struct {
	Actions []rollbackAction `json:"actions"`
}

func recordInto(rec capability.Recorder, item capability.RecordedItem) {
	switch item.Op {
	case capability.OpCreate:
		rec.Create(item.Entity, item.ID, item.Label, item.After)
	case capability.OpUpdate:
		rec.Update(item.Entity, item.ID, item.Label, item.Before, item.After)
	case capability.OpDelete:
		rec.Delete(item.Entity, item.ID, item.Label, item.Before)
	case capability.OpSkip:
		rec.Skip(item.Entity, item.ID, item.Label, item.Reason)
	}
}
func (s *Service) undoActions(ctx context.Context, data rollbackUndo) error {
	a, err := actor(ctx)
	if err != nil {
		return err
	}
	inverse := []rollbackAction{}
	for i := len(data.Actions) - 1; i >= 0; i-- {
		action := data.Actions[i]
		entry, ok := capability.Lookup(action.Capability)
		if !ok || !entry.HasUndo() {
			return errcode.Conflictf("回滚功能已不可用")
		}
		guard := entry
		if action.Guard != "" {
			var exists bool
			guard, exists = capability.Lookup(action.Guard)
			if !exists {
				return errcode.Conflictf("原操作已不存在")
			}
		}
		if !a.Can(guard.Perm) {
			return errcode.Forbiddenf("没有原操作权限")
		}
		if (a.Source == capability.SourceAI && !guard.AIEnabled) || (a.Source == capability.SourceMCP && !guard.MCPEnabled) {
			return errcode.NotFoundf("原操作不能通过此入口恢复")
		}
		if (a.Source == capability.SourceAI || a.Source == capability.SourceMCP) && (!guard.Risk.AtMost(a.MaxRisk) || guard.Risk == capability.RiskCritical) {
			return errcode.New(errcode.RiskExceeded, "原操作风险超过当前主体上限")
		}
		local := capability.NewRecorder()
		if err := entry.Undo(capability.WithRecorder(ctx, local), action.Payload); err != nil {
			return err
		}
		if payload, ok := local.UndoOverride(); ok {
			inverse = append(inverse, rollbackAction{Capability: action.Capability, Guard: action.Guard, Payload: payload})
			continue
		}
		if action.Capability != "system.changeset.rollback" {
			return errcode.Conflictf("原操作未提供反向恢复数据")
		}
		var p undo.Payload
		decoder := json.NewDecoder(bytes.NewReader(action.Payload))
		decoder.UseNumber()
		if err := decoder.Decode(&p); err != nil {
			return err
		}
		switch p.Op {
		case capability.OpUpdate:
			p.Before, p.After = p.After, p.Before
		case capability.OpCreate:
			p.Op = capability.OpDelete
			p.Before = p.After
			p.After = nil
		case capability.OpDelete:
			p.Op = capability.OpCreate
			p.After = p.Before
			p.Before = nil
		default:
			return errcode.BadRequestf("不支持的反向恢复类型")
		}
		inverse = append(inverse, rollbackAction{Capability: action.Capability, Guard: action.Guard, Payload: raw(p)})
	}
	capability.RecorderFrom(ctx).SetUndo(rollbackUndo{Actions: inverse})
	return nil
}
