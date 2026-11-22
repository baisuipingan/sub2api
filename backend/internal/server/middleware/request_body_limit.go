package middleware

import (
	"net/http"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
)

// RequestBodyLimit 使用 MaxBytesReader 限制请求体大小。
func RequestBodyLimit(maxBytes int64, budgets ...*pkghttputil.BodyMemoryBudget) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(budgets) > 0 {
			ctx, release := pkghttputil.WithRequestBodyMemoryBudget(c.Request.Context(), budgets[0])
			defer release()
			c.Request = c.Request.WithContext(ctx)
		}
		// An internal request with a complete body already in memory can keep
		// that buffer after applying the same size check as a streamed request.
		if preread, ok := c.Request.Body.(*pkghttputil.PrereadBody); !ok || int64(len(preread.Bytes())) > maxBytes {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
