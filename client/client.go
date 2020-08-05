package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/diamondburned/smolboard/smolboard"
	"github.com/pkg/errors"
)

// StatusCoder is an interface that ErrUnexpectedStatusCode implements.
type StatusCoder interface {
	StatusCode() int
}

// ErrGetStatusCode gets the status code from error, or returns orCode if it
// can't get any.
func ErrGetStatusCode(err error, orCode int) int {
	if scode, ok := err.(StatusCoder); ok {
		return scode.StatusCode()
	}
	return orCode
}

type ErrUnexpectedStatusCode struct {
	Code   int
	Body   string
	ErrMsg string
}

func (err ErrUnexpectedStatusCode) StatusCode() int {
	return err.Code
}

func (err ErrUnexpectedStatusCode) Error() string {
	var errstr = fmt.Sprintf("Unexpected status code %d", err.Code)
	switch {
	case err.ErrMsg != "":
		errstr += ": " + err.ErrMsg
	case err.Body != "":
		errstr += ", body: " + err.Body
	}

	return errstr
}

// Client contains a single stateful HTTP client. Each session should have its
// own client, as each client has its own cookiejar.
type Client struct {
	http.Client
	host  *url.URL
	agent string
}

// NewClient makes a new client. Host is optional. This client is HTTPS by
// default.
func NewClient(host string) (*Client, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse host URL")
	}

	var client = &Client{
		Client: http.Client{
			Timeout: 10 * time.Second,
			Jar:     NewJar(),
		},
		host: u,
	}

	return client, nil
}

// NewClientFromRequest creates a new stateful client with cookies and
// useragents from the request.
func NewClientFromRequest(host string, r *http.Request) (*Client, error) {
	c, err := NewClient(host)
	if err != nil {
		return nil, err
	}

	c.SetCookies(r.Cookies())
	c.SetUserAgent(r.UserAgent())

	return c, nil
}

func (c *Client) SetUserAgent(userAgent string) {
	c.agent = userAgent
}

func (c *Client) Cookies() []*http.Cookie {
	return c.Jar.Cookies(c.host)
}

func (c *Client) SetCookies(cookies []*http.Cookie) {
	c.Jar.SetCookies(c.host, cookies)
}

// HostURL returns a copy of the client's host URL with the Path pointing to
// /api/v1.
func (c *Client) HostURL() *url.URL {
	cpy := *c.host
	cpy.Path = "/api/v1"
	return &cpy
}

// Host returns the stringified URL.
func (c *Client) Host() string {
	return c.host.String()
}

// Endpoint returns the HTTPS endpoint, or empty
func (c *Client) Endpoint() string {
	return c.Host() + "/api/v1"
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// Override the UserAgent if we have one.
	if c.agent != "" {
		req.Header.Set("User-Agent", c.agent)
	}

	r, err := c.Client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to send request")
	}

	if r.StatusCode < 200 || r.StatusCode > 299 {
		// Start reading the body for the error.
		defer r.Body.Close()

		var unexp = ErrUnexpectedStatusCode{Code: r.StatusCode}

		b, err := ioutil.ReadAll(r.Body)
		if err == nil {
			var errResp smolboard.ErrResponse
			if json.Unmarshal(b, &errResp); errResp.Error != "" {
				unexp.ErrMsg = errResp.Error
			} else {
				if len(b) > 100 {
					unexp.Body = string(b[:97]) + "..."
				} else {
					unexp.Body = string(b)
				}
			}
		}

		return nil, unexp
	}

	return r, nil
}

func (c *Client) DoJSON(req *http.Request, resp interface{}) error {
	q, err := c.Do(req)
	if err != nil {
		return err
	}
	defer q.Body.Close()

	if resp != nil {
		err := json.NewDecoder(q.Body).Decode(resp)
		return errors.Wrap(err, "Failed to decode JSON")
	}

	return nil
}

func (c *Client) Post(path string, resp interface{}, v url.Values) error {
	return c.Request("POST", path, resp, v)
}

func (c *Client) Get(path string, resp interface{}, v url.Values) error {
	return c.Request("GET", path, resp, v)
}

func (c *Client) Delete(path string, resp interface{}, v url.Values) error {
	return c.Request("DELETE", path, resp, v)
}

func (c *Client) Request(method, path string, resp interface{}, v url.Values) (err error) {
	var r *http.Request

	switch method {
	case http.MethodPatch, http.MethodPost, http.MethodPut:
		r, err = http.NewRequest(method, c.Endpoint()+path, strings.NewReader(v.Encode()))
		if err == nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	default:
		var url = fmt.Sprintf("%s%s?%s", c.Endpoint(), path, v.Encode())
		r, err = http.NewRequest(method, url, nil)
	}

	if err != nil {
		return errors.Wrap(err, "Failed to create request")
	}

	return c.DoJSON(r, resp)
}
