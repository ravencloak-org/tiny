# Deploy TinyRaven to AWS (CloudFormation)

[`cloudformation/tinyraven-template.yaml`](../../cloudformation/tinyraven-template.yaml)
provisions a single-node TinyRaven: a VPC, public subnets, security groups, an
EC2 instance running `tr` as a systemd service, an ElastiCache Redis (single
node), and an Elastic IP. **ClickHouse is external** — supply its endpoint as a
parameter (ClickHouse Cloud or your own host).

## One-click

[![Launch on AWS](https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png)](https://console.aws.amazon.com/cloudformation/home#/stacks/new?stackName=tinyraven&templateURL=https://raw.githubusercontent.com/ravencloak-org/tiny/main/cloudformation/tinyraven-template.yaml)

Fill in the parameters, create the stack (~10–15 minutes), then read the
`TinyRavenURL` output.

## CLI

```bash
aws cloudformation deploy \
  --stack-name tinyraven \
  --template-file cloudformation/tinyraven-template.yaml \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    KeyName=my-keypair \
    ClickHouseEndpoint=https://abc.clickhouse.cloud:8443 \
    ClickHouseNative=abc.clickhouse.cloud:9440 \
    AdminToken=$(openssl rand -hex 24)
```

## Parameters

| Parameter | Default | Notes |
|-----------|---------|-------|
| `InstanceType` | `t3.medium` | EC2 size |
| `KeyName` | — | Existing EC2 key pair (for SSH) |
| `SSHLocation` | `0.0.0.0/0` | CIDR allowed to SSH — restrict this |
| `TinyRavenVersion` | `latest` | `tr` release to download (`latest` or `vX.Y.Z`) |
| `ClickHouseEndpoint` | — | ClickHouse HTTP endpoint (required) |
| `ClickHouseNative` | `""` | ClickHouse native TCP host:port |
| `ClickHouseDB` | `tr_main` | Database |
| `RedisEndpoint` | `""` | Reuse an existing Redis; blank = provision ElastiCache |
| `AdminToken` | — | Bootstrap admin token (NoEcho) |

> Pin `TinyRavenVersion` to a tag (e.g. `v0.1.0`) for reproducible UserData
> downloads. `latest` relies on a best-effort release-asset filename.

## Outputs

- `TinyRavenURL` — `http://<eip>:8000`
- `HealthCheck` — `http://<eip>:8000/health`
- `DatabaseEndpoint` — the Redis endpoint in use
- `SSHCommand` — ready-to-paste SSH command

## Verify

```bash
curl "$(aws cloudformation describe-stacks --stack-name tinyraven \
  --query "Stacks[0].Outputs[?OutputKey=='HealthCheck'].OutputValue" --output text)"
```
