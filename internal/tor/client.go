package tor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// UserAgent matches Tor Browser on Windows, the most common Tor fingerprint,
// so requests don't stand out among other Tor users.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; rv:140.0) Gecko/20100101 Firefox/140.0"

// Client is an HTTP client whose every connection goes through Tor.
// Cookies live only in memory for the life of the process.
type Client struct {
	*http.Client
	dialer    *Dialer
	transport *http.Transport
}

// NewClient returns a Client using the SOCKS5 proxy at proxyAddr.
func NewClient(proxyAddr string) *Client {
	d := NewDialer(proxyAddr)
	tr := &http.Transport{
		Proxy:                 nil, // never honour HTTP(S)_PROXY: everything goes via DialContext
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   60 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
	}
	jar, _ := cookiejar.New(nil)
	return &Client{
		Client:    &http.Client{Transport: headerTransport{tr}, Jar: jar},
		dialer:    d,
		transport: tr,
	}
}

// Fork returns an independent client on the same proxy, with its own
// circuits and cookies, for running downloads in parallel.
func (c *Client) Fork() *Client { return NewClient(c.dialer.ProxyAddr) }

// NewCircuit makes subsequent requests use a fresh Tor circuit.
func (c *Client) NewCircuit() {
	c.dialer.Renew()
	c.transport.CloseIdleConnections()
}

// headerTransport sets browser-like default headers on every request.
type headerTransport struct{ rt http.RoundTripper }

func (h headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("refusing %s request", req.URL.Scheme)
	}
	req = req.Clone(req.Context())
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	}
	return h.rt.RoundTrip(req)
}

// CheckResult is the Tor Project's verdict on our exit.
type CheckResult struct {
	IsTor bool   `json:"IsTor"`
	IP    string `json:"IP"`
}

// ErrNotTor means traffic is reaching the internet without going through Tor.
var ErrNotTor = errors.New("traffic is NOT going through Tor")

// Verify asks check.torproject.org whether our requests arrive from a Tor exit.
func (c *Client) Verify(ctx context.Context) (*CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://check.torproject.org/api/ip", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r CheckResult
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("tor check: %w", err)
	}
	if !r.IsTor {
		return &r, ErrNotTor
	}
	return &r, nil
}
