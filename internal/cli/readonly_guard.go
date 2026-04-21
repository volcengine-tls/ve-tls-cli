package cli

import (
	"errors"
	"fmt"
	"strings"
)

type readonlyEditionError struct {
	Surface string
	Target  string
}

func (e *readonlyEditionError) Error() string {
	return fmt.Sprintf("%s not available in default volclog readonly edition: %s", strings.TrimSpace(e.Surface), strings.TrimSpace(e.Target))
}

func newReadonlyEditionError(surface, target string) error {
	return &readonlyEditionError{
		Surface: strings.TrimSpace(surface),
		Target:  strings.TrimSpace(target),
	}
}

func ensureToolAccessibleInCurrentEdition(tool toolCatalog) error {
	if toolVisibleInCurrentEdition(tool) {
		return nil
	}
	return newReadonlyEditionError("tool", strings.TrimSpace(tool.ID))
}

func ensureWorkflowAccessibleInCurrentEdition(item workflowCatalog) error {
	if workflowVisibleInCurrentEdition(item) {
		return nil
	}
	return newReadonlyEditionError("workflow", strings.TrimSpace(item.ID))
}

func ensureRawCallAllowedInCurrentEdition(method, path string) error {
	if currentEdition() != cliEditionVolclog {
		return nil
	}
	catalog, err := loadToolCatalog()
	if err != nil {
		return err
	}
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	normalizedPath := strings.TrimSpace(path)
	for _, tool := range catalog.Tools {
		if strings.ToUpper(strings.TrimSpace(tool.Method)) != normalizedMethod {
			continue
		}
		if strings.TrimSpace(tool.Path) != normalizedPath {
			continue
		}
		return ensureToolAccessibleInCurrentEdition(tool)
	}
	return newReadonlyEditionError("raw", normalizedMethod+" "+normalizedPath)
}

func asReadonlyEditionError(err error) (*readonlyEditionError, bool) {
	var target *readonlyEditionError
	if !errors.As(err, &target) {
		return nil, false
	}
	return target, true
}
