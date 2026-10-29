package changeset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ItemIDs is nil for all remaining items; an explicit empty selection is invalid.
// PreviewHash binds an explicit selection to the state reviewed by the caller.
type RollbackIn struct {
	ID          string  `json:"id" uri:"id" validate:"required"`
	ItemIDs     []int64 `json:"itemIds"`
	PreviewHash string  `json:"previewHash,omitempty"`
}

func rollbackCandidate(item Item) bool {
	return item.Op != capability.OpSkip && item.Status != "rolled_back"
}

func selectItems(set *Set, ids []int64) (map[int64]bool, error) {
	selected := map[int64]bool{}
	byID := map[int64]Item{}
	for _, item := range set.Items {
		byID[item.ID] = item
	}
	if ids == nil {
		for _, item := range set.Items {
			if rollbackCandidate(item) {
				selected[item.ID] = true
			}
		}
	} else {
		if len(ids) == 0 {
			return nil, errcode.BadRequestf("请至少选择一个变更条目")
		}
		if len(ids) > 10000 {
			return nil, errcode.BadRequestf("单次选择条目过多")
		}
		for _, id := range ids {
			item, ok := byID[id]
			if !ok || id <= 0 {
				return nil, errcode.BadRequestf("条目 %d 不属于当前变更集", id)
			}
			if selected[id] {
				return nil, errcode.BadRequestf("条目 %d 重复选择", id)
			}
			if item.Op == capability.OpSkip {
				return nil, errcode.BadRequestf("条目 %d 未执行写入，无需回滚", id)
			}
			selected[id] = true
		}
	}
	for _, item := range set.Items {
		if !selected[item.ID] || item.UndoGroup == "" {
			continue
		}
		peers := []int64{}
		split := false
		rolled, pending := 0, 0
		for _, peer := range set.Items {
			if peer.UndoGroup == item.UndoGroup && peer.Op != capability.OpSkip {
				peers = append(peers, peer.ID)
				if peer.Status == "rolled_back" {
					rolled++
				} else {
					pending++
				}
				if !selected[peer.ID] {
					split = true
				}
			}
		}
		if split {
			return nil, errcode.Conflictf("条目使用原子补偿，必须整组选中").WithDetail(map[string]any{"undoGroup": item.UndoGroup, "requiredItemIds": peers})
		}
		if rolled > 0 && pending > 0 {
			return nil, errcode.Conflictf("原子补偿组的历史状态不一致，请核对后处理").WithDetail(map[string]any{"requiredItemIds": peers})
		}
	}
	return selected, nil
}

// rollbackUnits follows reverse execution order, keeping every custom undo group atomic.
func rollbackUnits(set *Set, selected map[int64]bool) [][]Item {
	units := [][]Item{}
	seen := map[string]bool{}
	for n := len(set.Items) - 1; n >= 0; n-- {
		item := set.Items[n]
		if !selected[item.ID] {
			continue
		}
		if item.UndoGroup == "" {
			units = append(units, []Item{item})
			continue
		}
		if seen[item.UndoGroup] {
			continue
		}
		seen[item.UndoGroup] = true
		peers := []Item{}
		for _, peer := range set.Items {
			if selected[peer.ID] && peer.UndoGroup == item.UndoGroup {
				peers = append(peers, peer)
			}
		}
		units = append(units, peers)
	}
	return units
}

func conflictingFields(item Item, current map[string]any) []string {
	out := []string{}
	for field, value := range fields(item.After) {
		if !capability.IsSensitive(field) && !same(current[field], value) {
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out
}
func rollbackDiff(item Item, current map[string]any, actual bool) []capability.FieldDiff {
	before, after := fields(item.Before), fields(item.After)
	keys := []string{}
	for k := range before {
		keys = append(keys, k)
	}
	if item.Op == capability.OpCreate {
		for k := range after {
			if _, ok := before[k]; !ok {
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	out := []capability.FieldDiff{}
	for _, k := range keys {
		value := after[k]
		if actual {
			value = current[k]
		}
		if !same(value, before[k]) {
			out = append(out, capability.FieldDiff{Field: k, Before: value, After: before[k]})
		}
	}
	return out
}

func (s *Service) PreviewRollback(ctx context.Context, id string) (*capability.Preview, error) {
	return s.PreviewRollbackSelected(ctx, RollbackIn{ID: id})
}
func (s *Service) PreviewRollbackSelected(ctx context.Context, in RollbackIn) (*capability.Preview, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Can("system:changeset:rollback") {
		return nil, errcode.Forbiddenf("没有回滚权限")
	}
	set, err := s.load(ctx, in.ID, false)
	if err != nil {
		return nil, err
	}
	selected, err := selectItems(set, in.ItemIDs)
	if err != nil {
		return nil, err
	}
	return s.previewSelection(ctx, set, selected)
}
func (s *Service) previewSelection(ctx context.Context, set *Set, selected map[int64]bool) (*capability.Preview, error) {
	selection := &capability.PreviewSelection{ItemIDs: []int64{}}
	for _, item := range set.Items {
		if item.Op != capability.OpSkip {
			selection.Total++
			if item.Status != "rolled_back" {
				selection.Remaining++
			}
		}
		if selected[item.ID] {
			selection.ItemIDs = append(selection.ItemIDs, item.ID)
			if item.Status == "rolled_back" {
				selection.AlreadyRolledBack++
			}
		}
	}
	sort.Slice(selection.ItemIDs, func(i, j int) bool { return selection.ItemIDs[i] < selection.ItemIDs[j] })
	p := &capability.Preview{Summary: fmt.Sprintf("回滚变更集 %s 中选定的 %d 项", set.ID, len(selected)), Reversible: true, Selection: selection}
	virtual := map[string]map[string]any{}
	// Hash all selected changes, even beyond the UI preview's enumeration limit.
	all := []capability.Change{}
	states := []any{}
	for _, unit := range rollbackUnits(set, selected) {
		unitChanges := []capability.Change{}
		unitCurrent := map[int64]map[string]any{}
		unitErr := error(nil)
		for _, item := range unit {
			change := capability.Change{ItemID: item.ID, Entity: item.Entity, ID: item.EntityID, Label: item.EntityLabel, Op: capability.OpUpdate, Status: "ready"}
			if item.Op == capability.OpCreate {
				change.Op = capability.OpDelete
			}
			states = append(states, []any{item.ID, item.Status, item.RollbackID})
			if item.Status == "rolled_back" {
				change.Op = capability.OpSkip
				change.Status = "already_rolled_back"
				change.Reason = "此条目已回滚，不会重复执行"
				unitChanges = append(unitChanges, change)
				continue
			}
			checkErr := s.check(ctx, item, false)
			key := item.Entity + "#" + item.EntityID
			current, exists := virtual[key]
			if !exists {
				current, _ = s.snapshot(ctx, item.Entity, item.EntityID, false)
			}
			unitCurrent[item.ID] = current
			if exists && (checkErr == nil || errcode.From(checkErr).Code == errcode.StaleData) {
				checkErr = nil
				if len(conflictingFields(item, current)) > 0 {
					checkErr = errcode.New(errcode.StaleData, "当前字段与变更记录不一致")
				}
			}
			if checkErr != nil {
				change.Op = capability.OpSkip
				change.Status = "conflict"
				change.Reason = errcode.From(checkErr).Msg
				if errcode.From(checkErr).Code == errcode.StaleData {
					change.ConflictFields = conflictingFields(item, current)
					change.Fields = rollbackDiff(item, current, true)
				}
				if unitErr == nil {
					unitErr = checkErr
				}
			} else {
				change.Fields = rollbackDiff(item, current, false)
			}
			unitChanges = append(unitChanges, change)
		}
		if unitErr != nil {
			for i := range unitChanges {
				if unitChanges[i].Status == "ready" {
					unitChanges[i].Op = capability.OpSkip
					unitChanges[i].Status = "blocked"
					unitChanges[i].Reason = "同组条目无法回滚：" + errcode.From(unitErr).Msg
				}
			}
			p.Warn("冲突条目及其原子补偿组不会执行；其余所选条目会独立回滚。")
		} else {
			for _, item := range unit {
				if item.Status == "rolled_back" {
					continue
				}
				current := unitCurrent[item.ID]
				if current == nil {
					current = map[string]any{}
				}
				next := map[string]any{}
				for k, v := range current {
					next[k] = v
				}
				if item.Op == capability.OpCreate {
					next = map[string]any{}
				} else {
					for k, v := range fields(item.Before) {
						next[k] = v
					}
				}
				virtual[item.Entity+"#"+item.EntityID] = next
				selection.Remaining--
			}
		}
		for _, change := range unitChanges {
			all = append(all, change)
			p.AddChange(change)
		}
	}
	digest := sha256.Sum256(raw([]any{set.ID, selection, states, all}))
	p.ConfirmationHash = hex.EncodeToString(digest[:])
	return p, nil
}

func (s *Service) Rollback(ctx context.Context, id string) (*Set, error) {
	return s.RollbackSelected(ctx, RollbackIn{ID: id})
}
func (s *Service) RollbackSelected(ctx context.Context, in RollbackIn) (*Set, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Can("system:changeset:rollback") {
		return nil, errcode.Forbiddenf("没有回滚权限")
	}
	inverseID := ""
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		original, e := s.load(txctx, in.ID, false)
		if e != nil {
			return e
		}
		selected, e := selectItems(original, in.ItemIDs)
		if e != nil {
			return e
		}
		entities := []string{}
		for _, item := range original.Items {
			if selected[item.ID] && item.Status != "rolled_back" {
				if entry, ok := capability.Lookup(item.Capability); ok && entry.Reversible && entry.HasUndo() {
					entities = append(entities, item.Entity)
				}
			}
		}
		if e = s.LockEntities(txctx, entities); e != nil {
			return e
		}
		if e = s.lockRollbackSets(txctx, original); e != nil {
			return e
		}
		original, e = s.load(txctx, in.ID, true)
		if e != nil {
			return e
		}
		selected, e = selectItems(original, in.ItemIDs)
		if e != nil {
			return e
		}
		pending := 0
		for _, item := range original.Items {
			if selected[item.ID] && item.Status != "rolled_back" {
				pending++
			}
		}
		// Completed requests are idempotent, including a retry with its old proof.
		if pending == 0 {
			for _, item := range original.Items {
				if selected[item.ID] && item.RollbackID != "" {
					inverseID = item.RollbackID
				}
			}
			return nil
		}
		preview, e := s.previewSelection(txctx, original, selected)
		if e != nil {
			return e
		}
		if in.ItemIDs != nil && in.PreviewHash == "" {
			return errcode.StaleDataf("请先预览选定条目的回滚结果")
		}
		if in.PreviewHash != "" && in.PreviewHash != preview.ConfirmationHash {
			return errcode.StaleDataf("所选条目或数据已变化，请重新预览")
		}
		if original.RollbackBaseStatus == "" {
			original.RollbackBaseStatus = original.Status
			if e = dbx.DBFrom(txctx, s.db).Model(&Set{}).Where("changeset_id=?", in.ID).Update("rollback_base_status", original.Status).Error; e != nil {
				return e
			}
		}
		rec := capability.RecorderFrom(txctx)
		actions := []rollbackAction{}
		stateChanges := []rollbackState{}
		for _, unit := range rollbackUnits(original, selected) {
			if unit[0].Status == "rolled_back" {
				continue
			}
			oneRecords := []capability.RecordedItem{}
			oneActions := []rollbackAction{}
			itemErr := dbx.NewTxRunner(s.db)(txctx, func(oneCtx context.Context) error {
				for _, peer := range unit {
					if e := s.check(oneCtx, peer, true); e != nil {
						return e
					}
				}
				entry, _ := capability.Lookup(unit[0].Capability)
				payload := json.RawMessage(nil)
				snapshots := map[int64]map[string]any{}
				for _, peer := range unit {
					current, e := s.snapshot(oneCtx, peer.Entity, peer.EntityID, false)
					if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
						return e
					}
					snapshots[peer.ID] = current
					if len(peer.Undo) > 0 {
						payload = peer.Undo
					}
				}
				if len(payload) == 0 {
					return errcode.Conflictf("缺少回滚数据")
				}
				local := capability.NewRecorder()
				if e := entry.Undo(capability.WithRecorder(oneCtx, local), payload); e != nil {
					return e
				}
				for _, peer := range unit {
					before := snapshots[peer.ID]
					after, e := s.snapshot(oneCtx, peer.Entity, peer.EntityID, false)
					if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
						return e
					}
					inverse := capability.RecordedItem{Entity: peer.Entity, ID: peer.EntityID, Label: peer.EntityLabel}
					if e != nil {
						inverse.Op = capability.OpDelete
						inverse.Before = safeFields(before)
					} else if before == nil {
						inverse.Op = capability.OpCreate
						inverse.After = safeFields(after)
					} else {
						inverse.Op = capability.OpUpdate
						inverse.Before = map[string]any{}
						inverse.After = map[string]any{}
						for k, v := range before {
							if !capability.IsSensitive(k) && !same(v, after[k]) {
								inverse.Before[k] = v
								inverse.After[k] = after[k]
							}
						}
					}
					oneRecords = append(oneRecords, inverse)
					if s.Invalidate != nil {
						entity, key := peer.Entity, peer.EntityID
						dbx.AfterCommit(oneCtx, func() {
							if e := s.Invalidate(context.WithoutCancel(ctx), entity, key); e != nil {
								slog.Error("回滚后缓存清理失败", "error", e)
							}
						})
					}
				}
				if payload, ok := local.UndoOverride(); ok {
					oneActions = append(oneActions, rollbackAction{Capability: entry.Name, Guard: entry.Name, Payload: payload})
				} else {
					for _, inverse := range oneRecords {
						oneActions = append(oneActions, rollbackAction{Capability: "system.changeset.rollback", Guard: entry.Name, Payload: raw(undo.Payload{Entity: inverse.Entity, ID: inverse.ID, Op: inverse.Op, Before: inverse.Before, After: inverse.After})})
					}
				}
				return nil
			})
			if itemErr == nil && inverseID == "" {
				if a.ChangesetID == in.ID {
					return errcode.Conflictf("回滚变更集不能与原变更集相同")
				}
				if a.ChangesetID == "" {
					a.ChangesetID, e = s.Begin(txctx, a, "回滚 #"+in.ID)
					if e != nil {
						return e
					}
				}
				inverseID = a.ChangesetID
				if e = dbx.DBFrom(txctx, s.db).Model(&Set{}).Where("changeset_id=?", inverseID).Update("rollback_of", in.ID).Error; e != nil {
					return e
				}
			}
			now := time.Now().Truncate(time.Microsecond)
			for _, peer := range unit {
				before := itemState(peer)
				updates := map[string]any{}
				if itemErr != nil {
					code := errcode.From(itemErr)
					conflicts := []string{}
					if code.Code == errcode.StaleData {
						current, _ := s.snapshot(txctx, peer.Entity, peer.EntityID, false)
						conflicts = conflictingFields(peer, current)
					}
					updates = map[string]any{"status": "failed", "error_msg": code.Msg, "error_code": code.Code, "conflict_fields": raw(conflicts)}
				} else {
					updates = map[string]any{"status": "rolled_back", "error_msg": "", "error_code": 0, "conflict_fields": nil, "rolled_back_at": now, "rolled_back_by": a.UserName, "rollback_changeset_id": inverseID}
					after := rollbackItemState{Status: "rolled_back", RolledBackAt: &now, RolledBackBy: a.UserName, RollbackID: inverseID}
					stateChanges = append(stateChanges, rollbackState{ChangesetID: in.ID, ItemID: peer.ID, Before: before, After: after})
				}
				if e = dbx.DBFrom(txctx, s.db).Model(&Item{}).Where("item_id=? AND changeset_id=?", peer.ID, in.ID).Updates(updates).Error; e != nil {
					return e
				}
			}
			if itemErr == nil {
				for _, record := range oneRecords {
					recordInto(rec, record)
				}
				actions = append(actions, oneActions...)
			}
		}
		if len(actions) > 0 {
			rec.SetUndo(rollbackUndo{Actions: actions, States: stateChanges})
		}
		return s.refreshRollbackStatus(txctx, in.ID)
	})
	if err != nil {
		return nil, err
	}
	result, err := s.load(ctx, in.ID, false)
	if result != nil {
		result.RollbackID = inverseID
	}
	return result, err
}

// Lock related history rows in a stable order before invoking inverse metadata changes.
func (s *Service) lockRollbackSets(ctx context.Context, set *Set) error {
	ids := map[string]bool{set.ID: true}
	for _, item := range set.Items {
		var data rollbackUndo
		if json.Unmarshal(item.Undo, &data) == nil {
			for _, state := range data.States {
				ids[state.ChangesetID] = true
			}
		}
	}
	ordered := []string{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		var row Set
		if err := dbx.DBFrom(ctx, s.db).Clauses(clause.Locking{Strength: "UPDATE"}).Where("changeset_id=?", id).First(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) refreshRollbackStatus(ctx context.Context, id string) error {
	var set Set
	db := dbx.DBFrom(ctx, s.db)
	if err := db.Where("changeset_id=?", id).First(&set).Error; err != nil {
		return err
	}
	items := []Item{}
	if err := db.Where("changeset_id=?", id).Find(&items).Error; err != nil {
		return err
	}
	total, rolled, failed := 0, 0, 0
	var latest *time.Time
	by := ""
	for _, item := range items {
		if item.Op == capability.OpSkip {
			continue
		}
		total++
		if item.Status == "rolled_back" {
			rolled++
			if item.RolledBackAt != nil && (latest == nil || item.RolledBackAt.After(*latest)) {
				latest = item.RolledBackAt
				by = item.RolledBackBy
			}
		}
		if item.Status == "failed" || item.Status == "skipped" {
			failed++
		}
	}
	status := set.RollbackBaseStatus
	if status == "" || status == "rolled_back" || status == "rollback_failed" || status == "partially_rolled_back" {
		status = "applied"
	}
	if total > 0 && rolled == total {
		status = "rolled_back"
	} else if failed > 0 {
		status = "rollback_failed"
	} else if rolled > 0 {
		status = "partially_rolled_back"
	}
	return db.Model(&Set{}).Where("changeset_id=?", id).Updates(map[string]any{"status": status, "rolled_back_at": latest, "rolled_back_by": by}).Error
}

type rollbackItemState struct {
	Status         string          `json:"status"`
	ErrorMsg       string          `json:"errorMsg,omitempty"`
	ErrorCode      int             `json:"errorCode,omitempty"`
	ConflictFields json.RawMessage `json:"conflictFields,omitempty"`
	RolledBackAt   *time.Time      `json:"rolledBackAt,omitempty"`
	RolledBackBy   string          `json:"rolledBackBy,omitempty"`
	RollbackID     string          `json:"rollbackId,omitempty"`
}
type rollbackState struct {
	ChangesetID string            `json:"changesetId"`
	ItemID      int64             `json:"itemId"`
	Before      rollbackItemState `json:"before"`
	After       rollbackItemState `json:"after"`
}

func itemState(item Item) rollbackItemState {
	return rollbackItemState{Status: item.Status, ErrorMsg: item.ErrorMsg, ErrorCode: item.ErrorCode, ConflictFields: item.ConflictFields, RolledBackAt: item.RolledBackAt, RolledBackBy: item.RolledBackBy, RollbackID: item.RollbackID}
}
func (s *Service) undoRollbackStates(ctx context.Context, states []rollbackState) ([]rollbackState, error) {
	inverse := []rollbackState{}
	sets := map[string]bool{}
	for _, state := range states {
		var item Item
		db := dbx.DBFrom(ctx, s.db)
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("changeset_id=? AND item_id=?", state.ChangesetID, state.ItemID).First(&item).Error; err != nil {
			return nil, err
		}
		if !sameRollbackState(itemState(item), state.After) {
			return nil, errcode.StaleDataf("原变更条目状态已变化，不能恢复回滚状态")
		}
		before := state.Before
		var link any
		if before.RollbackID != "" {
			link = before.RollbackID
		}
		updates := map[string]any{"status": before.Status, "error_msg": before.ErrorMsg, "error_code": before.ErrorCode, "conflict_fields": before.ConflictFields, "rolled_back_at": before.RolledBackAt, "rolled_back_by": before.RolledBackBy, "rollback_changeset_id": link}
		if err := db.Model(&Item{}).Where("item_id=?", item.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
		inverse = append(inverse, rollbackState{ChangesetID: state.ChangesetID, ItemID: state.ItemID, Before: state.After, After: state.Before})
		sets[state.ChangesetID] = true
	}
	for id := range sets {
		if err := s.refreshRollbackStatus(ctx, id); err != nil {
			return nil, err
		}
	}
	return inverse, nil
}

func sameRollbackState(a, b rollbackItemState) bool {
	return a.Status == b.Status && a.ErrorMsg == b.ErrorMsg && a.ErrorCode == b.ErrorCode && same(a.ConflictFields, b.ConflictFields) && same(a.RolledBackAt, b.RolledBackAt) && a.RolledBackBy == b.RolledBackBy && a.RollbackID == b.RollbackID
}
