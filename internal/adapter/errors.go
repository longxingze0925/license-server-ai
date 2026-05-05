package adapter

import (
	"errors"
	"fmt"
	"net"
	"net/url"
)

// UpstreamHTTPError carries an upstream non-2xx status so callers can decide
// whether switching credentials can help.
type UpstreamHTTPError struct {
	Operation  string
	StatusCode int
	Body       string
}

func (e *UpstreamHTTPError) Error() string {
	if e.Operation == "" {
		return fmt.Sprintf("上游 HTTP %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("%s 上游 HTTP %d: %s", e.Operation, e.StatusCode, e.Body)
}

func newUpstreamHTTPError(operation string, statusCode int, respBody []byte, max int) error {
	return &UpstreamHTTPError{
		Operation:  operation,
		StatusCode: statusCode,
		Body:       truncateAdapter(string(respBody), max),
	}
}

func CreateErrorNeedsCredentialRetry(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *UpstreamHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 401 ||
			httpErr.StatusCode == 403 ||
			httpErr.StatusCode == 429 ||
			httpErr.StatusCode >= 500
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

func CreateErrorDegradesCredential(err error) bool {
	var httpErr *UpstreamHTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403)
}

func CreateErrorMarksCredentialDown(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}
