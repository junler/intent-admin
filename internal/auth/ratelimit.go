package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

var rateLimitScript = redis.NewScript(`
local count=redis.call('INCR',KEYS[1])
if count==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end
return count
`)

// Bound the public bcrypt and image-rendering work even when attackers rotate
// usernames. Bootstrap disables untrusted forwarding headers before ClientIP
// is used, so changing X-Forwarded-For cannot reset this bucket.
func (s *Service) rateLimit(bucket string, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		hash := sha256.Sum256([]byte(c.ClientIP()))
		key := s.cache.Key("auth_rate", bucket, hex.EncodeToString(hash[:]))
		n, err := rateLimitScript.Run(c.Request.Context(), s.cache.Client, []string{key}, time.Minute.Milliseconds()).Int()
		if err != nil {
			response.Abort(c, err)
			return
		}
		if n > limit {
			c.Header("Retry-After", "60")
			response.Abort(c, errcode.New(errcode.TooManyReq, "请求过于频繁，请稍后重试"))
			return
		}
		c.Next()
	}
}
