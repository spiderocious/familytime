// Package response is the only sanctioned way to write a response body.
// c.JSON in a handler is always a bug: it bypasses the envelope.
package response

import (
	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

// ErrorBody does three jobs, one field each: code branches, message displays,
// severity measures. field_errors appears on validation failures only;
// rejection_reason is an operator diagnostic and not part of the contract.
type ErrorBody struct {
	Code            constants.ErrorCode `json:"code"`
	Message         string              `json:"message"`
	Severity        constants.Severity  `json:"severity"`
	FieldErrors     map[string][]string `json:"field_errors,omitempty"`
	RejectionReason string              `json:"rejection_reason,omitempty"`
}

type Meta struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

type successEnvelope[T any] struct {
	Data T     `json:"data"`
	Meta *Meta `json:"meta,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

func OK[T any](c *gin.Context, data T) {
	c.JSON(constants.StatusOK, successEnvelope[T]{Data: data})
}

func Page[T any](c *gin.Context, data T, meta Meta) {
	c.JSON(constants.StatusOK, successEnvelope[T]{Data: data, Meta: &meta})
}

func Created[T any](c *gin.Context, data T) {
	c.JSON(constants.StatusCreated, successEnvelope[T]{Data: data})
}

// NoContent carries no body at all — parsing a 204 is a client-side error.
func NoContent(c *gin.Context) {
	c.Status(constants.StatusNoContent)
}

// Error is called by the error middleware only.
func Error(c *gin.Context, status int, body ErrorBody) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: body})
}

// AppError is what a controller hands to the error middleware. Controllers
// never render an error themselves, so envelopes cannot drift per handler.
type AppError struct {
	Failure *result.Failure
}

func (e *AppError) Error() string { return "service rejected: " + string(e.Failure.Code) }

// Bail converts a failed service result into the error the middleware renders.
func Bail(c *gin.Context, failure *result.Failure) {
	_ = c.Error(&AppError{Failure: failure})
	c.Abort()
}

// Malformed reports an unparseable body: a client error, never a 500.
func Malformed(c *gin.Context) {
	Bail(c, &result.Failure{
		Code:       constants.CodeMalformedJSON,
		MessageKey: messages.CommonMalformedJSON,
		Status:     constants.StatusBadRequest,
	})
}

// BodyOf builds the envelope body for a failure. Shared with the realtime
// gateway so a socket error and an HTTP error read identically.
func BodyOf(f *result.Failure) ErrorBody {
	message := messages.ForCode(f.Code)
	if f.MessageKey != "" {
		message = messages.Resolve(f.MessageKey)
	}
	return ErrorBody{
		Code:            f.Code,
		Message:         message,
		Severity:        constants.SeverityFor(f.Code),
		FieldErrors:     f.FieldErrors,
		RejectionReason: f.RejectionReason,
	}
}
