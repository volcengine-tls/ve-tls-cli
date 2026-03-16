//go:build !embed_tlsctl

package runner

func embeddedTLSCTL() (name string, data []byte, ok bool) {
	return "", nil, false
}

