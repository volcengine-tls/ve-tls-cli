package signv4

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SecurityToken   string
	Region          string
	Service         string
}

func Sign(req *http.Request, creds Credentials, now time.Time) error {
	q := req.URL.Query()
	req.URL.RawQuery = q.Encode()

	if req.URL.Path == "" {
		req.URL.Path = "/"
	}

	body, err := readAndReplaceBody(req)
	if err != nil {
		return err
	}

	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	xDate := now.UTC().Format("20060102T150405Z")
	req.Header.Set("X-Date", xDate)
	req.Header.Set("Host", req.Host)

	bodyHash := hashSHA256(body)
	req.Header.Set("X-Content-Sha256", bodyHash)

	if strings.TrimSpace(creds.SecurityToken) != "" {
		req.Header.Set("X-Security-Token", creds.SecurityToken)
	}

	meta := metadata{
		date:            xDate[:8],
		service:         creds.Service,
		region:          creds.Region,
		algorithm:       "HMAC-SHA256",
		credentialScope: strings.Join([]string{xDate[:8], creds.Region, creds.Service, "request"}, "/"),
	}

	requestSignMap := map[string][]string{}
	for k, v := range req.Header {
		requestSignMap[k] = v
	}

	hashedCanonReq := hashedCanonicalRequest(req.Method, req.URL.Path, q, &meta, requestSignMap, bodyHash)
	stringToSign := strings.Join([]string{meta.algorithm, xDate, meta.credentialScope, hashedCanonReq}, "\n")
	signingKey := signingKey(creds.SecretAccessKey, meta.date, meta.region, meta.service)
	signature := signature(signingKey, stringToSign)
	req.Header.Set("Authorization", buildAuthHeader(signature, meta, creds.AccessKeyID))
	return nil
}

type metadata struct {
	date            string
	service         string
	region          string
	algorithm       string
	credentialScope string
	signedHeaders   string
}

func hashedCanonicalRequest(method, path string, q url.Values, meta *metadata, requestSignMap map[string][]string, bodyHash string) string {
	canonicalHeaders, signedHeaders := canonicalHeaders(requestSignMap)
	meta.signedHeaders = strings.Join(signedHeaders, ";")
	canonicalRequest := strings.Join([]string{
		method,
		normURI(path),
		normQuery(q),
		canonicalHeaders,
		meta.signedHeaders,
		bodyHash,
	}, "\n")
	return hashSHA256([]byte(canonicalRequest))
}

func canonicalHeaders(requestSignMap map[string][]string) (string, []string) {
	signMap := map[string][]string{}
	var keys []string
	for k, v := range requestSignMap {
		lk := strings.ToLower(k)
		signMap[lk] = v
		switch k {
		case "Content-Type", "Content-Md5", "Host", "X-Security-Token", "X-Date", "X-Content-Sha256":
			keys = append(keys, lk)
		default:
			if strings.HasPrefix(k, "X-") {
				keys = append(keys, lk)
			}
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := ""
		if vv := signMap[k]; len(vv) > 0 {
			v = strings.TrimSpace(vv[0])
		}
		if k == "host" {
			if i := strings.IndexByte(v, ':'); i >= 0 {
				port := v[i+1:]
				if port == "80" || port == "443" {
					v = v[:i]
				}
			}
		}
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String(), keys
}

func buildAuthHeader(signature string, meta metadata, ak string) string {
	credential := ak + "/" + meta.credentialScope
	return meta.algorithm + " Credential=" + credential + ", SignedHeaders=" + meta.signedHeaders + ", Signature=" + signature
}

func signingKey(secretKey, date, region, service string) []byte {
	kDate := hmacSHA256([]byte(secretKey), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "request")
}

func signature(signingKey []byte, stringToSign string) string {
	return hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
}

func hmacSHA256(key []byte, content string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(content))
	return mac.Sum(nil)
}

func hashSHA256(content []byte) string {
	h := sha256.New()
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

func readAndReplaceBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return []byte{}, nil
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

func normQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		ek := escapeQuery(k)
		for _, v := range vals {
			parts = append(parts, ek+"="+escapeQuery(v))
		}
	}
	return strings.Join(parts, "&")
}

func escapeQuery(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func normURI(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	segs := strings.Split(path, "/")
	for i := range segs {
		segs[i] = escapePath(segs[i])
	}
	return strings.Join(segs, "/")
}

func escapePath(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "+", "%20")
}
