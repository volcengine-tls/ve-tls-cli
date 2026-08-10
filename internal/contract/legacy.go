package contract

func RebuildLegacyToolV1(catalog Catalog, operation Operation) (LegacyToolV1, error) {
	contextSchema, err := ExpandContextSchema(catalog.ContextSchema, catalog.ExecutionSchema)
	if err != nil {
		return LegacyToolV1{}, err
	}
	return LegacyToolV1{
		ID:               string(operation.ID),
		Group:            operation.Group,
		Action:           operation.Action,
		Resource:         operation.Resource,
		Verb:             operation.Verb,
		Family:           operation.Family,
		Method:           operation.Wire.Method,
		Path:             operation.Wire.Path,
		Visibility:       operation.Visibility,
		Summary:          operation.Docs.Summary,
		InputSchema:      cloneSchema(operation.InputSchema),
		ContextSchema:    contextSchema,
		ExecutionSchema:  cloneSchema(catalog.ExecutionSchema),
		OutputPolicy:     operation.Output.Policy,
		ErrorRecovery:    operation.Risk.ErrorRecovery,
		DocSource:        operation.Docs.Source,
		UsageConstraints: operation.Docs.UsageConstraints,
		RiskLevel:        operation.Risk.Level,
		SupportsDryRun:   operation.Runtime.SupportsDryRun,
		SupportsAll:      operation.Pagination != nil,
		IsEnvelopeOutput: operation.Output.IsEnvelopeOutput,
	}, nil
}
