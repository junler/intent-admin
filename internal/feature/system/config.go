package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/secret"
	svc "github.com/junler/intent-admin/internal/service/system"
)

type configUndoRow struct {
	Payload  undo.Payload `json:"payload"`
	VaultRef string       `json:"vaultRef,omitempty"`
}
type configUndoData struct {
	Rows []configUndoRow `json:"configRows"`
}
type configValuePair struct{ Before, After string }

func configCipher(s *Service) (*secret.Cipher, error) {
	key := sha256.Sum256([]byte("intent-admin:config-vault:" + s.Cfg.JWT.Secret))
	return secret.New(hex.EncodeToString(key[:]))
}
func configVault(s *Service, ctx context.Context, before, after string) (string, error) {
	if !capability.RecordsHistory() {
		return "", nil
	}
	cipher, err := configCipher(s)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(configValuePair{Before: before, After: after})
	encoded, err := cipher.Encrypt(string(raw))
	if err != nil {
		return "", err
	}
	ref := uuid.NewString()
	days := s.Cfg.Changeset.RetainDays
	if days <= 0 {
		days = 90
	}
	err = s.Cache.Set(ctx, s.Cache.Key("config_vault", ref), encoded, time.Duration(days)*24*time.Hour).Err()
	return ref, err
}
func configUnvault(s *Service, ctx context.Context, ref string) (configValuePair, error) {
	var pair configValuePair
	encoded, err := s.Cache.Get(ctx, s.Cache.Key("config_vault", ref)).Result()
	if err != nil {
		return pair, errcode.Conflictf("凭据参数的回滚保护已过期，请人工重新设置")
	}
	cipher, err := configCipher(s)
	if err != nil {
		return pair, err
	}
	raw, err := cipher.Decrypt(encoded)
	if err != nil {
		return pair, errcode.Conflictf("凭据参数回滚保护无法解密")
	}
	err = json.Unmarshal([]byte(raw), &pair)
	return pair, err
}
func cloneFields(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func recordResource(ctx context.Context, s *Service, r svc.Resource, changes []svc.Change) error {
	if r.Name != "config" {
		record(ctx, r.Table, changes)
		return nil
	}
	safe := make([]svc.Change, len(changes))
	custom := configUndoData{}
	hasVault := false
	for i, ch := range changes {
		safe[i] = ch
		safe[i].Before = cloneFields(ch.Before)
		safe[i].After = cloneFields(ch.After)
		ref := ""
		if ch.SecretConfig {
			if value, present := ch.After["config_value"]; present {
				before := ""
				if ch.Before["config_value"] != nil {
					before = fmt.Sprint(ch.Before["config_value"])
				}
				var err error
				ref, err = configVault(s, ctx, before, fmt.Sprint(value))
				if err != nil {
					return err
				}
				hasVault = true
			}
			delete(safe[i].Before, "config_value")
			delete(safe[i].After, "config_value")
			if ch.Op == "update" && ref != "" {
				safe[i].Before["config_key"] = ch.ConfigKey
				safe[i].After["config_key"] = ch.ConfigKey
			}

		}
		custom.Rows = append(custom.Rows, configUndoRow{Payload: undo.Payload{Entity: r.Table, ID: capability.ID(ch.ID), Op: capability.Op(ch.Op), Before: safe[i].Before, After: safe[i].After}, VaultRef: ref})
		if ch.Op == "delete" {
			custom.Rows[len(custom.Rows)-1].Payload.Op = capability.OpDelete
		}
	}
	record(ctx, r.Table, safe)
	if hasVault {
		capability.RecorderFrom(ctx).SetUndo(custom)
	}
	return nil
}
func undoConfig(s *Service, ctx context.Context, data configUndoData) error {
	inverse := configUndoData{}
	for i := len(data.Rows) - 1; i >= 0; i-- {
		row := data.Rows[i]
		p := row.Payload
		if p.Entity != "sys_config" {
			return errcode.BadRequestf("参数回滚实体不匹配")
		}
		if err := AuthorizeUndo(s)(ctx, p); err != nil {
			return err
		}
		var current map[string]any
		if err := dbx.DBFrom(ctx, s.DB).Table("sys_config").Where("config_id=?", p.ID).Take(&current).Error; err != nil {
			return err
		}
		patch := cloneFields(p.Before)
		invBefore, invAfter := cloneFields(p.After), cloneFields(p.Before)
		invVault := ""
		if row.VaultRef != "" {
			pair, err := configUnvault(s, ctx, row.VaultRef)
			if err != nil {
				return err
			}
			if fmt.Sprint(current["config_value"]) != pair.After {
				return errcode.Conflictf("凭据参数已被后续修改，不能覆盖")
			}
			if p.Op != capability.OpCreate {
				patch["config_value"] = pair.Before
			}
			invVault, err = configVault(s, ctx, pair.After, pair.Before)
			if err != nil {
				return err
			}
		}
		switch p.Op {
		case capability.OpCreate:
			// Keep the value in its hidden row so the inverse needs no secret snapshot.
			patch = map[string]any{"del_flag": "2"}
			invBefore = map[string]any{"del_flag": "0"}
			invAfter = map[string]any{"del_flag": "2"}
			// The value itself did not change when hiding the created parameter.
			if row.VaultRef != "" {
				pair, err := configUnvault(s, ctx, row.VaultRef)
				if err != nil {
					return err
				}
				invVault, err = configVault(s, ctx, pair.After, pair.After)
				if err != nil {
					return err
				}
			}
		case capability.OpDelete:
			patch["del_flag"] = "0"
			invBefore = map[string]any{"del_flag": "2"}
			invAfter = map[string]any{"del_flag": "0"}
		case capability.OpUpdate:
		default:
			return errcode.BadRequestf("参数回滚操作无效")
		}
		if len(patch) > 0 {
			if err := dbx.DBFrom(ctx, s.DB).Table("sys_config").Where("config_id=?", p.ID).Updates(patch).Error; err != nil {
				return err
			}
		}
		inverse.Rows = append(inverse.Rows, configUndoRow{Payload: undo.Payload{Entity: "sys_config", ID: p.ID, Op: capability.OpUpdate, Before: invBefore, After: invAfter}, VaultRef: invVault})
	}
	capability.RecorderFrom(ctx).SetUndo(inverse)
	dbx.AfterCommit(ctx, func() { _ = s.Invalidate(context.Background(), "sys_config", "") })
	return nil
}
