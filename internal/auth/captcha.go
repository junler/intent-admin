package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"math/big"
	visualrand "math/rand/v2"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

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

// Render at twice the display resolution with a fixed font size and separate
// digit cells. Keep background interference light and behind the text.
func captchaPNG() (string, string, error) {
	code := make([]byte, 5)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", "", err
		}
		code[i] = '0' + byte(n.Int64())
	}
	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return "", "", err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 64, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return "", "", err
	}
	defer face.Close()
	img := image.NewRGBA(image.Rect(0, 0, 224, 72))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	for range 2 {
		phase := visualrand.Float64() * 2 * math.Pi
		for x := 0; x < 224; x++ {
			y := 36 + int(16*math.Sin(float64(x)/40+phase))
			img.SetRGBA(x, y, color.RGBA{184, 197, 213, 255})
		}
	}
	d := font.Drawer{Dst: img, Face: face}
	for i, digit := range code {
		text := string(digit)
		advance := d.MeasureString(text).Ceil()
		d.Dot = fixed.P(12+i*40+(40-advance)/2, 58+visualrand.IntN(5)-2)
		d.Src = image.NewUniform(color.RGBA{uint8(24 + visualrand.IntN(32)), 43, 77, 255})
		d.DrawString(text)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", err
	}
	return string(code), base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
