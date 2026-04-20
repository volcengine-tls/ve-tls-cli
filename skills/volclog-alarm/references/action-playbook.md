# Alarm Action Playbook

## Alarm Policy

```bash
volclog api alarm DescribeAlarms --describe
volclog api alarm CreateAlarm --describe
volclog api alarm ModifyAlarm --describe
volclog api alarm DisableAlarm --describe
volclog api alarm DeleteAlarm --describe
```

复杂 body 时：

```bash
volclog api alarm CreateAlarm --print-request-template=full
volclog --dry-run api alarm CreateAlarm --request file://req.json
```

## Webhook Integration

```bash
volclog api alarm DescribeAlarmWebhookIntegrations --describe
volclog api alarm CreateAlarmWebhookIntegration --describe
volclog api alarm ModifyAlarmWebhookIntegration --describe
```

## Content Template / Notify Group

先从：

```bash
volclog capabilities --group alarm --view text
```

里选对应 template / notify group action，再 `api alarm <Action> --describe`。
