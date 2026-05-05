import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as ecr from 'aws-cdk-lib/aws-ecr';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as sqs from 'aws-cdk-lib/aws-sqs';
import * as sns from 'aws-cdk-lib/aws-sns';
import * as cloudwatch from 'aws-cdk-lib/aws-cloudwatch';
import * as cloudwatchActions from 'aws-cdk-lib/aws-cloudwatch-actions';
import * as ssm from 'aws-cdk-lib/aws-ssm';
import * as logs from 'aws-cdk-lib/aws-logs';
import { BaobotSharedStack } from './shared-stack';

export interface BaobotBotStackProps extends cdk.StackProps {
  /** The logical name of this bot (e.g. "orchestrator", "architect") */
  botName: string;
  /** The role/type of this bot (e.g. "orchestrator", "implementer") */
  botRole: string;
  /** ECR image tag (default: "latest") */
  imageTag?: string;
  /** Number of running tasks (default: 1) */
  desiredCount?: number;
  /** CPU units for the task definition (default: 512) */
  cpu?: number;
  /** Memory limit MiB for the task definition (default: 1024) */
  memoryLimitMiB?: number;
  /** Reference to the shared stack */
  sharedStack: BaobotSharedStack;
}

/**
 * Deploys one BaoBot worker: SQS queues, private S3 bucket, IAM role,
 * ECS Fargate service with ADOT sidecar, DLQ alarm, and OTel SSM parameter.
 */
export class BaobotBotStack extends cdk.Stack {
  public readonly workQueue: sqs.Queue;
  public readonly dlq: sqs.Queue;
  public readonly privateBucket: s3.Bucket;
  public readonly taskRole: iam.Role;
  public readonly service: ecs.FargateService;

  constructor(scope: Construct, id: string, props: BaobotBotStackProps) {
    super(scope, id, props);

    const {
      botName,
      botRole,
      imageTag = 'latest',
      desiredCount = 1,
      cpu = 512,
      memoryLimitMiB = 1024,
      sharedStack,
    } = props;

    // ── 1. SQS DLQ ───────────────────────────────────────────────────────────
    this.dlq = new sqs.Queue(this, 'Dlq', {
      queueName: `${botName}-dlq`,
      retentionPeriod: cdk.Duration.days(14),
    });

    // ── 2. SQS work queue ─────────────────────────────────────────────────────
    this.workQueue = new sqs.Queue(this, 'WorkQueue', {
      queueName: `${botName}-work`,
      visibilityTimeout: cdk.Duration.seconds(300),
      retentionPeriod: cdk.Duration.days(14),
      deadLetterQueue: {
        queue: this.dlq,
        maxReceiveCount: 5,
      },
    });

    // ── 3. Private S3 bucket ──────────────────────────────────────────────────
    this.privateBucket = new s3.Bucket(this, 'PrivateBucket', {
      bucketName: `baobotprivate-${botName}-${this.account}-${this.region}`,
      versioned: true,
      encryption: s3.BucketEncryption.S3_MANAGED,
      blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ── 4. IAM task role ──────────────────────────────────────────────────────
    this.taskRole = new iam.Role(this, 'TaskRole', {
      roleName: `baobot-${botName}-task-role`,
      assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com'),
      managedPolicies: [sharedStack.sharedBaseManagedPolicy],
    });

    // Inline policy scoped to this bot's private bucket only
    this.taskRole.addToPolicy(
      new iam.PolicyStatement({
        sid: 'PrivateBucketAccess',
        effect: iam.Effect.ALLOW,
        actions: ['s3:GetObject', 's3:PutObject', 's3:DeleteObject', 's3:ListBucket'],
        resources: [
          this.privateBucket.bucketArn,
          `${this.privateBucket.bucketArn}/*`,
        ],
      }),
    );

    // SQS permissions scoped to this bot's own queues
    this.taskRole.addToPolicy(
      new iam.PolicyStatement({
        sid: 'SqsOwnQueues',
        effect: iam.Effect.ALLOW,
        actions: [
          'sqs:ReceiveMessage',
          'sqs:DeleteMessage',
          'sqs:GetQueueAttributes',
          'sqs:SendMessage',
        ],
        resources: [this.workQueue.queueArn, this.dlq.queueArn],
      }),
    );

    // ── 5. ECS Task Execution Role ────────────────────────────────────────────
    const executionRole = new iam.Role(this, 'ExecutionRole', {
      roleName: `baobot-${botName}-execution-role`,
      assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com'),
      managedPolicies: [
        iam.ManagedPolicy.fromAwsManagedPolicyName(
          'service-role/AmazonECSTaskExecutionRolePolicy',
        ),
      ],
    });
    // Allow execution role to read the DB secret for secrets injection
    executionRole.addToPolicy(
      new iam.PolicyStatement({
        sid: 'SecretsManagerForInjection',
        effect: iam.Effect.ALLOW,
        actions: ['secretsmanager:GetSecretValue'],
        resources: [sharedStack.dbSecret.secretArn],
      }),
    );

    // ── 6. CloudWatch Log Group for this bot ─────────────────────────────────
    const botLogGroup = new logs.LogGroup(this, 'BotLogGroup', {
      logGroupName: `/baobotRuntime/${botName}`,
      retention: logs.RetentionDays.ONE_MONTH,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ── 7. SSM parameter – ADOT collector config ──────────────────────────────
    const otelConfigYaml = `
extensions:
  health_check:

receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:

exporters:
  awsemf:
    namespace: BaoBot/${botName}
    region: ${this.region}
    log_group_name: /baobotRuntime/${botName}/metrics
    log_stream_name: otel

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [awsemf]
  extensions: [health_check]
`.trim();

    const otelConfigParam = new ssm.StringParameter(this, 'OtelConfig', {
      parameterName: `/baobotRuntime/${botName}/otelConfig`,
      stringValue: otelConfigYaml,
      description: `ADOT collector config for ${botName}`,
    });

    // ── 8. ECS Task Definition ────────────────────────────────────────────────
    const taskDefinition = new ecs.FargateTaskDefinition(this, 'TaskDef', {
      family: `baobot-${botName}`,
      cpu,
      memoryLimitMiB,
      taskRole: this.taskRole,
      executionRole,
    });

    // Main container
    const mainContainer = taskDefinition.addContainer('main', {
      image: ecs.ContainerImage.fromEcrRepository(
        sharedStack.ecrRepository,
        imageTag,
      ),
      essential: true,
      environment: {
        BOT_NAME: botName,
        BOT_ROLE: botRole,
        SQS_QUEUE_URL: this.workQueue.queueUrl,
        PRIVATE_BUCKET_NAME: this.privateBucket.bucketName,
        SHARED_BUCKET_NAME: sharedStack.sharedMemoryBucket.bucketName,
        SNS_TOPIC_ARN: sharedStack.snsTopic.topicArn,
        DYNAMODB_TABLE: sharedStack.budgetTable.tableName,
        AWS_REGION: this.region,
      },
      secrets: {
        DB_HOST: ecs.Secret.fromSecretsManager(sharedStack.dbSecret, 'host'),
        DB_USERNAME: ecs.Secret.fromSecretsManager(
          sharedStack.dbSecret,
          'username',
        ),
        DB_PASSWORD: ecs.Secret.fromSecretsManager(
          sharedStack.dbSecret,
          'password',
        ),
      },
      logging: ecs.LogDrivers.awsLogs({
        streamPrefix: botName,
        logGroup: botLogGroup,
      }),
    });
    mainContainer.addPortMappings({ containerPort: 8080 });

    // ADOT sidecar container
    const adotLogGroup = new logs.LogGroup(this, 'AdotLogGroup', {
      logGroupName: `/baobotRuntime/${botName}/adot`,
      retention: logs.RetentionDays.ONE_WEEK,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    taskDefinition.addContainer('adot', {
      image: ecs.ContainerImage.fromRegistry(
        'public.ecr.aws/aws-observability/aws-otel-collector:latest',
      ),
      essential: false,
      command: ['--config', `ssm:${otelConfigParam.parameterName}`],
      environment: {
        AWS_REGION: this.region,
      },
      logging: ecs.LogDrivers.awsLogs({
        streamPrefix: `${botName}-adot`,
        logGroup: adotLogGroup,
      }),
    });

    // ── 9. ECS Fargate Service ────────────────────────────────────────────────
    this.service = new ecs.FargateService(this, 'Service', {
      serviceName: `baobot-${botName}`,
      cluster: sharedStack.cluster,
      taskDefinition,
      desiredCount,
      vpcSubnets: { subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS },
      securityGroups: [sharedStack.ecsSecurityGroup],
      assignPublicIp: false,
    });

    // Auto-scaling
    const scaling = this.service.autoScaleTaskCount({
      minCapacity: 1,
      maxCapacity: 3,
    });
    scaling.scaleOnCpuUtilization('CpuScaling', {
      targetUtilizationPercent: 70,
      scaleInCooldown: cdk.Duration.seconds(60),
      scaleOutCooldown: cdk.Duration.seconds(60),
    });

    // ── 10. CloudWatch Alarm – DLQ depth > 0 ─────────────────────────────────
    const dlqAlarm = new cloudwatch.Alarm(this, 'DlqAlarm', {
      alarmName: `${botName}-dlq-depth`,
      alarmDescription: `DLQ for ${botName} has messages — investigate failed work items`,
      metric: this.dlq.metricApproximateNumberOfMessagesVisible({
        period: cdk.Duration.minutes(5),
        statistic: 'Maximum',
      }),
      threshold: 0,
      comparisonOperator:
        cloudwatch.ComparisonOperator.GREATER_THAN_THRESHOLD,
      evaluationPeriods: 1,
      treatMissingData: cloudwatch.TreatMissingData.NOT_BREACHING,
    });
    dlqAlarm.addAlarmAction(
      new cloudwatchActions.SnsAction(sharedStack.snsTopic as sns.Topic),
    );

    // ── Stack outputs ─────────────────────────────────────────────────────────
    new cdk.CfnOutput(this, 'WorkQueueUrl', {
      value: this.workQueue.queueUrl,
      exportName: `Baobot-${botName}-WorkQueueUrl`,
    });
    new cdk.CfnOutput(this, 'PrivateBucketName', {
      value: this.privateBucket.bucketName,
      exportName: `Baobot-${botName}-PrivateBucketName`,
    });
    new cdk.CfnOutput(this, 'ServiceArn', {
      value: this.service.serviceArn,
      exportName: `Baobot-${botName}-ServiceArn`,
    });
  }
}
