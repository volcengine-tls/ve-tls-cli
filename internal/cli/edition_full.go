//go:build !agent

package cli

func currentEdition() cliEdition {
	return cliEditionFull
}
