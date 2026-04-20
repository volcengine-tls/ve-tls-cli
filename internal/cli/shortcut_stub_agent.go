//go:build agent

package cli

func shortcutCommandHelpLookup(_ string) subcommandHelpLookup {
	return nil
}

func maybeHandleShortcutMeta(_, _ string, _ []string) (any, bool, error) {
	return nil, false, nil
}

func usageLog() string {
	return usageText()
}
