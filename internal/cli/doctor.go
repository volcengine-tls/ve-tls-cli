package cli

func runDoctor(ctx *Context, args []string) (any, int, error) {
	if hasHelp(args) {
		return nil, 0, &usageError{Text: usageDoctor(), ExitCode: 0}
	}
	online := false
	for len(args) > 0 {
		switch args[0] {
		case "--online":
			online = true
			args = args[1:]
		default:
			return nil, 0, &usageError{Text: usageDoctor(), ExitCode: 1}
		}
	}

	state, err := collectDoctorRuntimeState(ctx)
	if err != nil {
		return nil, 0, err
	}
	if online {
		runDoctorNetworkChecks(ctx, state)
	}
	return buildDoctorOutput(state, online)
}
