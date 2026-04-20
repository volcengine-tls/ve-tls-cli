# Trace Action Playbook

## Instance Management

```bash
volclog api trace DescribeTraceInstances --describe
volclog api trace DescribeTraceInstances --all
volclog api trace DescribeTraceInstance --describe
volclog api trace CreateTraceInstance --describe
volclog api trace ModifyTraceInstance --describe
```

如果目标是“把实例都列全”，优先补 `--all`。
如果是查单个实例详情且返回较大，优先：

```bash
volclog --output-mode file --output-file ./trace-instance-detail.json api trace DescribeTraceInstance --TraceInstanceId <TraceInstanceId>
```

如果用户目标是“先建后查”，推荐顺序：

```bash
volclog api trace CreateTraceInstance --describe
volclog api trace CreateTraceInstance --print-request-template=full
volclog api trace DescribeTraceInstances --describe
volclog api trace DescribeTraceInstance --describe
```

## Trace / Span Query

```bash
volclog api trace DescribeTrace --describe
volclog api trace SearchTraces --describe
volclog api trace SearchSpans --describe
```

## Write Path

实例写入前：

```bash
volclog api trace CreateTraceInstance --print-request-template=full
volclog --dry-run api trace CreateTraceInstance --request file://req.json
```
