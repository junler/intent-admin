// Package notification delivers private workflow events to their recipients.
package notification

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/pkg/response"
)

type Service struct{ db *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{db: db} }

type Message struct {
	ID         string     `gorm:"column:notification_id;primaryKey" json:"noticeId"`
	UserID     int64      `json:"-"`
	Title      string     `gorm:"column:title" json:"noticeTitle"`
	Content    string     `gorm:"column:content" json:"noticeContent"`
	Link       string     `json:"link"`
	ReadAt     *time.Time `json:"readAt,omitempty"`
	CreateTime time.Time  `json:"createTime"`
}

func (Message) TableName() string { return "sys_notification" }

// Publish joins the workflow transaction. A rolled-back approval never leaves
// a notification claiming it succeeded. Links are always local page routes.
func (s *Service) Publish(ctx context.Context, userID int64, title, content, link string) error {
	if userID <= 0 || title == "" || !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") || strings.ContainsAny(link, "\\\r\n") {
		return errcode.BadRequestf("通知参数无效")
	}
	return dbx.DBFrom(ctx, s.db).Create(&Message{ID: uuid.NewString(), UserID: userID, Title: title, Content: content, Link: link, CreateTime: time.Now()}).Error
}

func principal(ctx context.Context) (*capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok || a.UserID <= 0 {
		return nil, errcode.Unauthorizedf("请先登录")
	}
	if a.Source != capability.SourceHTTP {
		return nil, errcode.Forbiddenf("请在页面查看个人通知")
	}
	return a, nil
}

func (s *Service) List(ctx context.Context, query paginate.Query) ([]Message, int64, error) {
	a, err := principal(ctx)
	if err != nil {
		return nil, 0, err
	}
	query.Normalize()
	q := dbx.DBFrom(ctx, s.db).Model(&Message{}).Where("user_id = ?", a.UserID)
	var count int64
	if err = q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	rows := []Message{}
	err = q.Order("create_time DESC, notification_id DESC").Scopes(query.Scope()).Find(&rows).Error
	return rows, count, err
}

func (s *Service) Read(ctx context.Context, ids []string) error {
	a, err := principal(ctx)
	if err != nil {
		return err
	}
	if len(ids) > 200 {
		return errcode.BadRequestf("通知条目过多")
	}
	for _, id := range ids {
		if _, err = uuid.Parse(id); err != nil {
			return errcode.BadRequestf("通知编号无效")
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return dbx.DBFrom(ctx, s.db).Model(&Message{}).Where("user_id = ? AND notification_id IN ? AND read_at IS NULL", a.UserID, ids).Update("read_at", time.Now()).Error
}

func (s *Service) Mount(api *gin.RouterGroup) {
	api.GET("/personal-notifications", func(c *gin.Context) {
		var query paginate.Query
		if c.ShouldBindQuery(&query) != nil || query.PageNum > 1_000_000 {
			response.Fail(c, errcode.BadRequestf("分页参数无效"))
			return
		}
		query.Normalize()
		rows, total, err := s.List(c.Request.Context(), query)
		c.Header("Cache-Control", "no-store")
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, response.Page{Rows: rows, Total: total, PageNum: query.PageNum, PageSize: query.PageSize})
	})
	api.POST("/personal-notifications/read", func(c *gin.Context) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if c.ShouldBindJSON(&in) != nil {
			response.Fail(c, errcode.BadRequestf("通知参数无效"))
			return
		}
		if err := s.Read(c.Request.Context(), in.IDs); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	})
}
