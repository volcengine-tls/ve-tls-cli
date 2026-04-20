package cli

type subcommandDispatch func(command string, args []string) (any, error)
type subcommandHelpLookup func(command string) (string, bool)

func hasHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func runSubcommandGroup(args []string, usage string, skipNestedHelpFor map[string]struct{}, helpLookup subcommandHelpLookup, dispatch subcommandDispatch) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usage, ExitCode: 1}
	}
	command := args[0]
	if command == "-h" || command == "--help" {
		return nil, &usageError{Text: usage, ExitCode: 0}
	}
	if _, skip := skipNestedHelpFor[command]; !skip && hasHelp(args[1:]) {
		if helpLookup != nil {
			if help, ok := helpLookup(command); ok {
				return nil, &usageError{Text: help, ExitCode: 0}
			}
		}
		return nil, &usageError{Text: usage, ExitCode: 0}
	}
	return dispatch(command, args[1:])
}
