import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as rds from 'aws-cdk-lib/aws-rds';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as sns from 'aws-cdk-lib/aws-sns';
import * as snsSubscriptions from 'aws-cdk-lib/aws-sns-subscriptions';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as ecr from 'aws-cdk-lib/aws-ecr';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as secretsmanager from 'aws-cdk-lib/aws-secretsmanager';
import * as logs from 'aws-cdk-lib/aws-logs';

export interface BaobotSharedStackProps extends cdk.StackProps {
  /** Email address for SNS notification subscription */
  notificationEmail?: string;
}

export class BaobotSharedStack extends cdk.Stack {
  public readonly vpc: ec2.Vpc;
  public readonly cluster: ecs.Cluster;
  public readonly ecrRepository: ecr.Repository;
  public readonly sharedBaseManagedPolicy: iam.ManagedPolicy;
  public readonly snsTopic: sns.Topic;
  public readonly rdsCluster: rds.DatabaseCluster;
  public readonly budgetTable: dynamodb.Table;
  public readonly sharedMemoryBucket: s3.Bucket;
  public readonly ecsSecurityGroup: ec2.SecurityGroup;
  public readonly dbSecret: secretsmanager.ISecret;

  constructor(scope: Construct, id: string, props: BaobotSharedStackProps = {}) {
    super(scope, id, props);

    // ── 1. VPC ────────────────────────────────────────────────────────────────
    this.vpc = new ec2.Vpc(this, 'BaobotVpc', {
      maxAzs: 2,
      natGateways: 1,
      subnetConfiguration: [
        {
          cidrMask: 24,
          name: 'Public',
          subnetType: ec2.SubnetType.PUBLIC,
        },
        {
          cidrMask: 24,
          name: 'Private',
          subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS,
        },
      ],
    });

    // ── 2. ECS Security Group (allows outbound 443) ───────────────────────────
    this.ecsSecurityGroup = new ec2.SecurityGroup(this, 'EcsSecurityGroup', {
      vpc: this.vpc,
      description: 'Security group for BaoBot ECS tasks',
      allowAllOutbound: false,
    });
    this.ecsSecurityGroup.addEgressRule(
      ec2.Peer.anyIpv4(),
      ec2.Port.tcp(443),
      'Allow HTTPS outbound',
    );

    // ── 3. RDS Aurora Serverless v2 ───────────────────────────────────────────
    const dbSecurityGroup = new ec2.SecurityGroup(this, 'DbSecurityGroup', {
      vpc: this.vpc,
      description: 'Security group for BaoBot RDS',
      allowAllOutbound: false,
    });
    dbSecurityGroup.addIngressRule(
      this.ecsSecurityGroup,
      ec2.Port.tcp(5432),
      'Allow PostgreSQL from ECS tasks',
    );

    const dbCredentialsSecret = new secretsmanager.Secret(this, 'DbCredentials', {
      secretName: 'baobotDbCredentials',
      generateSecretString: {
        secretStringTemplate: JSON.stringify({ username: 'baobotadmin' }),
        generateStringKey: 'password',
        excludePunctuation: true,
        includeSpace: false,
      },
    });
    this.dbSecret = dbCredentialsSecret;

    this.rdsCluster = new rds.DatabaseCluster(this, 'BaobotRds', {
      engine: rds.DatabaseClusterEngine.auroraPostgres({
        version: rds.AuroraPostgresEngineVersion.VER_15_4,
      }),
      credentials: rds.Credentials.fromSecret(dbCredentialsSecret),
      serverlessV2MinCapacity: 0.5,
      serverlessV2MaxCapacity: 8,
      writer: rds.ClusterInstance.serverlessV2('writer'),
      vpc: this.vpc,
      vpcSubnets: { subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS },
      securityGroups: [dbSecurityGroup],
      storageEncrypted: true,
      defaultDatabaseName: 'baobot',
    });

    // ── 4. DynamoDB budget table ──────────────────────────────────────────────
    this.budgetTable = new dynamodb.Table(this, 'BaobotBudget', {
      tableName: 'baobotBudget',
      partitionKey: { name: 'botId', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'date', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      timeToLiveAttribute: 'expiresAt',
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ── 5. S3 shared memory bucket ────────────────────────────────────────────
    this.sharedMemoryBucket = new s3.Bucket(this, 'BaobotSharedMemory', {
      bucketName: `baobotsharedmemory-${this.account}-${this.region}`,
      versioned: true,
      encryption: s3.BucketEncryption.S3_MANAGED,
      blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
      lifecycleRules: [
        {
          id: 'ExpireOldVersions',
          noncurrentVersionExpiration: cdk.Duration.days(90),
          enabled: true,
        },
      ],
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ── 6. SNS topic ──────────────────────────────────────────────────────────
    this.snsTopic = new sns.Topic(this, 'BaobotNotifications', {
      topicName: 'baobotNotifications',
      displayName: 'BaoBot Notifications',
    });

    const notificationEmail =
      props.notificationEmail ?? this.node.tryGetContext('notificationEmail');
    if (notificationEmail) {
      this.snsTopic.addSubscription(
        new snsSubscriptions.EmailSubscription(notificationEmail),
      );
    }

    // ── 7. ECS Cluster ────────────────────────────────────────────────────────
    this.cluster = new ecs.Cluster(this, 'BaobotCluster', {
      vpc: this.vpc,
      containerInsightsV2: ecs.ContainerInsights.ENABLED,
    });

    // ── 8. ECR repository ─────────────────────────────────────────────────────
    this.ecrRepository = new ecr.Repository(this, 'BaobotRuntime', {
      repositoryName: 'baobot-runtime',
      imageTagMutability: ecr.TagMutability.IMMUTABLE,
      imageScanOnPush: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ── 9. Shared base IAM managed policy ─────────────────────────────────────
    this.sharedBaseManagedPolicy = new iam.ManagedPolicy(
      this,
      'BaobotSharedBasePolicy',
      {
        managedPolicyName: 'BaobotSharedBasePolicy',
        description:
          'Base permissions shared by all BaoBot worker ECS task roles',
        statements: [
          // Bedrock – Claude model invocations
          new iam.PolicyStatement({
            sid: 'BedrockInvokeModel',
            effect: iam.Effect.ALLOW,
            actions: ['bedrock:InvokeModel', 'bedrock:InvokeModelWithResponseStream'],
            resources: [
              `arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-*`,
            ],
          }),
          // SNS – subscribe to notifications topic
          new iam.PolicyStatement({
            sid: 'SnsSubscribe',
            effect: iam.Effect.ALLOW,
            actions: ['sns:Subscribe', 'sns:Publish'],
            resources: [this.snsTopic.topicArn],
          }),
          // S3 – read shared memory bucket
          new iam.PolicyStatement({
            sid: 'S3SharedMemoryRead',
            effect: iam.Effect.ALLOW,
            actions: ['s3:GetObject', 's3:ListBucket'],
            resources: [
              this.sharedMemoryBucket.bucketArn,
              `${this.sharedMemoryBucket.bucketArn}/*`,
            ],
          }),
          // DynamoDB – budget table
          new iam.PolicyStatement({
            sid: 'DynamoDBBudget',
            effect: iam.Effect.ALLOW,
            actions: [
              'dynamodb:GetItem',
              'dynamodb:PutItem',
              'dynamodb:UpdateItem',
            ],
            resources: [this.budgetTable.tableArn],
          }),
          // Secrets Manager – shared secrets
          new iam.PolicyStatement({
            sid: 'SecretsManagerShared',
            effect: iam.Effect.ALLOW,
            actions: ['secretsmanager:GetSecretValue'],
            resources: [
              `arn:aws:secretsmanager:${this.region}:${this.account}:secret:baobot*`,
            ],
          }),
          // CloudWatch Logs
          new iam.PolicyStatement({
            sid: 'CloudWatchLogs',
            effect: iam.Effect.ALLOW,
            actions: [
              'logs:CreateLogStream',
              'logs:PutLogEvents',
              'logs:DescribeLogStreams',
            ],
            resources: [`arn:aws:logs:${this.region}:${this.account}:log-group:/baobotRuntime*`],
          }),
          // SSM – read ADOT config parameters
          new iam.PolicyStatement({
            sid: 'SsmReadOtelConfig',
            effect: iam.Effect.ALLOW,
            actions: ['ssm:GetParameter'],
            resources: [
              `arn:aws:ssm:${this.region}:${this.account}:parameter/baobotRuntime/*`,
            ],
          }),
          // ECS – allow task to interact with its own service
          new iam.PolicyStatement({
            sid: 'EcsExec',
            effect: iam.Effect.ALLOW,
            actions: [
              'ssmmessages:CreateControlChannel',
              'ssmmessages:CreateDataChannel',
              'ssmmessages:OpenControlChannel',
              'ssmmessages:OpenDataChannel',
            ],
            resources: ['*'],
          }),
        ],
      },
    );

    // ── 10. Admin Secrets Manager secret ──────────────────────────────────────
    new secretsmanager.Secret(this, 'BaobotAdmin', {
      secretName: 'baobotAdmin',
      description: 'BaoBot admin credentials',
      generateSecretString: {
        secretStringTemplate: JSON.stringify({ username: 'admin' }),
        generateStringKey: 'password',
        excludePunctuation: true,
        includeSpace: false,
        passwordLength: 32,
      },
    });

    // ── 11. CloudWatch Log Group ──────────────────────────────────────────────
    new logs.LogGroup(this, 'BaobotLogGroup', {
      logGroupName: '/baobotRuntime',
      retention: logs.RetentionDays.ONE_MONTH,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ── Stack outputs ─────────────────────────────────────────────────────────
    new cdk.CfnOutput(this, 'VpcId', {
      value: this.vpc.vpcId,
      exportName: 'BaobotVpcId',
    });
    new cdk.CfnOutput(this, 'ClusterArn', {
      value: this.cluster.clusterArn,
      exportName: 'BaobotClusterArn',
    });
    new cdk.CfnOutput(this, 'EcrRepositoryUri', {
      value: this.ecrRepository.repositoryUri,
      exportName: 'BaobotEcrRepositoryUri',
    });
    new cdk.CfnOutput(this, 'SharedPolicyArn', {
      value: this.sharedBaseManagedPolicy.managedPolicyArn,
      exportName: 'BaobotSharedPolicyArn',
    });
    new cdk.CfnOutput(this, 'SnsTopicArn', {
      value: this.snsTopic.topicArn,
      exportName: 'BaobotSnsTopicArn',
    });
    new cdk.CfnOutput(this, 'RdsEndpoint', {
      value: this.rdsCluster.clusterEndpoint.hostname,
      exportName: 'BaobotRdsEndpoint',
    });
    new cdk.CfnOutput(this, 'DynamoDbTableName', {
      value: this.budgetTable.tableName,
      exportName: 'BaobotDynamoDbTableName',
    });
    new cdk.CfnOutput(this, 'SharedMemoryBucketName', {
      value: this.sharedMemoryBucket.bucketName,
      exportName: 'BaobotSharedMemoryBucketName',
    });
    new cdk.CfnOutput(this, 'EcsSecurityGroupId', {
      value: this.ecsSecurityGroup.securityGroupId,
      exportName: 'BaobotEcsSecurityGroupId',
    });
  }
}
