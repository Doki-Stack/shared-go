package httpclient

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ExponentialBackoff returns the default backoff strategy: 100ms * 2^attempt + random(0, 100ms).
func ExponentialBackoff(attempt int) time.Duration {
	base := 100 * time.Millisecond
	exp := time.Duration(1<<uint(attempt)) * base
	jitter := time.Duration(0)
	if attempt >= 0 {
		// Simple jitter: 0-100ms (using time.Now().UnixNano() mod 100ms)
		jitter = time.Duration(time.Now().UnixNano()%int64(100*time.Millisecond)) * time.Nanosecond
	}
	return exp + jitter
}

// shouldRetry returns true if the request should be retried based on status code or error.
func shouldRetry(statusCode int, err error) bool {
	if err != nil {
		return true // connection errors, context deadline, etc.
	}
	switch statusCode {
	case http.StatusTooManyRequests: // 429
		return true
	case http.StatusBadGateway: // 502
		return true
	case http.StatusServiceUnavailable: // 503
		return true
	case http.StatusGatewayTimeout: // 504
		return true
	default:
		if statusCode >= 400 && statusCode < 500 {
			return false // 4xx except 429: no retry
		}
		if statusCode >= 500 {
			return false // 5xx except 502/503/504: no retry
		}
		return false
	}
}

// parseRetryAfter parses the Retry-After header. Returns 0 if not present or invalid.
// Numeric (seconds): capped at 60s. HTTP-date: duration until that time, capped at 60s.
func parseRetryAfter(hdr string, now time.Time) time.Duration {
	hdr = strings.TrimSpace(hdr)
	if hdr == "" {
		return 0
	}
	if sec, err := strconv.Atoi(hdr); err == nil {
		d := time.Duration(sec) * time.Second
		if d > 60*time.Second {
			d = 60 * time.Second
		}
		return d
	}
	if t, err := http.ParseTime(hdr); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		if d > 60*time.Second {
			d = 60 * time.Second
		}
		return d
	}
	return 0
}

// doWithRetry executes the request with retry logic. Caller must close resp.Body.
func (c *Client) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastResp *http.Response
	var lastErr error
	reqURL := req.URL.String()

	for attempt := 0; attempt <= c.retries; attempt++ {
		reqToSend := req
		if attempt > 0 {
			reqToSend = req.Clone(ctx)
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				reqToSend.Body = body
			}
		}

		resp, err := c.http.Transport.RoundTrip(reqToSend)
		if err != nil {
			lastErr = err
			if attempt < c.retries {
				logRetry(ctx, attempt, 0, reqURL, err)
				delay := c.backoff(attempt)
				if !sleep(ctx, delay) {
					return nil, ctx.Err()
				}
			}
			continue
		}

		statusCode := resp.StatusCode
		if !shouldRetry(statusCode, nil) {
			return c.maybeLimitBody(resp), nil
		}

		if attempt < c.retries {
			// Drain and close body before retry
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			logRetry(ctx, attempt, statusCode, reqURL, nil)

			delay := c.backoff(attempt)
			if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); retryAfter > 0 {
				delay = retryAfter
			}
			if !sleep(ctx, delay) {
				return nil, ctx.Err()
			}
		} else {
			lastResp = resp
			break
		}
	}

	if lastResp != nil {
		return c.maybeLimitBody(lastResp), nil
	}
	return nil, lastErr
}

func logRetry(ctx context.Context, attempt int, statusCode int, url string, err error) {
	// Use standard log; logger.FromContext not available in this package
	if err != nil {
		log.Printf("[httpclient] retry attempt %d after error: url=%s err=%v", attempt+1, url, err)
	} else {
		log.Printf("[httpclient] retry attempt %d after status %d: url=%s", attempt+1, statusCode, url)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ErrResponseTooLarge is returned when the response body exceeds maxResponseSize.
var ErrResponseTooLarge = &responseTooLargeError{}

type responseTooLargeError struct{}

func (e *responseTooLargeError) Error() string {
	return "httpclient: response body exceeds max size"
}

// limitedReadCloser wraps a reader and returns ErrResponseTooLarge when limit is exceeded.
type limitedReadCloser struct {
	r       io.Reader
	closer  io.Closer
	maxSize int64
	read    int64
	err     error
}

func (l *limitedReadCloser) Read(p []byte) (n int, err error) {
	if l.err != nil {
		return 0, l.err
	}
	if l.read >= l.maxSize {
		return 0, ErrResponseTooLarge
	}
	maxToRead := l.maxSize - l.read
	if int64(len(p)) > maxToRead {
		p = p[:maxToRead]
	}
	n, err = l.r.Read(p)
	l.read += int64(n)
	if l.read >= l.maxSize && err == nil {
		var buf [1]byte
		n2, err2 := l.r.Read(buf[:])
		if n2 > 0 {
			l.err = ErrResponseTooLarge
			return n, ErrResponseTooLarge
		}
		if err2 != nil && err2 != io.EOF {
			return n, err2
		}
	}
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.closer.Close()
}

func (c *Client) maybeLimitBody(resp *http.Response) *http.Response {
	if c.maxSize <= 0 {
		return resp
	}
	resp.Body = &limitedReadCloser{
		r:       resp.Body,
		closer:  resp.Body,
		maxSize: c.maxSize,
	}
	return resp
}
