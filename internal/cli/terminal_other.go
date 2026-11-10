//go:build !darwin && !linux && !windows

package cli

import "os"

func inputIsTerminal(*os.File) bool { return false }
