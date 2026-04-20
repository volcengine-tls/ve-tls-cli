package cli

func flagParam(name, cliFlag, in string, required bool, typ, desc string) apiCapParam {
	return apiCapParam{
		Name:        name,
		CLIFlag:     cliFlag,
		In:          in,
		Required:    required,
		Type:        typ,
		Description: desc,
	}
}
