package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"
)

var ErrResponseTooLarge = errors.New("httpx: response body exceeds configured limit")

type Config struct {
	Timeout time.Duration

	DialTimeout time.Duration

	TLSHandshakeTimeout time.Duration

	ResponseHeaderTimeout time.Duration

	IdleConnTimeout time.Duration

	MaxIdleConns int

	MaxIdleConnsPerHost int

	MaxResponseBytes int64

	MaxRetries int

	RetryBaseDelay time.Duration

	RetryMaxDelay time.Duration

	MaxRedirects int

	AllowPrivateNetworks bool
}

func DefaultConfig() Config {
	return Config{
		Timeout:               30 * time.Second,
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		MaxResponseBytes:      8 << 20,
		MaxRetries:            2,
		RetryBaseDelay:        200 * time.Millisecond,
		RetryMaxDelay:         5 * time.Second,
		MaxRedirects:          5,
		AllowPrivateNetworks:  true,
	}
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Client struct {
	http *http.Client
	cfg  Config
}

func New(cfg Config) (*Client, error) {
	if cfg.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("httpx: MaxResponseBytes must be positive, got %d", cfg.MaxResponseBytes)
	}
	if cfg.MaxRetries < 0 {
		return nil, fmt.Errorf("httpx: MaxRetries must not be negative, got %d", cfg.MaxRetries)
	}

	dialer := &net.Dialer{
		Timeout:   cfg.DialTimeout,
		KeepAlive: 30 * time.Second,
		Control:   guardDial(cfg.AllowPrivateNetworks),
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		cfg: cfg,
		http: &http.Client{
			Transport:     transport,
			Timeout:       cfg.Timeout,
			CheckRedirect: checkRedirect(cfg.MaxRedirects),
		},
	}, nil
}

func (c *Client) Do(ctx context.Context, req *http.Request) (*Response, error) {
	req = req.WithContext(ctx)

	for attempt := 0; ; attempt++ {
		if attempt > 0 {

			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("httpx: rewind request body: %w", err)
				}
				req.Body = body
			}
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt >= c.cfg.MaxRetries || !retryableTransportError(err) || !canRetry(req) {
				return nil, SanitizeError(err)
			}
			if waitErr := sleep(ctx, c.backoff(attempt, 0)); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if attempt < c.cfg.MaxRetries && retryableStatus(resp.StatusCode) && canRetry(req) {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			drainAndClose(resp.Body)
			if waitErr := sleep(ctx, c.backoff(attempt, retryAfter)); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		out, err := c.readResponse(resp)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func (c *Client) readResponse(resp *http.Response) (*Response, error) {
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, c.cfg.MaxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("httpx: read response body: %w", SanitizeError(err))
	}
	if int64(len(body)) > c.cfg.MaxResponseBytes {
		return nil, fmt.Errorf("%w (%d bytes)", ErrResponseTooLarge, c.cfg.MaxResponseBytes)
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, c.cfg.RetryMaxDelay)
	}
	backoff := c.cfg.RetryBaseDelay << attempt
	if backoff > c.cfg.RetryMaxDelay || backoff <= 0 {
		backoff = c.cfg.RetryMaxDelay
	}
	return rand.N(backoff) //nolint:gosec // jitter, not a security decision
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func canRetry(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
	default:
		return false
	}
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableTransportError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrBlockedAddress) {
		return false
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	_ = body.Close()
}

func bufferedBody(payload []byte) io.Reader {
	if len(payload) == 0 {
		return nil
	}
	return bytes.NewReader(payload)
}

func NewRequest(ctx context.Context, method, url string, payload []byte) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, bufferedBody(payload))
}
