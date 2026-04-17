// Package errors defines structured error types returned by all skillhub tools.
package errors

import "encoding/json"

// ErrorCode is a machine-readable error classification.
type ErrorCode string

// Error code constants for all tool results.
const (
	ErrNotImplemented         ErrorCode = "NOT_IMPLEMENTED"
	ErrAuthFailed             ErrorCode = "AUTH_FAILED"
	ErrNetworkFailed          ErrorCode = "NETWORK_FAILED"
	ErrPluginNotFound         ErrorCode = "PLUGIN_NOT_FOUND"
	ErrSkillNotFound          ErrorCode = "SKILL_NOT_FOUND"
	ErrDriftBlocked           ErrorCode = "DRIFT_BLOCKED"
	ErrNothingToPropose       ErrorCode = "NOTHING_TO_PROPOSE"
	ErrInvalidManifest        ErrorCode = "INVALID_MANIFEST"
	ErrMarketplaceUnreachable ErrorCode = "MARKETPLACE_UNREACHABLE"
)

// SkillhubError is the structured error type serialised into all tool results.
type SkillhubError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Detail  string    `json:"detail,omitempty"`
}

func (e *SkillhubError) Error() string { return string(e.Code) + ": " + e.Message }

// JSON returns the JSON encoding of e. Panics only on impossible marshal failure.
func (e *SkillhubError) JSON() string {
	b, err := json.Marshal(e)
	if err != nil {
		panic("skerrors: marshal failed: " + err.Error())
	}
	return string(b)
}
