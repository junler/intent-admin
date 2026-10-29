package auth

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

func (s *Service) MountPublic(g *gin.RouterGroup) {
	g.POST("/login", s.rateLimit("credentials", 20), s.login)
	g.POST("/register", s.rateLimit("credentials", 20), s.register)
	g.GET("/captchaImage", s.rateLimit("captcha", 60), s.captchaImage)
}
func (s *Service) MountProtected(g *gin.RouterGroup) {
	g.POST("/logout", s.logout)
	g.GET("/getInfo", s.getInfo)
	g.GET("/getRouters", s.getRouters)
	g.POST("/refreshToken", s.refreshToken)
}

func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		fields := strings.Fields(c.GetHeader("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			response.Abort(c, errcode.Unauthorizedf("请先登录"))
			return
		}
		id, session, err := s.loadSession(c.Request.Context(), fields[1])
		if err != nil {
			response.Abort(c, err)
			return
		}
		user, err := s.identity.User(c.Request.Context(), session.UserID)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user.Status != "0") {
			_ = s.cache.Del(c.Request.Context(), s.sessionKey(id)).Err()
			response.Abort(c, errcode.Unauthorizedf("用户已停用或不存在，请联系管理员"))
			return
		}
		if err != nil {
			response.Abort(c, err)
			return
		}
		snapshot, err := s.permissions(c.Request.Context(), user.UserID)
		if err != nil {
			response.Abort(c, err)
			return
		}
		actor := &capability.Actor{Type: capability.ActorHuman, Source: capability.SourceHTTP, UserID: user.UserID, UserName: user.UserName, Perms: snapshot.Permissions, MaxRisk: capability.RiskCritical, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), ApprovalMode: "manual"}
		c.Set(sessionContextKey, id)
		c.Set(userContextKey, user)
		c.Request = c.Request.WithContext(capability.WithActor(c.Request.Context(), actor))
		c.Next()
	}
}

type loginInput struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	Code           string `json:"code"`
	UUID           string `json:"uuid"`
	Nickname       string `json:"nickName"`
	LegacyNickname string `json:"nickname"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{1,29}$`)

func bindInput(c *gin.Context) (loginInput, bool) {
	var in loginInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if c.ShouldBindJSON(&in) != nil {
		response.Fail(c, errcode.BadRequestf("请求格式错误"))
		return in, false
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Nickname == "" {
		in.Nickname = in.LegacyNickname
	}
	if !usernamePattern.MatchString(in.Username) || len(in.Password) == 0 || len(in.Password) > 72 || len(in.Code) > 16 || len(in.UUID) > 64 {
		response.Fail(c, errcode.BadRequestf("请输入正确的用户名和密码（密码最多 72 字节）"))
		return in, false
	}
	return in, true
}
func (s *Service) login(c *gin.Context) {
	in, ok := bindInput(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := s.checkLock(ctx, in.Username); err != nil {
		response.Fail(c, err)
		return
	}
	captcha, _, err := s.settings(ctx)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if err = s.checkCaptcha(ctx, captcha, in.UUID, in.Code); err != nil {
		response.Fail(c, err)
		return
	}
	user, err := s.identity.FindUser(ctx, in.Username)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		response.Fail(c, err)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = nil
	}
	passwordOK := false
	if user == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
	} else {
		passwordOK = verifyPassword(user.Password, in.Password)
	}
	if !passwordOK || user == nil || user.Status != "0" {
		s.recordLogin(ctx, in.Username, c.ClientIP(), c.Request.UserAgent(), "1", "用户名或密码错误或用户已停用")
		response.Fail(c, s.failed(ctx, in.Username))
		return
	}
	if err = s.cache.Del(ctx, s.failureKey(in.Username)).Err(); err != nil {
		response.Fail(c, err)
		return
	}
	if err = s.InvalidatePermissions(ctx, user.UserID); err != nil {
		response.Fail(c, err)
		return
	}
	if err = s.identity.UpdateLogin(ctx, user.UserID, c.ClientIP()); err != nil {
		response.Fail(c, err)
		return
	}
	token, err := s.issueSession(ctx, user, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Fail(c, err)
		return
	}
	s.recordLogin(ctx, user.UserName, c.ClientIP(), c.Request.UserAgent(), "0", "登录成功")
	c.Header("Cache-Control", "no-store")
	response.OK(c, map[string]string{"token": token})
}
func (s *Service) register(c *gin.Context) {
	in, ok := bindInput(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	captcha, enabled, err := s.settings(ctx)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !enabled {
		response.Fail(c, errcode.Forbiddenf("当前不允许注册"))
		return
	}
	if utf8.RuneCountInString(in.Password) < 6 || utf8.RuneCountInString(in.Nickname) > 30 {
		response.Fail(c, errcode.BadRequestf("密码至少 6 位，昵称最多 30 字"))
		return
	}
	if err = s.checkCaptcha(ctx, captcha, in.UUID, in.Code); err != nil {
		response.Fail(c, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		response.Fail(c, err)
		return
	}
	nickname := strings.TrimSpace(in.Nickname)
	if nickname == "" {
		nickname = in.Username
	}
	_, err = s.identity.Register(ctx, in.Username, nickname, string(hash))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
func (s *Service) logout(c *gin.Context) {
	id, _ := c.Get(sessionContextKey)
	if err := s.cache.Del(c.Request.Context(), s.sessionKey(id.(string))).Err(); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
func (s *Service) getInfo(c *gin.Context) {
	raw, _ := c.Get(userContextKey)
	user := raw.(*model.User)
	snapshot, err := s.permissions(c.Request.Context(), user.UserID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, map[string]any{"user": user, "roles": snapshot.Roles, "permissions": snapshot.Permissions, "aiMaxRisk": s.effectiveRisk(user)})
}
func (s *Service) getRouters(c *gin.Context) {
	actor, _ := capability.ActorFrom(c.Request.Context())
	menus, err := s.identity.Menus(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, BuildRoutes(menus, actor.Perms))
}
func (s *Service) refreshToken(c *gin.Context) {
	id, _ := c.Get(sessionContextKey)
	ttl, err := s.refresh(c.Request.Context(), id.(string))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, map[string]int64{"expiresIn": int64(ttl.Seconds())})
}
