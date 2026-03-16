//go:build embed_tlsctl

package runner

import (
	_ "embed"
	"runtime"
)

//go:embed embedded_tlsctl.bin
var embeddedTLSCTLBytes []byte

func embeddedTLSCTL() (name string, data []byte, ok bool) {
	name = "tlsctl"
	if runtime.GOOS == "windows" {
		name = "tlsctl.exe"
	}
	return name, embeddedTLSCTLBytes, len(embeddedTLSCTLBytes) > 0
}

