package catalogdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Catalog matches the expected tool-catalog JSON shape.
type Catalog struct {
	Version string `json:"version"`
	Tools   []Tool `json:"tools"`
}

// Tool is the subset of tool-catalog fields required for diff reporting.
type Tool struct {
	ID             string         `json:"id"`
	Group          string         `json:"group"`
	Action         string         `json:"action"`
	Resource       string         `json:"resource"`
	Verb           string         `json:"verb"`
	Family         string         `json:"family"`
	Method         string         `json:"method"`
	Path           string         `json:"path"`
	Summary        string         `json:"summary"`
	InputSchema    map[string]any `json:"input_schema"`
	OutputPolicy   string         `json:"output_policy"`
	DocSource      string         `json:"doc_source"`
	Source         string         `json:"source"`
	SourceQuality  string         `json:"source_quality"`
	RiskLevel      string         `json:"risk_level"`
	SupportsDryRun bool           `json:"supports_dry_run"`
	SupportsAll    bool           `json:"supports_all"`
}

// ToolIdentity is the display identity for diff entries.
type ToolIdentity struct {
	ID     string `json:"id"`
	Group  string `json:"group"`
	Action string `json:"action"`
}

func (i ToolIdentity) String() string {
	if strings.TrimSpace(i.ID) != "" {
		return strings.TrimSpace(i.ID)
	}
	if strings.TrimSpace(i.Group) != "" && strings.TrimSpace(i.Action) != "" {
		return strings.TrimSpace(i.Group) + "." + strings.TrimSpace(i.Action)
	}
	return "<unknown>"
}

// ToolMetadata contains identity/action fields that can be changed without a rename.
type ToolMetadata struct {
	ID           string `json:"id"`
	Group        string `json:"group"`
	Action       string `json:"action"`
	Resource     string `json:"resource"`
	Verb         string `json:"verb"`
	Family       string `json:"family"`
	Summary      string `json:"summary"`
	OutputPolicy string `json:"output_policy"`
}

// IdentityMetadataChange captures identity/action metadata changes for the same tool.
type IdentityMetadataChange struct {
	Identity      ToolIdentity `json:"identity"`
	Old           ToolMetadata `json:"old"`
	New           ToolMetadata `json:"new"`
	ChangedFields []string     `json:"changed_fields"`
}

type MethodPathChange struct {
	Identity ToolIdentity `json:"identity"`
	Old      struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"old"`
	New struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"new"`
}

type InputSchemaDigestChange struct {
	Identity ToolIdentity `json:"identity"`
	Old      string       `json:"old"`
	New      string       `json:"new"`
}

type RiskLevelChange struct {
	Identity ToolIdentity `json:"identity"`
	Old      string       `json:"old"`
	New      string       `json:"new"`
}

type SourceChange struct {
	Identity ToolIdentity `json:"identity"`
	Old      struct {
		Source        string `json:"source"`
		SourceQuality string `json:"source_quality"`
	} `json:"old"`
	New struct {
		Source        string `json:"source"`
		SourceQuality string `json:"source_quality"`
	} `json:"new"`
}

type SupportsChange struct {
	Identity ToolIdentity `json:"identity"`
	Old      struct {
		SupportsDryRun bool `json:"supports_dry_run"`
		SupportsAll    bool `json:"supports_all"`
	} `json:"old"`
	New struct {
		SupportsDryRun bool `json:"supports_dry_run"`
		SupportsAll    bool `json:"supports_all"`
	} `json:"new"`
}

// CatalogDiffReport is the structured output for catalog comparison.
type CatalogDiffReport struct {
	Added                    []ToolIdentity            `json:"added"`
	Removed                  []ToolIdentity            `json:"removed"`
	IdentityMetadataChanges  []IdentityMetadataChange  `json:"identity_metadata_changes"`
	MethodPathChanges        []MethodPathChange        `json:"method_path_changes"`
	InputSchemaDigestChanges []InputSchemaDigestChange `json:"input_schema_digest_changes"`
	RiskLevelChanges         []RiskLevelChange         `json:"risk_level_changes"`
	SourceChanges            []SourceChange            `json:"source_changes"`
	SupportsChanges          []SupportsChange          `json:"supports_changes"`
	Summary                  CatalogDiffSummary        `json:"summary"`
}

type CatalogDiffSummary struct {
	Added                    int `json:"added"`
	Removed                  int `json:"removed"`
	IdentityMetadataChanges  int `json:"identity_metadata_changes"`
	MethodPathChanges        int `json:"method_path_changes"`
	InputSchemaDigestChanges int `json:"input_schema_digest_changes"`
	RiskLevelChanges         int `json:"risk_level_changes"`
	SourceChanges            int `json:"source_changes"`
	SupportsChanges          int `json:"supports_changes"`
	TotalChanges             int `json:"total_changes"`
}

// LoadCatalog parses a catalog JSON file into a Catalog.
func LoadCatalog(path string) (Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	var catalog Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("parse catalog %q: %w", path, err)
	}
	return catalog, nil
}

// CompareFiles loads two catalog files and compares their tool contract sets.
func CompareFiles(oldPath, newPath string) (CatalogDiffReport, error) {
	oldCatalog, err := LoadCatalog(oldPath)
	if err != nil {
		return CatalogDiffReport{}, err
	}
	newCatalog, err := LoadCatalog(newPath)
	if err != nil {
		return CatalogDiffReport{}, err
	}
	return CompareCatalogs(oldCatalog, newCatalog)
}

// CompareCatalogs compares two in-memory catalog objects.
func CompareCatalogs(oldCatalog, newCatalog Catalog) (CatalogDiffReport, error) {
	oldIdxByID := map[string]int{}
	oldIdxByIdentity := map[string]int{}
	for i, tool := range oldCatalog.Tools {
		if id := strings.ToLower(strings.TrimSpace(tool.ID)); id != "" {
			oldIdxByID[id] = i
		}
		if identity := oldIdentityKey(tool); identity != "" {
			oldIdxByIdentity[identity] = i
		}
	}

	usedOld := make(map[int]struct{}, len(oldCatalog.Tools))
	report := CatalogDiffReport{}

	for _, newTool := range newCatalog.Tools {
		oldIdx, found := findOldMatch(oldIdxByID, oldIdxByIdentity, usedOld, oldCatalog.Tools, newTool)
		if !found {
			report.Added = append(report.Added, buildToolIdentity(newTool))
			continue
		}
		usedOld[oldIdx] = struct{}{}
		oldTool := oldCatalog.Tools[oldIdx]
		appendIdentityMetadataChanges(&report, oldTool, newTool)
		appendMethodPathChanges(&report, oldTool, newTool)
		if err := appendInputSchemaDigestChanges(&report, oldTool, newTool); err != nil {
			return CatalogDiffReport{}, err
		}
		appendRiskLevelChanges(&report, oldTool, newTool)
		appendSourceChanges(&report, oldTool, newTool)
		appendSupportsChanges(&report, oldTool, newTool)
	}

	for i, oldTool := range oldCatalog.Tools {
		if _, ok := usedOld[i]; ok {
			continue
		}
		report.Removed = append(report.Removed, buildToolIdentity(oldTool))
	}

	sortChangeLists(&report)
	report.Summary = buildSummary(report)

	return report, nil
}

func (tool Tool) effectiveSource() string {
	if strings.TrimSpace(tool.DocSource) != "" {
		return strings.TrimSpace(tool.DocSource)
	}
	return strings.TrimSpace(tool.Source)
}

func findOldMatch(oldIdxByID, oldIdxByIdentity map[string]int, used map[int]struct{}, oldTools []Tool, newTool Tool) (int, bool) {
	candidates := []int{}
	if id := strings.ToLower(strings.TrimSpace(newTool.ID)); id != "" {
		if idx, ok := oldIdxByID[id]; ok {
			candidates = append(candidates, idx)
		}
	}
	if len(candidates) == 0 {
		if identity := oldIdentityKey(newTool); identity != "" {
			if idx, ok := oldIdxByIdentity[identity]; ok {
				candidates = append(candidates, idx)
			}
		}
	}
	for _, idx := range candidates {
		if _, ok := used[idx]; ok {
			continue
		}
		_ = oldTools
		return idx, true
	}
	return -1, false
}

func buildToolIdentity(tool Tool) ToolIdentity {
	return ToolIdentity{
		ID:     strings.TrimSpace(tool.ID),
		Group:  strings.TrimSpace(tool.Group),
		Action: strings.TrimSpace(tool.Action),
	}
}

func buildToolMetadata(tool Tool) ToolMetadata {
	return ToolMetadata{
		ID:           strings.TrimSpace(tool.ID),
		Group:        strings.TrimSpace(tool.Group),
		Action:       strings.TrimSpace(tool.Action),
		Resource:     strings.TrimSpace(tool.Resource),
		Verb:         strings.TrimSpace(tool.Verb),
		Family:       strings.TrimSpace(tool.Family),
		Summary:      strings.TrimSpace(tool.Summary),
		OutputPolicy: strings.TrimSpace(tool.OutputPolicy),
	}
}

func appendIdentityMetadataChanges(report *CatalogDiffReport, oldTool, newTool Tool) {
	oldMeta := buildToolMetadata(oldTool)
	newMeta := buildToolMetadata(newTool)
	changedFields := []string{}
	if oldMeta.ID != newMeta.ID {
		changedFields = append(changedFields, "id")
	}
	if oldMeta.Group != newMeta.Group {
		changedFields = append(changedFields, "group")
	}
	if oldMeta.Action != newMeta.Action {
		changedFields = append(changedFields, "action")
	}
	if oldMeta.Resource != newMeta.Resource {
		changedFields = append(changedFields, "resource")
	}
	if oldMeta.Verb != newMeta.Verb {
		changedFields = append(changedFields, "verb")
	}
	if oldMeta.Family != newMeta.Family {
		changedFields = append(changedFields, "family")
	}
	if oldMeta.Summary != newMeta.Summary {
		changedFields = append(changedFields, "summary")
	}
	if oldMeta.OutputPolicy != newMeta.OutputPolicy {
		changedFields = append(changedFields, "output_policy")
	}
	if len(changedFields) == 0 {
		return
	}
	report.IdentityMetadataChanges = append(report.IdentityMetadataChanges, IdentityMetadataChange{
		Identity:      buildToolIdentity(newTool),
		Old:           oldMeta,
		New:           newMeta,
		ChangedFields: changedFields,
	})
}

func appendMethodPathChanges(report *CatalogDiffReport, oldTool, newTool Tool) {
	oldMethod := strings.ToUpper(strings.TrimSpace(oldTool.Method))
	newMethod := strings.ToUpper(strings.TrimSpace(newTool.Method))
	oldPath := strings.TrimSpace(oldTool.Path)
	newPath := strings.TrimSpace(newTool.Path)
	if oldMethod == newMethod && oldPath == newPath {
		return
	}
	change := MethodPathChange{Identity: buildToolIdentity(newTool)}
	change.Old.Method = oldMethod
	change.Old.Path = oldPath
	change.New.Method = newMethod
	change.New.Path = newPath
	report.MethodPathChanges = append(report.MethodPathChanges, change)
}

func appendInputSchemaDigestChanges(report *CatalogDiffReport, oldTool, newTool Tool) error {
	oldDigest, err := inputSchemaDigest(oldTool.InputSchema)
	if err != nil {
		return err
	}
	newDigest, err := inputSchemaDigest(newTool.InputSchema)
	if err != nil {
		return err
	}
	if oldDigest == newDigest {
		return nil
	}
	report.InputSchemaDigestChanges = append(report.InputSchemaDigestChanges, InputSchemaDigestChange{
		Identity: buildToolIdentity(newTool),
		Old:      oldDigest,
		New:      newDigest,
	})
	return nil
}

func appendRiskLevelChanges(report *CatalogDiffReport, oldTool, newTool Tool) {
	oldRisk := strings.TrimSpace(strings.ToLower(oldTool.RiskLevel))
	newRisk := strings.TrimSpace(strings.ToLower(newTool.RiskLevel))
	if oldRisk == newRisk {
		return
	}
	report.RiskLevelChanges = append(report.RiskLevelChanges, RiskLevelChange{
		Identity: buildToolIdentity(newTool),
		Old:      oldRisk,
		New:      newRisk,
	})
}

func appendSourceChanges(report *CatalogDiffReport, oldTool, newTool Tool) {
	oldSource := strings.TrimSpace(oldTool.effectiveSource())
	newSource := strings.TrimSpace(newTool.effectiveSource())
	oldSourceQuality := strings.TrimSpace(oldTool.SourceQuality)
	newSourceQuality := strings.TrimSpace(newTool.SourceQuality)
	if oldSource == newSource && oldSourceQuality == newSourceQuality {
		return
	}
	report.SourceChanges = append(report.SourceChanges, SourceChange{
		Identity: buildToolIdentity(newTool),
		Old: struct {
			Source        string `json:"source"`
			SourceQuality string `json:"source_quality"`
		}{
			Source:        oldSource,
			SourceQuality: oldSourceQuality,
		},
		New: struct {
			Source        string `json:"source"`
			SourceQuality string `json:"source_quality"`
		}{
			Source:        newSource,
			SourceQuality: newSourceQuality,
		},
	})
}

func appendSupportsChanges(report *CatalogDiffReport, oldTool, newTool Tool) {
	if oldTool.SupportsDryRun == newTool.SupportsDryRun && oldTool.SupportsAll == newTool.SupportsAll {
		return
	}
	report.SupportsChanges = append(report.SupportsChanges, SupportsChange{
		Identity: buildToolIdentity(newTool),
		Old: struct {
			SupportsDryRun bool `json:"supports_dry_run"`
			SupportsAll    bool `json:"supports_all"`
		}{
			SupportsDryRun: oldTool.SupportsDryRun,
			SupportsAll:    oldTool.SupportsAll,
		},
		New: struct {
			SupportsDryRun bool `json:"supports_dry_run"`
			SupportsAll    bool `json:"supports_all"`
		}{
			SupportsDryRun: newTool.SupportsDryRun,
			SupportsAll:    newTool.SupportsAll,
		},
	})
}

func oldIdentityKey(tool Tool) string {
	group := strings.ToLower(strings.TrimSpace(tool.Group))
	action := strings.ToLower(strings.TrimSpace(tool.Action))
	if group == "" || action == "" {
		return ""
	}
	return group + "." + action
}

func buildSummary(report CatalogDiffReport) CatalogDiffSummary {
	summary := CatalogDiffSummary{
		Added:                    len(report.Added),
		Removed:                  len(report.Removed),
		IdentityMetadataChanges:  len(report.IdentityMetadataChanges),
		MethodPathChanges:        len(report.MethodPathChanges),
		InputSchemaDigestChanges: len(report.InputSchemaDigestChanges),
		RiskLevelChanges:         len(report.RiskLevelChanges),
		SourceChanges:            len(report.SourceChanges),
		SupportsChanges:          len(report.SupportsChanges),
	}
	summary.TotalChanges = summary.Added + summary.Removed + summary.IdentityMetadataChanges + summary.MethodPathChanges + summary.InputSchemaDigestChanges + summary.RiskLevelChanges + summary.SourceChanges + summary.SupportsChanges
	return summary
}

func sortChangeLists(report *CatalogDiffReport) {
	sort.Slice(report.Added, func(i, j int) bool {
		return report.Added[i].String() < report.Added[j].String()
	})
	sort.Slice(report.Removed, func(i, j int) bool {
		return report.Removed[i].String() < report.Removed[j].String()
	})
	sort.Slice(report.IdentityMetadataChanges, func(i, j int) bool {
		return report.IdentityMetadataChanges[i].Identity.String() < report.IdentityMetadataChanges[j].Identity.String()
	})
	sort.Slice(report.MethodPathChanges, func(i, j int) bool {
		return report.MethodPathChanges[i].Identity.String() < report.MethodPathChanges[j].Identity.String()
	})
	sort.Slice(report.InputSchemaDigestChanges, func(i, j int) bool {
		return report.InputSchemaDigestChanges[i].Identity.String() < report.InputSchemaDigestChanges[j].Identity.String()
	})
	sort.Slice(report.RiskLevelChanges, func(i, j int) bool {
		return report.RiskLevelChanges[i].Identity.String() < report.RiskLevelChanges[j].Identity.String()
	})
	sort.Slice(report.SourceChanges, func(i, j int) bool {
		return report.SourceChanges[i].Identity.String() < report.SourceChanges[j].Identity.String()
	})
	sort.Slice(report.SupportsChanges, func(i, j int) bool {
		return report.SupportsChanges[i].Identity.String() < report.SupportsChanges[j].Identity.String()
	})
}

func inputSchemaDigest(schema map[string]any) (string, error) {
	normalized := map[string]any{}
	for key, value := range schema {
		normalized[key] = normalizeSchemaValue(value)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeSchemaValue(value any) any {
	obj, ok := value.(map[string]any)
	if !ok {
		arr, ok := value.([]any)
		if !ok {
			return value
		}
		out := make([]any, 0, len(arr))
		for _, item := range arr {
			out = append(out, normalizeSchemaValue(item))
		}
		return out
	}
	out := make(map[string]any, len(obj))
	for key, val := range obj {
		out[strings.TrimSpace(key)] = normalizeSchemaValue(val)
	}
	return out
}

func (report CatalogDiffReport) FormatText() string {
	var lines []string
	lines = append(lines, "catalog diff summary")
	lines = append(lines, "==================")
	lines = append(lines, fmt.Sprintf("added: %d", len(report.Added)))
	lines = append(lines, formatToolIdentities("added_tools", report.Added)...)
	lines = append(lines, fmt.Sprintf("removed: %d", len(report.Removed)))
	lines = append(lines, formatToolIdentities("removed_tools", report.Removed)...)
	lines = append(lines, fmt.Sprintf("identity/action metadata changes: %d", len(report.IdentityMetadataChanges)))
	lines = append(lines, formatIdentityMetadata("identity_metadata_changes", report.IdentityMetadataChanges)...)
	lines = append(lines, fmt.Sprintf("method/path changes: %d", len(report.MethodPathChanges)))
	lines = append(lines, formatMethodPathChanges(report.MethodPathChanges)...)
	lines = append(lines, fmt.Sprintf("input schema digest changes: %d", len(report.InputSchemaDigestChanges)))
	lines = append(lines, formatDigestChanges(report.InputSchemaDigestChanges)...)
	lines = append(lines, fmt.Sprintf("risk_level changes: %d", len(report.RiskLevelChanges)))
	lines = append(lines, formatRiskChanges(report.RiskLevelChanges)...)
	lines = append(lines, fmt.Sprintf("source/source_quality changes: %d", len(report.SourceChanges)))
	lines = append(lines, formatSourceChanges(report.SourceChanges)...)
	lines = append(lines, fmt.Sprintf("supports_dry_run/supports_all changes: %d", len(report.SupportsChanges)))
	lines = append(lines, formatSupportsChanges(report.SupportsChanges)...)
	return strings.Join(lines, "\n")
}

func formatToolIdentities(prefix string, identities []ToolIdentity) []string {
	out := make([]string, 0, len(identities))
	for _, identity := range identities {
		out = append(out, "  - ["+prefix+"] "+identity.String())
	}
	return out
}

func formatIdentityMetadata(prefix string, changes []IdentityMetadataChange) []string {
	out := make([]string, 0, len(changes))
	for _, item := range changes {
		out = append(out, fmt.Sprintf("  - [%s] %s [%s]", prefix, item.Identity.String(), strings.Join(item.ChangedFields, ",")))
	}
	return out
}

func formatMethodPathChanges(changes []MethodPathChange) []string {
	out := make([]string, 0, len(changes))
	for _, item := range changes {
		out = append(out, fmt.Sprintf("  - [method_path] %s old=%s %s new=%s %s", item.Identity, item.Old.Method, item.Old.Path, item.New.Method, item.New.Path))
	}
	return out
}

func formatDigestChanges(changes []InputSchemaDigestChange) []string {
	out := make([]string, 0, len(changes))
	for _, item := range changes {
		out = append(out, fmt.Sprintf("  - [input_schema] %s", item.Identity.String()))
	}
	return out
}

func formatRiskChanges(changes []RiskLevelChange) []string {
	out := make([]string, 0, len(changes))
	for _, item := range changes {
		out = append(out, fmt.Sprintf("  - [risk_level] %s old=%s new=%s", item.Identity.String(), item.Old, item.New))
	}
	return out
}

func formatSourceChanges(changes []SourceChange) []string {
	out := make([]string, 0, len(changes))
	for _, item := range changes {
		out = append(out, fmt.Sprintf("  - [source] %s old=%s/%s new=%s/%s", item.Identity.String(), item.Old.Source, item.Old.SourceQuality, item.New.Source, item.New.SourceQuality))
	}
	return out
}

func formatSupportsChanges(changes []SupportsChange) []string {
	out := make([]string, 0, len(changes))
	for _, item := range changes {
		out = append(out, fmt.Sprintf("  - [supports] %s old=(dry_run=%t,all=%t) new=(dry_run=%t,all=%t)", item.Identity.String(), item.Old.SupportsDryRun, item.Old.SupportsAll, item.New.SupportsDryRun, item.New.SupportsAll))
	}
	return out
}
