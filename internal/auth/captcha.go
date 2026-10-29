package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"

	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

const captchaTTL = 5 * time.Minute

func (s *Service) checkCaptcha(ctx context.Context, enabled bool, id, code string) error {
	if !enabled {
		return nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return errcode.BadRequestf("验证码错误或已过期")
	}
	// Atomic GET + DEL consumes the challenge on success and failure, including
	// on Redis 6.0 where GETDEL is not yet available. A replay or two
	// simultaneous login attempts can never use the same challenge twice.
	expected, err := consumeCaptchaScript.Run(ctx, s.cache.Client, []string{s.cache.Key("captcha", id)}).Text()
	if errors.Is(err, redis.Nil) {
		return errcode.BadRequestf("验证码错误或已过期")
	}
	if err != nil {
		return err
	}
	if code == "" || expected != strings.TrimSpace(code) {
		return errcode.BadRequestf("验证码错误或已过期")
	}
	return nil
}
func (s *Service) captchaImage(c *gin.Context) {
	enabled, register, err := s.settings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	data := map[string]any{"captchaEnabled": enabled, "registerEnabled": register}
	c.Header("Cache-Control", "no-store")
	if enabled {
		id := uuid.NewString()
		code, img, err := captchaPNG()
		if err != nil {
			response.Fail(c, err)
			return
		}
		if err = s.cache.Set(c.Request.Context(), s.cache.Key("captcha", id), code, captchaTTL).Err(); err != nil {
			response.Fail(c, err)
			return
		}
		data["uuid"] = id
		data["img"] = img
	}
	response.OK(c, data)
}

// consumeCaptchaScript works on Redis 6.0 and never leaves a consumed answer.
var consumeCaptchaScript = redis.NewScript(`
local value=redis.call('GET',KEYS[1])
if value then redis.call('DEL',KEYS[1]) end
return value
`)

// Use the raster digit renderer, with skew and random noise, rather than SVG
// primitives that reveal the answer directly to an XML parser.
func captchaPNG() (string, string, error) {
	driver := base64Captcha.NewDriverDigit(48, 160, 5, 0.75, 80)
	_, question, answer := driver.GenerateIdQuestionAnswer()
	item, err := driver.DrawCaptcha(question)
	if err != nil {
		return "", "", err
	}
	img := strings.TrimPrefix(item.EncodeB64string(), "data:image/png;base64,")
	return answer, img, nil
}
