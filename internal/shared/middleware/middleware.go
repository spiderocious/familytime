// Package middleware holds the cross-cutting HTTP middleware. Order is
// load-bearing; app.go says what depends on what.
package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/ids"
	"familytime/backend/internal/lib/logger"
	"familytime/backend/internal/lib/reqctx"
	"familytime/backend/internal/lib/response"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

const requestIDHeader = "X-Request-Id"

// RequestID seeds the request context. Everything that logs depends on it.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" || len(id) > 64 {
			id = ids.New(ids.Request)
		}
		c.Request = c.Request.WithContext(reqctx.WithRequestID(c.Request.Context(), id))
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

// HTTPLogger logs one line per request, after it completes.
func HTTPLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.With(c.Request.Context()).Info("http",
			"method", c.Request.Method,
			// The route pattern has bounded cardinality; the raw path does not.
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

// CORS allows the origins the policy accepts and nothing else.
func CORS(policy OriginPolicy) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && policy.Allows(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type,"+requestIDHeader)
			c.Header("Access-Control-Expose-Headers", requestIDHeader+",Retry-After")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(constants.StatusNoContent)
			return
		}
		c.Next()
	}
}

// ErrorHandler is the one place an error becomes a response body. Registered
// before the routes so it wraps them; it acts after c.Next() returns.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.With(c.Request.Context()).Error("panic in handler", "panic", recovered)
				if !c.Writer.Written() {
					renderInternal(c)
				}
			}
		}()

		c.Next()

		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}

		err := c.Errors.Last().Err
		var appErr *response.AppError
		if errors.As(err, &appErr) {
			logger.With(c.Request.Context()).Warn("handled application error",
				"code", appErr.Failure.Code,
				"status", appErr.Failure.Status,
				"rejection_reason", appErr.Failure.RejectionReason,
			)
			response.Error(c, appErr.Failure.Status, response.BodyOf(appErr.Failure))
			return
		}

		// The one count that must stay at zero: everything above is a client
		// mistake, this is ours.
		logger.With(c.Request.Context()).Error("unhandled error", "error", err.Error())
		renderInternal(c)
	}
}

func renderInternal(c *gin.Context) {
	response.Error(c, constants.StatusInternal, response.ErrorBody{
		Code:     constants.CodeInternal,
		Message:  messages.Resolve(messages.CommonInternal),
		Severity: constants.SeverityFor(constants.CodeInternal),
	})
}

// NotFound is the terminal 404 for an unmatched path, in the standard envelope.
func NotFound(c *gin.Context) {
	response.Error(c, constants.StatusNotFound, response.ErrorBody{
		Code:     constants.CodeNotFound,
		Message:  messages.Resolve(messages.CommonNotFound),
		Severity: constants.SeverityFor(constants.CodeNotFound),
	})
}
