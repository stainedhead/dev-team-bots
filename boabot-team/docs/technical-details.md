# Technical Details — boabot-team

## CDK Stack Structure

```
cdk/
  bin/
    app.ts              # CDK app entry point
  lib/
    team-stack.ts       # main stack — reads team.yaml, iterates bots
    bot-construct.ts    # reusable construct for a single bot's resources
    config.ts           # team.yaml parser and type definitions
  test/
    team-stack.test.ts  # CDK assertion tests
  package.json
  tsconfig.json
  cdk.json
```

## team.yaml Parsing

The CDK stack reads `../team.yaml` at synth time. Each enabled bot entry is passed to `BotConstruct`, which provisions all per-bot resources. Disabled entries are parsed but skipped.

## BotConstruct Resources

For each bot, `BotConstruct` creates:

```
S3 Bucket (private memory)
  └── S3 Vectors access enabled
  └── S3 Files access enabled

SQS Queue (inbound)
  └── Dead-letter queue (maxReceiveCount: 3)
  └── Message retention: 14 days

IAM Role
  └── ECS task execution trust policy
  └── S3: own bucket (r/w), team bucket (r)
  └── SQS: own queue (send, receive, delete)
  └── SNS: broadcast topic (publish)
  └── Bedrock: InvokeModel
  └── Secrets Manager: own path prefix (read)

ECS Task Definition
  └── Container: shared ECR image
  └── Environment: CONFIG_PATH, QUEUE_URL, PRIVATE_BUCKET, TEAM_BUCKET, SNS_TOPIC_ARN
  └── Secrets: model provider keys from Secrets Manager

ECS Service
  └── Desired count: 1
  └── Cluster: shared cluster (imported from boabot/cdk stack)
```

## Cross-Stack References

Shared stack outputs are imported via `Fn.importValue`:

```typescript
const clusterArn = Fn.importValue('BoabotClusterArn');
const ecrUri = Fn.importValue('BoabotEcrUri');
const snsTopicArn = Fn.importValue('BoabotSnsTopicArn');
const teamBucketName = Fn.importValue('BoabotTeamBucketName');
```

The shared stack must export these values and be deployed before the team stack.
