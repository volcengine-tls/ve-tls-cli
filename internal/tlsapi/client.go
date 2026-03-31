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
	"os"
	"strings"
	"time"

	"volclog/internal/version"

	"github.com/volcengine/volc-sdk-golang/base"
)

type Client struct {
	Endpoint string
	Region   string
	Service  string
	Timeout  time.Duration
	Creds    base.Credentials
	HTTP     *http.Client
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func New(endpoint, region, sdkProfile, ak, sk, token string, timeout time.Duration) (*Client, error) {
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
	r := strings.TrimSpace(region)
	if r == "" {
		return nil, errors.New("empty region")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	service := "TLS"
	_ = strings.TrimSpace(sdkProfile)
	creds, err := resolveSigningCredentials(r, service, ak, sk, token)
	if err != nil {
		return nil, err
	}
	c := &Client{
		Endpoint: strings.TrimRight(ep, "/"),
		Region:   r,
		Service:  service,
		Timeout:  timeout,
		Creds:    creds,
		HTTP:     &http.Client{Timeout: timeout},
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

	req.Header.Set("User-Agent", "volclog/"+version.Version)
	req.Header.Set("x-tls-apiversion", "0.3.0")
	req.Header.Set("Content-Type", "application/json")

	if len(body) > 0 {
		sum := md5.Sum(body)
		req.Header.Set("Content-Md5", strings.ToUpper(hex.EncodeToString(sum[:])))
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}

	req = c.Creds.Sign(req)
	if strings.TrimSpace(req.Header.Get("Authorization")) == "" {
		return Response{}, errors.New("signing failed: missing Authorization header")
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

func resolveSigningCredentials(region string, service string, ak string, sk string, token string) (base.Credentials, error) {
	accessKeyID := strings.TrimSpace(ak)
	secretAccessKey := strings.TrimSpace(sk)
	sessionToken := strings.TrimSpace(token)
	if accessKeyID == "" || secretAccessKey == "" {
		envAK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_ID"))
		envSK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET"))
		envToken := strings.TrimSpace(os.Getenv("VOLCENGINE_TOKEN"))
		if accessKeyID == "" {
			accessKeyID = envAK
		}
		if secretAccessKey == "" {
			secretAccessKey = envSK
		}
		if sessionToken == "" {
			sessionToken = envToken
		}
	}
	if accessKeyID == "" || secretAccessKey == "" {
		return base.Credentials{}, errors.New("missing access key id or secret access key")
	}
	return base.Credentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    sessionToken,
		Region:          strings.TrimSpace(region),
		Service:         strings.TrimSpace(service),
	}, nil
}
