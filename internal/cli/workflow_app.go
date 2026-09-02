package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

const (
	appResolveResourcesWorkflowID       = "app.resolve-resources"
	appResolveTopicIDsWorkflowID        = "app.resolve-topic-ids"
	appDescribeOperationID              = "app.describe"
	appDescribeLogAppOperationID        = "log-app.describe"
	appDescribeTraceInstanceOperationID = "trace.describe-trace-instance"
)

type appWorkflowDescribeAppResponse struct {
	AppType   string `json:"AppType"`
	Region    string `json:"Region"`
	Resources []struct {
		ID string `json:"Id"`
	} `json:"Resources"`
}

type appWorkflowDescribeLogAppResponse struct {
	LogAppID         string `json:"LogAppId"`
	LogAppName       string `json:"LogAppName"`
	RelatedResources []struct {
		Region       string `json:"Region"`
		ResourceID   string `json:"ResourceID"`
		ResourceName string `json:"ResourceName"`
		DisplayName  string `json:"DisplayName"`
		ResourceType *int   `json:"ResourceType"`
	} `json:"RelatedResourceList"`
}

type appWorkflowDescribeTraceResponse struct {
	TraceTopicID      string `json:"TraceTopicId"`
	DependencyTopicID string `json:"DependencyTopicId"`
}

type appWorkflowApp struct {
	AppID   string `json:"AppId"`
	AppType string `json:"AppType"`
	Region  string `json:"Region,omitempty"`
}

type appWorkflowNode struct {
	Kind         string `json:"Kind"`
	ID           string `json:"Id"`
	Region       string `json:"Region,omitempty"`
	ResourceType *int   `json:"ResourceType,omitempty"`
	ResourceName string `json:"ResourceName,omitempty"`
	DisplayName  string `json:"DisplayName,omitempty"`
}

type appWorkflowEdge struct {
	FromKind string `json:"FromKind"`
	FromID   string `json:"FromId"`
	Relation string `json:"Relation"`
	ToKind   string `json:"ToKind"`
	ToID     string `json:"ToId"`
	Region   string `json:"Region,omitempty"`
}

type appWorkflowResourceGraph struct {
	App              appWorkflowApp    `json:"App"`
	Nodes            []appWorkflowNode `json:"Nodes"`
	Edges            []appWorkflowEdge `json:"Edges"`
	LogAppIDs        []string          `json:"LogAppIds"`
	TraceInstanceIDs []string          `json:"TraceInstanceIds"`
	TopicIDs         []string          `json:"TopicIds"`
}

type appWorkflowTopicResult struct {
	TopicIDs []string `json:"TopicIds"`
}

type appWorkflowGraphBuilder struct {
	graph     appWorkflowResourceGraph
	nodeIndex map[string]int
	edges     map[string]struct{}
	lists     map[string]map[string]struct{}
}

func appResolveResources(ctx *Context, input map[string]any) (any, error) {
	return resolveAppResourceGraph(ctx, input, appResolveResourcesWorkflowID)
}

func appResolveTopicIDs(ctx *Context, input map[string]any) (any, error) {
	result, err := resolveAppResourceGraph(ctx, input, appResolveTopicIDsWorkflowID)
	if err != nil {
		return nil, err
	}
	if ctx != nil && ctx.DryRun {
		return result, nil
	}
	graph, ok := result.(appWorkflowResourceGraph)
	if !ok {
		return nil, errors.New("app topic resolution invalid result")
	}
	if graph.App.AppType != "LogApp" {
		return nil, newUnsupportedFeatureError(
			fmt.Sprintf("app topic resolution unsupported: AppType %q cannot resolve TopicIds; expected LogApp", graph.App.AppType),
			"use app.resolve-resources for non-LogApp applications",
		)
	}
	return appWorkflowTopicResult{TopicIDs: append([]string(nil), graph.TopicIDs...)}, nil
}

func resolveAppResourceGraph(ctx *Context, input map[string]any, workflowID string) (any, error) {
	if ctx == nil {
		return nil, errors.New("missing cli context")
	}
	appID, err := appWorkflowAppID(input)
	if err != nil {
		return nil, err
	}
	if ctx.DryRun {
		return appWorkflowDryRunPlan(ctx, workflowID, appID)
	}

	var app appWorkflowDescribeAppResponse
	if err := appWorkflowGET(ctx, appDescribeOperationID, map[string]string{"AppId": appID}, &app); err != nil {
		return nil, err
	}
	appType := strings.TrimSpace(app.AppType)
	if appType == "" {
		return nil, errors.New("app resource resolution invalid response: DescribeApp response is missing AppType")
	}

	builder := newAppWorkflowGraphBuilder(appWorkflowApp{
		AppID: appID, AppType: appType, Region: strings.TrimSpace(app.Region),
	})
	builder.addNode(appWorkflowNode{Kind: "App", ID: appID, Region: strings.TrimSpace(app.Region)})
	if appType != "LogApp" {
		for index, resource := range app.Resources {
			resourceID := strings.TrimSpace(resource.ID)
			if resourceID == "" {
				return nil, fmt.Errorf("app resource resolution invalid response: DescribeApp Resources[%d] is missing Id", index)
			}
			builder.addNode(appWorkflowNode{Kind: "AppResource", ID: resourceID, Region: builder.graph.App.Region})
			builder.addEdge(appWorkflowEdge{FromKind: "App", FromID: appID, Relation: "contains", ToKind: "AppResource", ToID: resourceID, Region: builder.graph.App.Region})
		}
		return builder.graph, nil
	}

	client, err := ctx.Client()
	if err != nil {
		return nil, err
	}
	executionRegion := strings.TrimSpace(client.Region)
	expectedRegion := builder.graph.App.Region
	if expectedRegion != "" && executionRegion != "" && !strings.EqualFold(expectedRegion, executionRegion) {
		return nil, fmt.Errorf("app resource resolution unsupported: App region %q differs from execution region %q", expectedRegion, executionRegion)
	}
	if expectedRegion == "" {
		expectedRegion = executionRegion
	}

	seenLogApps := map[string]struct{}{}
	seenTraceInstances := map[string]struct{}{}
	for index, resource := range app.Resources {
		logAppID := strings.TrimSpace(resource.ID)
		if logAppID == "" {
			return nil, fmt.Errorf("app resource resolution invalid response: DescribeApp Resources[%d] is missing Id", index)
		}
		if _, ok := seenLogApps[logAppID]; ok {
			continue
		}
		seenLogApps[logAppID] = struct{}{}
		builder.addList("log-app", logAppID)
		builder.addNode(appWorkflowNode{Kind: "LogApp", ID: logAppID, Region: expectedRegion})
		builder.addEdge(appWorkflowEdge{FromKind: "App", FromID: appID, Relation: "contains", ToKind: "LogApp", ToID: logAppID, Region: expectedRegion})

		var logApp appWorkflowDescribeLogAppResponse
		if err := appWorkflowGET(ctx, appDescribeLogAppOperationID, map[string]string{"LogAppId": logAppID}, &logApp); err != nil {
			return nil, err
		}
		builder.addNode(appWorkflowNode{Kind: "LogApp", ID: logAppID, Region: expectedRegion, ResourceName: strings.TrimSpace(logApp.LogAppName)})

		for relatedIndex, related := range logApp.RelatedResources {
			resourceRegion := strings.TrimSpace(related.Region)
			if resourceRegion == "" {
				resourceRegion = expectedRegion
			}
			if resourceRegion != "" && expectedRegion != "" && !strings.EqualFold(resourceRegion, expectedRegion) {
				return nil, fmt.Errorf("app resource resolution unsupported: related resource region %q differs from execution region %q", resourceRegion, expectedRegion)
			}
			resourceID := strings.TrimSpace(related.ResourceID)
			if resourceID == "" {
				return nil, fmt.Errorf("app resource resolution invalid response: DescribeLogApp RelatedResourceList[%d] is missing ResourceID", relatedIndex)
			}
			if related.ResourceType == nil {
				return nil, fmt.Errorf("app resource resolution invalid response: DescribeLogApp RelatedResourceList[%d] is missing ResourceType", relatedIndex)
			}
			node := appWorkflowNode{
				ID: resourceID, Region: resourceRegion, ResourceType: related.ResourceType,
				ResourceName: strings.TrimSpace(related.ResourceName), DisplayName: strings.TrimSpace(related.DisplayName),
			}
			switch *related.ResourceType {
			case 0:
				node.Kind = "TraceInstance"
				builder.addNode(node)
				builder.addEdge(appWorkflowEdge{FromKind: "LogApp", FromID: logAppID, Relation: "related_trace_instance", ToKind: node.Kind, ToID: resourceID, Region: resourceRegion})
				if _, ok := seenTraceInstances[resourceID]; ok {
					continue
				}
				seenTraceInstances[resourceID] = struct{}{}
				builder.addList("trace", resourceID)
				var trace appWorkflowDescribeTraceResponse
				if err := appWorkflowGET(ctx, appDescribeTraceInstanceOperationID, map[string]string{"TraceInstanceId": resourceID}, &trace); err != nil {
					return nil, err
				}
				builder.addTraceTopic(resourceID, resourceRegion, "trace_topic", trace.TraceTopicID)
				builder.addTraceTopic(resourceID, resourceRegion, "dependency_topic", trace.DependencyTopicID)
			case 1:
				node.Kind = "Topic"
				builder.addNode(node)
				builder.addList("topic", resourceID)
				builder.addEdge(appWorkflowEdge{FromKind: "LogApp", FromID: logAppID, Relation: "related_log_topic", ToKind: node.Kind, ToID: resourceID, Region: resourceRegion})
			case 2:
				node.Kind = "Topic"
				builder.addNode(node)
				builder.addList("topic", resourceID)
				builder.addEdge(appWorkflowEdge{FromKind: "LogApp", FromID: logAppID, Relation: "related_metric_topic", ToKind: node.Kind, ToID: resourceID, Region: resourceRegion})
			default:
				return nil, fmt.Errorf("app resource resolution invalid response: unsupported ResourceType %d in DescribeLogApp RelatedResourceList[%d]", *related.ResourceType, relatedIndex)
			}
		}
	}
	return builder.graph, nil
}

func newAppWorkflowGraphBuilder(app appWorkflowApp) *appWorkflowGraphBuilder {
	return &appWorkflowGraphBuilder{
		graph:     appWorkflowResourceGraph{App: app, Nodes: []appWorkflowNode{}, Edges: []appWorkflowEdge{}, LogAppIDs: []string{}, TraceInstanceIDs: []string{}, TopicIDs: []string{}},
		nodeIndex: map[string]int{}, edges: map[string]struct{}{},
		lists: map[string]map[string]struct{}{"log-app": {}, "trace": {}, "topic": {}},
	}
}

func (builder *appWorkflowGraphBuilder) addNode(node appWorkflowNode) {
	key := strings.ToLower(strings.TrimSpace(node.Region)) + "\x00" + node.Kind + "\x00" + strings.TrimSpace(node.ID)
	if index, ok := builder.nodeIndex[key]; ok {
		existing := &builder.graph.Nodes[index]
		if existing.ResourceName == "" {
			existing.ResourceName = node.ResourceName
		}
		if existing.DisplayName == "" {
			existing.DisplayName = node.DisplayName
		}
		if existing.ResourceType == nil {
			existing.ResourceType = node.ResourceType
		}
		return
	}
	builder.nodeIndex[key] = len(builder.graph.Nodes)
	builder.graph.Nodes = append(builder.graph.Nodes, node)
}

func (builder *appWorkflowGraphBuilder) addEdge(edge appWorkflowEdge) {
	key := strings.Join([]string{edge.FromKind, edge.FromID, edge.Relation, edge.ToKind, edge.ToID, strings.ToLower(edge.Region)}, "\x00")
	if _, ok := builder.edges[key]; ok {
		return
	}
	builder.edges[key] = struct{}{}
	builder.graph.Edges = append(builder.graph.Edges, edge)
}

func (builder *appWorkflowGraphBuilder) addList(kind, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, ok := builder.lists[kind][id]; ok {
		return
	}
	builder.lists[kind][id] = struct{}{}
	switch kind {
	case "log-app":
		builder.graph.LogAppIDs = append(builder.graph.LogAppIDs, id)
	case "trace":
		builder.graph.TraceInstanceIDs = append(builder.graph.TraceInstanceIDs, id)
	case "topic":
		builder.graph.TopicIDs = append(builder.graph.TopicIDs, id)
	}
}

func (builder *appWorkflowGraphBuilder) addTraceTopic(traceInstanceID, region, relation, topicID string) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return
	}
	builder.addNode(appWorkflowNode{Kind: "Topic", ID: topicID, Region: region})
	builder.addList("topic", topicID)
	builder.addEdge(appWorkflowEdge{FromKind: "TraceInstance", FromID: traceInstanceID, Relation: relation, ToKind: "Topic", ToID: topicID, Region: region})
}

func appWorkflowAppID(input map[string]any) (string, error) {
	value, ok := workflowInputValue(input, "AppId")
	if !ok {
		return "", errors.New("missing required field: AppId")
	}
	appID, ok := value.(string)
	if !ok {
		return "", errors.New("workflow field AppId expects string")
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return "", errors.New("missing required field: AppId")
	}
	return appID, nil
}

func appWorkflowGET(ctx *Context, operationID string, query map[string]string, target any) error {
	operation, err := appWorkflowOperation(operationID)
	if err != nil {
		return err
	}
	value, err := ctx.Do(operation.Wire.Method, operation.Wire.Path, query, nil, nil)
	if err != nil {
		return fmt.Errorf("%s failed: %w", operation.Action, err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("app resource resolution invalid response: encode %s response: %w", operation.Action, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("app resource resolution invalid response: decode %s response: %w", operation.Action, err)
	}
	return nil
}

func appWorkflowOperation(operationID string) (contract.Operation, error) {
	operation, ok := loadToolOperation(operationID)
	if !ok {
		return contract.Operation{}, fmt.Errorf("app resource resolution contract error: unknown operation %q", operationID)
	}
	if !strings.EqualFold(strings.TrimSpace(operation.Wire.Method), "GET") || strings.TrimSpace(operation.Wire.Path) == "" || strings.TrimSpace(operation.Action) == "" {
		return contract.Operation{}, fmt.Errorf("app resource resolution contract error: operation %q must define a GET action and path", operationID)
	}
	return operation, nil
}

func appWorkflowDryRunPlan(ctx *Context, workflowID, appID string) (map[string]any, error) {
	describeApp, err := appWorkflowOperation(appDescribeOperationID)
	if err != nil {
		return nil, err
	}
	describeLogApp, err := appWorkflowOperation(appDescribeLogAppOperationID)
	if err != nil {
		return nil, err
	}
	describeTrace, err := appWorkflowOperation(appDescribeTraceInstanceOperationID)
	if err != nil {
		return nil, err
	}
	plan := ctx.buildDryRunPlan(describeApp.Wire.Method, describeApp.Wire.Path, map[string]string{"AppId": appID}, nil, nil, nil, requestFormatJSON, false)
	plan["workflow"] = workflowID
	plan["steps"] = []map[string]any{
		{"action": describeApp.Action, "method": describeApp.Wire.Method, "path": describeApp.Wire.Path},
		{"action": describeLogApp.Action, "method": describeLogApp.Wire.Method, "path": describeLogApp.Wire.Path, "depends_on": "DescribeApp.Resources when AppType=LogApp"},
		{"action": describeTrace.Action, "method": describeTrace.Wire.Method, "path": describeTrace.Wire.Path, "depends_on": "DescribeLogApp.RelatedResourceList[ResourceType=0]"},
	}
	plan["note"] = "dependent resource IDs and region checks require live responses; no partial result is returned on failure"
	return plan, nil
}
