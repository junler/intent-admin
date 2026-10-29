package importer

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/service"
	systemsvc "github.com/junler/intent-admin/internal/service/system"
)

const previewTTL = 10 * time.Minute

type Service struct {
	db         *gorm.DB
	cache      *cache.Cache
	changesets *changeset.Service
	maxRows    int
}
type ConfirmInput struct {
	ConfirmationID string `json:"confirmationId" validate:"required,uuid"`
	Fingerprint    string `json:"fingerprint" validate:"required,len=64,hexadecimal"`
}
type Result struct {
	Affected    int    `json:"affected"`
	ChangesetID string `json:"changesetId"`
}
type Report struct {
	ConfirmationID string              `json:"confirmationId"`
	Fingerprint    string              `json:"fingerprint,omitempty"`
	ExpiresAt      time.Time           `json:"expiresAt"`
	Total          int                 `json:"total"`
	Valid          bool                `json:"valid"`
	Errors         []RowError          `json:"errors"`
	Preview        *capability.Preview `json:"preview"`
}
type stored struct {
	Report
	UserID     int64      `json:"userId"`
	Capability string     `json:"capability"`
	Rows       []inputRow `json:"rows"`
	Salt       []byte     `json:"salt"`
}

// New is called after the ordinary create capabilities are registered. Imports
// compose those capabilities; no spreadsheet path writes business data itself.
func New(db *gorm.DB, c *cache.Cache, sets *changeset.Service, maxItems ...int) *Service {
	s := &Service{db: db, cache: c, changesets: sets, maxRows: MaxRows}
	if len(maxItems) > 0 && maxItems[0] > 0 && maxItems[0] < s.maxRows {
		s.maxRows = maxItems[0]
	}
	for _, sp := range specs {
		e, ok := capability.Lookup(sp.Capability)
		if !ok {
			panic("missing import create capability: " + sp.Capability)
		}
		if !e.Mutating || !e.Reversible || !e.HasDryRun() || !e.HasUndo() {
			panic("import requires a reversible create capability: " + sp.Capability)
		}
		name := importName(sp)
		capability.Register(capability.Capability[ConfirmInput, Result]{
			Meta: capability.Meta{Name: name, Summary: "批量导入" + sp.Title, Tags: []string{"批量导入"}, Perm: e.Perm, Risk: e.Risk, Mutating: true, Reversible: true, BizType: capability.BizImport, LogTitle: "导入" + sp.Title},
			Fn:   func(ctx context.Context, in ConfirmInput) (Result, error) { return s.confirm(ctx, sp, in) },
			DryRun: func(ctx context.Context, in ConfirmInput) (*capability.Preview, error) {
				v, _, err := s.load(ctx, sp, in.ConfirmationID)
				if err != nil {
					return nil, err
				}
				p, _, err := s.previewRows(ctx, sp, v.Rows)
				return p, err
			},
			Undo: e.Undo,
		})
	}
	return s
}
func importName(s spec) string { return strings.TrimSuffix(s.Capability, "create") + "import" }
func (s *Service) authorize(ctx context.Context, sp spec) (*capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return nil, errcode.Unauthorizedf("请先登录")
	}
	if a.Source != capability.SourceHTTP {
		return nil, errcode.Forbiddenf("批量导入需要在管理页面上传并确认")
	}
	e, ok := capability.Lookup(sp.Capability)
	if !ok {
		return nil, errcode.NotFoundf("不支持该导入类型")
	}
	if !a.Can(e.Perm) {
		return nil, errcode.Forbiddenf("没有新增%s的权限", sp.Title)
	}
	return a, nil
}

func (s *Service) Preview(ctx context.Context, sp spec, file io.Reader) (*Report, error) {
	a, err := s.authorize(ctx, sp)
	if err != nil {
		return nil, err
	}
	rows, errors, total, err := parse(sp, file)
	if err != nil {
		return nil, err
	}
	if total > s.maxRows {
		return nil, errcode.BadRequestf("一次最多导入 %d 行，请分批导入", s.maxRows)
	}
	id := uuid.NewString()
	salt := make([]byte, 32)
	if _, err = rand.Read(salt); err != nil {
		return nil, err
	}
	v := stored{Report: Report{ConfirmationID: id, ExpiresAt: time.Now().Add(previewTTL), Total: total, Errors: errors}, UserID: a.UserID, Capability: sp.Capability, Rows: rows, Salt: salt}
	// A single repeatable snapshot makes the reviewed batch internally
	// consistent; the capability preview runner additionally rejects writes.
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		if e := dbx.DBFrom(txctx, s.db).Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").Error; e != nil {
			return e
		}
		txctx = dbx.WithReadOnly(txctx)
		p, rowErrors, e := s.previewRows(txctx, sp, rows)
		if e != nil {
			return e
		}
		v.Preview = p
		v.Errors = append(v.Errors, rowErrors...)
		v.Valid = len(v.Errors) == 0
		if v.Valid {
			v.Fingerprint, e = s.fingerprint(txctx, sp, &v, p)
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(v.Errors, func(i, j int) bool { return v.Errors[i].Row < v.Errors[j].Row })
	// Invalid input values are never retained in Redis or the error workbook.
	if !v.Valid {
		v.Rows = nil
		v.Salt = nil
	}
	raw, _ := json.Marshal(v)
	err = redis.NewScript(`
local now=tonumber(ARGV[1]); redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
if redis.call('ZCARD',KEYS[2])>=5 then return 0 end
redis.call('SET',KEYS[1],ARGV[4],'PX',ARGV[3]); redis.call('ZADD',KEYS[2],ARGV[2],KEYS[1]); redis.call('PEXPIRE',KEYS[2],ARGV[3]); return 1
`).Run(ctx, s.cache.Client, []string{s.key(id), s.cache.Key("import_owner", capability.ID(a.UserID))}, time.Now().UnixMilli(), v.ExpiresAt.UnixMilli(), previewTTL.Milliseconds(), string(raw)).Err()
	if err != nil {
		return nil, errcode.Internalf("导入预览暂不可用").Wrap(err)
	}
	// Read back so a quota rejection cannot appear to be a confirmable upload.
	if exists, e := s.cache.Exists(ctx, s.key(id)).Result(); e != nil {
		return nil, errcode.Internalf("导入预览暂不可用").Wrap(e)
	} else if exists != 1 {
		return nil, errcode.New(errcode.TooManyReq, "最多保留 5 个导入预览，请等待过期后重试")
	}
	return &v.Report, nil
}

func (s *Service) previewRows(ctx context.Context, sp spec, rows []inputRow) (*capability.Preview, []RowError, error) {
	if _, err := s.authorize(ctx, sp); err != nil {
		return nil, nil, err
	}
	p := &capability.Preview{Summary: "批量新增" + sp.Title, Capability: importName(sp), Reversible: true, Evidence: &capability.Evidence{Route: sp.Base}}
	if e, ok := capability.Lookup(sp.Capability); ok {
		p.Risk = e.Risk
	}
	errors := []RowError{}
	for _, row := range rows {
		exists, err := s.uniqueExists(ctx, sp, row)
		if err != nil {
			return nil, nil, err
		}
		if exists {
			errors = append(errors, RowError{row.Number, "唯一键已存在，导入只支持新增"})
			continue
		}
		one, err := capability.RunPreview(ctx, sp.Capability, row.Arguments)
		if err != nil {
			code := errcode.From(err)
			if code.Code == errcode.InternalError || code.Code == errcode.Unauthorized || code.Code == errcode.Forbidden {
				return nil, nil, err
			}
			errors = append(errors, RowError{row.Number, code.Msg})
			continue
		}
		if one.Truncated {
			return nil, nil, errcode.BadRequestf("单行预览超出上限")
		}
		for _, ch := range one.Changes {
			ch.ID = "row:" + capability.ID(row.Number)
			// JSON numbers beyond JavaScript's exact integer range would show a
			// rounded amount in the browser even though PostgreSQL stores it exactly.
			for i, f := range ch.Fields {
				if n, ok := f.After.(int64); ok && (n > 9007199254740991 || n < -9007199254740991) {
					ch.Fields[i].After = capability.ID(n)
				}
			}
			p.AddChange(ch)
		}
		for _, w := range one.Warnings {
			p.Warn(w)
		}
	}
	if sp.Capability == "system.user.create" {
		p.Warn("用户使用系统配置的初始密码创建，不分配角色或额外权限。请通过安全渠道通知用户并要求修改密码。")
	}
	if len(errors) > 0 {
		p.Warn("存在校验错误，整批不会执行；请修正文件后重新上传。")
	}
	return p, errors, nil
}

func (s *Service) uniqueExists(ctx context.Context, sp spec, row inputRow) (bool, error) {
	tables := map[string]string{"system.user.create": "sys_user", "system.role.create": "sys_role", "system.config.create": "sys_config", "system.dictData.create": "sys_dict_data", "demo.item.create": "demo_item"}
	var args map[string]any
	if err := json.Unmarshal(row.Arguments, &args); err != nil {
		return false, err
	}
	q := dbx.DBFrom(ctx, s.db).Table(tables[sp.Capability])
	for _, field := range sp.Unique {
		value := args[field]
		if value == nil {
			value = ""
		}
		q = q.Where(systemsvc.Snake(field)+" = ?", value)
	}
	if sp.Capability == "system.user.create" || sp.Capability == "system.role.create" || sp.Capability == "demo.item.create" {
		q = q.Where("del_flag='0'")
	}
	var count int64
	err := q.Count(&count).Error
	return count > 0, err
}

func (s *Service) key(id string) string { return s.cache.Key("import_preview", id) }
func (s *Service) load(ctx context.Context, sp spec, id string) (*stored, string, error) {
	a, err := s.authorize(ctx, sp)
	if err != nil {
		return nil, "", err
	}
	if _, err = uuid.Parse(id); err != nil {
		return nil, "", errcode.BadRequestf("导入预览编号无效")
	}
	raw, err := s.cache.Get(ctx, s.key(id)).Result()
	if err == redis.Nil {
		return nil, "", errcode.StaleDataf("导入预览已过期或已使用，请重新上传")
	}
	if err != nil {
		return nil, "", errcode.Internalf("导入预览暂不可用").Wrap(err)
	}
	var v stored
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil, "", errcode.Internalf("导入预览损坏，请重新上传")
	}
	if v.UserID != a.UserID || v.Capability != sp.Capability {
		return nil, "", errcode.NotFoundf("导入预览不存在")
	}
	if time.Now().After(v.ExpiresAt) {
		return nil, "", errcode.StaleDataf("导入预览已过期，请重新上传")
	}
	return &v, raw, nil
}

func (s *Service) discard(ctx context.Context, sp spec, id string) error {
	v, raw, err := s.load(ctx, sp, id)
	if err != nil {
		return err
	}
	_, err = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[2],KEYS[1]);end;return 1`).Run(ctx, s.cache.Client, []string{s.key(id), s.cache.Key("import_owner", capability.ID(v.UserID))}, raw).Result()
	if err != nil {
		return errcode.Internalf("取消导入预览暂不可用").Wrap(err)
	}
	return nil
}

func (s *Service) fingerprint(ctx context.Context, sp spec, v *stored, p *capability.Preview) (string, error) {
	mac := hmac.New(sha256.New, v.Salt)
	data, _ := json.Marshal(struct {
		Rows    []inputRow
		Preview *capability.Preview
	}{v.Rows, p})
	_, _ = mac.Write(data)
	// Passwords never enter the preview or stored arguments. Binding their
	// private dependency with a per-upload HMAC still detects a changed default.
	if sp.Capability == "system.user.create" {
		var value string
		if err := dbx.DBFrom(ctx, s.db).Table("sys_config").Where("config_key=? AND status='0' AND del_flag='0'", "sys.user.initPassword").Pluck("config_value", &value).Error; err != nil {
			return "", err
		}
		_, _ = mac.Write([]byte(value))
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) confirm(ctx context.Context, sp spec, in ConfirmInput) (Result, error) {
	v, raw, err := s.load(ctx, sp, in.ConfirmationID)
	if err != nil {
		return Result{}, err
	}
	if !v.Valid || len(v.Rows) == 0 || len(v.Rows) > s.maxRows {
		return Result{}, errcode.BadRequestf("请修正所有校验错误后重新上传")
	}
	if !hmac.Equal([]byte(in.Fingerprint), []byte(v.Fingerprint)) {
		return Result{}, errcode.StaleDataf("确认内容与预览不一致，请重新上传")
	}
	// Compare-and-delete is atomic on Redis 6.0 and only consumes this owner's
	// exact snapshot. Duplicate confirms cannot race into two imports.
	claimed, e := redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[2],KEYS[1]);return 1 end;return 0`).Run(ctx, s.cache.Client, []string{s.key(in.ConfirmationID), s.cache.Key("import_owner", capability.ID(v.UserID))}, raw).Int()
	if e != nil {
		return Result{}, errcode.Internalf("导入确认暂不可用").Wrap(e)
	}
	if claimed != 1 {
		return Result{}, errcode.StaleDataf("导入预览已使用或过期，请重新上传")
	}
	// Lock all authorization and referenced tables, including absent-row
	// phantoms. The executor already owns the outer transaction here.
	if err = s.changesets.LockEntities(ctx, []string{"demo_item", "sys_config", "sys_dict_type", "sys_dict_data", "sys_menu", "sys_role", "sys_role_menu", "sys_user", "sys_user_perm", "sys_user_role"}); err != nil {
		return Result{}, err
	}
	a, _ := capability.ActorFrom(ctx)
	identity := service.NewIdentity(dbx.DBFrom(ctx, s.db))
	user, err := identity.User(ctx, a.UserID)
	if err != nil || user.Status != "0" {
		return Result{}, errcode.Unauthorizedf("账户已停用或不存在")
	}
	_, perms, err := identity.RolesAndPermissions(ctx, a.UserID)
	if err != nil {
		return Result{}, err
	}
	fresh := *a
	fresh.Perms = perms
	fresh.UserName = user.UserName
	fresh.ApprovalMode = "diff_confirmed"
	ctx = capability.WithActor(ctx, &fresh)
	if _, err = s.authorize(ctx, sp); err != nil {
		return Result{}, err
	}
	p, errors, err := s.previewRows(ctx, sp, v.Rows)
	if err != nil {
		return Result{}, err
	}
	if len(errors) > 0 {
		return Result{}, errcode.StaleDataf("数据或规则已变化，请重新上传并预览").WithDetail(errors)
	}
	fingerprint, err := s.fingerprint(ctx, sp, v, p)
	if err != nil {
		return Result{}, err
	}
	if !hmac.Equal([]byte(fingerprint), []byte(v.Fingerprint)) {
		return Result{}, errcode.StaleDataf("数据或规则已变化，请重新上传并预览")
	}
	fresh.ChangesetID, err = s.changesets.Begin(ctx, &fresh, "批量导入"+sp.Title)
	if err != nil {
		return Result{}, err
	}
	for _, row := range v.Rows {
		if _, err = capability.InvokeJSON(ctx, sp.Capability, row.Arguments); err != nil {
			return Result{}, errcode.BadRequestf("第 %d 行执行失败，整批已回滚：%s", row.Number, errcode.From(err).Msg).Wrap(err)
		}
	}
	return Result{Affected: len(v.Rows), ChangesetID: fresh.ChangesetID}, nil
}
