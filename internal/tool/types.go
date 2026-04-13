package tool

import (
	"encoding/json"
	"errors"
	"net/http"
)

const (
	DefaultMaxLines    = 2000
	DefaultMaxBytes    = 50 * 1024
	GrepMaxLineLength  = 500
	DefaultGrepLimit   = 100
	DefaultFindLimit   = 1000
	DefaultLSLimit     = 500
	DefaultStartPort   = 8080
	MaxPortScanAttempt = 100
)

type ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type Response struct {
	Content []ContentItem `json:"content"`
	Details any           `json:"details,omitempty"`
}

type Error struct {
	StatusCode int
	Message    string
}

func (e *Error) Error() string {
	return e.Message
}

func ValidationError(message string) error {
	return &Error{StatusCode: http.StatusBadRequest, Message: message}
}

func ExecutionError(message string) error {
	return &Error{StatusCode: http.StatusInternalServerError, Message: message}
}

func StatusCode(err error) int {
	var toolErr *Error
	if errors.As(err, &toolErr) {
		return toolErr.StatusCode
	}
	return http.StatusInternalServerError
}

func MarshalResponse(response Response) ([]byte, error) {
	return json.Marshal(response)
}

type HeadTruncation struct {
	Truncated             bool   `json:"truncated"`
	TruncatedBy           string `json:"truncatedBy,omitempty"`
	TotalLines            int    `json:"totalLines,omitempty"`
	OutputLines           int    `json:"outputLines,omitempty"`
	OutputBytes           int    `json:"outputBytes,omitempty"`
	MaxLines              int    `json:"maxLines,omitempty"`
	MaxBytes              int    `json:"maxBytes,omitempty"`
	FirstLineExceedsLimit bool   `json:"firstLineExceedsLimit,omitempty"`
}

type TailTruncation struct {
	Truncated       bool   `json:"truncated"`
	TruncatedBy     string `json:"truncatedBy,omitempty"`
	TotalLines      int    `json:"totalLines,omitempty"`
	OutputLines     int    `json:"outputLines,omitempty"`
	OutputBytes     int    `json:"outputBytes,omitempty"`
	MaxLines        int    `json:"maxLines,omitempty"`
	MaxBytes        int    `json:"maxBytes,omitempty"`
	LastLinePartial bool   `json:"lastLinePartial,omitempty"`
}
