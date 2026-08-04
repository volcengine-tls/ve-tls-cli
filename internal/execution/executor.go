package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type Executor struct {
	transport Transport
	codecs    *CodecRegistry
}

func NewExecutor(transport Transport, codecs *CodecRegistry) *Executor {
	if codecs == nil {
		codecs = NewCodecRegistry()
	}
	return &Executor{transport: transport, codecs: codecs}
}

func (e *Executor) Execute(ctx context.Context, invocation Invocation) (Result, error) {
	result := Result{OperationID: invocation.Operation.ID}
	if e == nil {
		return result, errors.New("nil executor")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	switch invocation.Options.ValidationPolicy {
	case ValidationRequired:
		if err := ValidateInput(invocation.Operation, invocation.Input); err != nil {
			return result, err
		}
	case ValidationCallerLegacy:
		// The caller preserves its established validation contract.
	default:
		return result, errors.New("unsupported validation policy")
	}
	request, err := BuildRequest(invocation.Operation, invocation.Input)
	if err != nil {
		return result, err
	}
	codecs := e.codecs
	if codecs == nil {
		codecs = NewCodecRegistry()
	}
	codec, err := codecs.Resolve(invocation.Operation.Wire.Codec)
	if err != nil {
		return result, err
	}
	if invocation.Options.PageAll && invocation.Operation.Pagination == nil {
		return result, unsupportedPaginationError(invocation.Operation.ID)
	}

	if invocation.Options.DryRun {
		if invocation.Options.PageAll {
			request, _, err = prepareFirstPaginatedRequest(request, invocation.Operation.Pagination)
			if err != nil {
				return result, err
			}
		}
		preview := cloneRequest(request)
		encoded, _, err := codec.Encode(ctx, invocation.Operation, cloneRequest(request))
		if err != nil {
			return result, err
		}
		result.Plan = buildDryRunPlan(invocation, preview, encoded)
		return result, nil
	}
	if isNilTransport(e.transport) {
		return result, errors.New("nil transport")
	}
	if invocation.Options.PageAll {
		return e.executeAll(ctx, invocation, request, codec)
	}
	return e.executeOne(ctx, invocation.Operation, request, codec)
}

func isNilTransport(transport Transport) bool {
	if transport == nil {
		return true
	}
	value := reflect.ValueOf(transport)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (e *Executor) executeOne(ctx context.Context, operation contract.Operation, request Request, codec Codec) (Result, error) {
	result := Result{OperationID: operation.ID}
	encoded, state, err := codec.Encode(ctx, operation, cloneRequest(request))
	if err != nil {
		return result, err
	}
	response, transportErr := e.transport.Do(ctx, encoded)
	result = resultForResponse(operation.ID, response)
	if transportErr != nil {
		return result, transportErr
	}
	data, decodeErr := codec.Decode(ctx, operation, response, state)
	result.Data = data
	return result, decodeErr
}

func buildDryRunPlan(invocation Invocation, preview, encoded Request) *DryRunPlan {
	checks := make([]PreflightCheck, 0, len(invocation.Runtime.Checks)+3)
	checks = append(checks, invocation.Runtime.Checks...)
	if !failedCheck(checks, "profile") {
		endpoint := strings.TrimSpace(invocation.Runtime.Endpoint)
		region := strings.TrimSpace(invocation.Runtime.Region)
		checks = append(checks,
			PreflightCheck{
				Name:   "endpoint",
				OK:     endpoint != "",
				Detail: &endpoint,
			},
			PreflightCheck{
				Name:   "region",
				OK:     region != "",
				Detail: &region,
			},
		)
	}
	if len(strings.TrimSpace(string(encoded.Body))) > 0 && strings.TrimSpace(string(encoded.Body)) != "{}" {
		if invocation.Operation.Wire.Codec != contract.CodecJSON {
			checks = append(checks, PreflightCheck{Name: "body_codec", OK: true})
		} else {
			checks = append(checks, validateJSONBodyCheck(encoded))
		}
	}
	valid := true
	for _, check := range checks {
		if !check.OK {
			valid = false
		}
	}
	sum := sha256.Sum256(encoded.Body)
	plan := &DryRunPlan{
		Type:            "plan",
		Method:          strings.ToUpper(strings.TrimSpace(encoded.Method)),
		Path:            strings.TrimSpace(encoded.Path),
		QueryKeys:       sortedMapKeys(encoded.Query),
		HeadersRedacted: redactedHeaderKeys(encoded.Header),
		BodySHA256:      hex.EncodeToString(sum[:]),
		Checks:          checks,
		Valid:           valid,
		RequestPreview: map[string]any{
			"query": cloneStringMap(preview.Query),
		},
	}
	if body, ok := previewBody(preview); ok {
		plan.RequestPreview["body"] = body
	}
	if invocation.Operation.Wire.Codec != contract.CodecJSON {
		plan.RequestPreview["body_source"] = "input_before_special_io"
	}
	if invocation.Options.PageAll {
		plan.PageAll = &DryRunPaginationPlan{
			Requested: true,
			Mode:      "dry_run",
			Note:      "dry-run validates the first request shape; real execution will iterate all supported pages",
			PageSize:  pageSizeFromRequest(encoded.Query, invocation.Operation.Pagination),
		}
	}
	return plan
}

func failedCheck(checks []PreflightCheck, name string) bool {
	for _, check := range checks {
		if check.Name == name && !check.OK {
			return true
		}
	}
	return false
}

func validateJSONBodyCheck(request Request) PreflightCheck {
	check := PreflightCheck{Name: "body_json", OK: true}
	switch request.BodyFormat {
	case BodyFormatJSONL:
		for _, line := range strings.Split(string(request.Body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var value any
			if err := json.Unmarshal([]byte(line), &value); err != nil {
				check.OK = false
				detail := err.Error()
				check.Detail = &detail
				return check
			}
		}
	default:
		var value any
		if err := json.Unmarshal(request.Body, &value); err != nil {
			check.OK = false
			detail := err.Error()
			check.Detail = &detail
		}
	}
	return check
}

func previewBody(request Request) (any, bool) {
	trimmed := strings.TrimSpace(string(request.Body))
	if trimmed == "" {
		return map[string]any{}, true
	}
	switch request.BodyFormat {
	case BodyFormatJSONL:
		lines := strings.Split(trimmed, "\n")
		rows := make([]any, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row any
			if json.Unmarshal([]byte(line), &row) != nil {
				return trimmed, true
			}
			rows = append(rows, row)
		}
		return rows, true
	default:
		var value any
		if json.Unmarshal(request.Body, &value) == nil {
			return value, true
		}
		return trimmed, true
	}
}

func redactedHeaderKeys(header map[string]string) []string {
	seen := map[string]struct{}{
		"Authorization":    {},
		"X-Security-Token": {},
	}
	for key := range header {
		key = strings.TrimSpace(key)
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
