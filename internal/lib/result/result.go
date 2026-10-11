// Package result is what services return. They never return a Go error for an
// expected domain failure — "name taken" and "room not found" are ordinary
// outcomes. A Go error is reserved for the genuinely unexpected.
package result

import (
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

type Failure struct {
	Code       constants.ErrorCode
	MessageKey messages.Key
	Status     int
	// Present on validation failures only.
	FieldErrors map[string][]string
	// Diagnostic for operators. Never contract — no client may branch on it.
	RejectionReason string
}

type Result[T any] struct {
	Data    T
	Failure *Failure
}

func (r Result[T]) OK() bool { return r.Failure == nil }

func Ok[T any](data T) Result[T] { return Result[T]{Data: data} }

func Fail[T any](code constants.ErrorCode, key messages.Key, status int) Result[T] {
	return Result[T]{Failure: &Failure{Code: code, MessageKey: key, Status: status}}
}

// FailWith carries a prepared failure through a different result type.
func FailWith[T any](f *Failure) Result[T] { return Result[T]{Failure: f} }

func Invalid[T any](fieldErrors map[string][]string) Result[T] {
	return Result[T]{Failure: &Failure{
		Code:        constants.CodeValidation,
		MessageKey:  messages.CommonValidation,
		Status:      constants.StatusUnprocessable,
		FieldErrors: fieldErrors,
	}}
}

// Reason attaches the operator diagnostic.
func (f *Failure) Reason(reason string) *Failure {
	f.RejectionReason = reason
	return f
}
