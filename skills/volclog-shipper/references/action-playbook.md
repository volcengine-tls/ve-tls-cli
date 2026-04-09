# Shipper Action Playbook

## Create / Modify / Delete

先看约束：

```bash
volclog api shipper CreateShipper --describe
volclog api shipper ModifyShipper --describe
volclog api shipper DeleteShipper --describe
```

需要 body 时：

```bash
volclog api shipper CreateShipper --print-request-template=full
volclog --dry-run api shipper CreateShipper --request file://req.json
```

## Describe

列举或查单个配置时：

```bash
volclog api shipper DescribeShippers --describe
volclog api shipper DescribeShippers --all
```

如果是复数 `Describe...s`，可评估是否使用 `--all`。

如果是看单个配置详情且返回较大，优先直接落文件。

常见闭环：

```bash
volclog api shipper DescribeShippers --describe
volclog api shipper CreateShipper --describe
volclog api shipper ModifyShipper --describe
volclog api shipper DeleteShipper --describe
```

## Retry Task

任务重试时：

```bash
volclog api shipper RetryShipperTask --describe
```
