package tlsapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tlsctl/internal/signv4"
)

type Client struct {
	Endpoint string
	Region   string
	Service  string
	Timeout  time.Duration
	Creds    signv4.Credentials
	HTTP     *http.Client
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func New(endpoint, region, ak, sk, token string, timeout time.Duration) (*Client, error) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return nil, errors.New("empty endpoint")
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	_, err := url.Parse(ep)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	c := &Client{
		Endpoint: strings.TrimRight(ep, "/"),
		Region:   strings.TrimSpace(region),
		Service:  "TLS",
		Timeout:  timeout,
		Creds: signv4.Credentials{
			AccessKeyID:     strings.TrimSpace(ak),
			SecretAccessKey: strings.TrimSpace(sk),
			SecurityToken:   strings.TrimSpace(token),
			Region:          strings.TrimSpace(region),
			Service:         "TLS",
		},
		HTTP: &http.Client{Timeout: timeout},
	}
	return c, nil
}

func (c *Client) Do(ctx context.Context, method, path string, query map[string]string, header map[string]string, body []byte) (Response, error) {
	u, err := url.Parse(c.Endpoint + path)
	if err != nil {
		return Response{}, err
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	} else {
		r = bytes.NewReader([]byte{})
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return Response{}, err
	}
	req.Host = u.Host

	req.Header.Set("User-Agent", "tlsctl/0.1")
	req.Header.Set("x-tls-apiversion", "0.3.0")
	req.Header.Set("Content-Type", "application/json")

	if len(body) > 0 {
		sum := md5.Sum(body)
		req.Header.Set("Content-Md5", strings.ToUpper(hex.EncodeToString(sum[:])))
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}

	if err := signv4.Sign(req, c.Creds, time.Now()); err != nil {
		return Response{}, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return Response{}, err
		}
		defer zr.Close()
		reader = zr
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		return Response{}, err
	}
	return Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: b}, nil
}
