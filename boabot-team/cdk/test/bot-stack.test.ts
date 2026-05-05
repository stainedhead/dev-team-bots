import * as cdk from 'aws-cdk-lib';
import { Template, Match } from 'aws-cdk-lib/assertions';
import { BaobotSharedStack } from '../lib/shared-stack';
import { BaobotBotStack } from '../lib/bot-stack';

let app: cdk.App;
let sharedStack: BaobotSharedStack;
let botStack: BaobotBotStack;
let template: Template;

beforeAll(() => {
  app = new cdk.App();
  sharedStack = new BaobotSharedStack(app, 'TestSharedStack', {
    env: { account: '123456789012', region: 'us-east-1' },
  });
  botStack = new BaobotBotStack(app, 'TestBotStack', {
    env: { account: '123456789012', region: 'us-east-1' },
    botName: 'architect',
    botRole: 'architect',
    imageTag: 'abc1234',
    desiredCount: 1,
    sharedStack,
  });
  template = Template.fromStack(botStack);
});

// ── SQS Queues ────────────────────────────────────────────────────────────────
describe('SQS queues', () => {
  test('creates the work queue', () => {
    template.hasResourceProperties('AWS::SQS::Queue', {
      QueueName: 'architect-work',
      VisibilityTimeout: 300,
      MessageRetentionPeriod: 1209600, // 14 days in seconds
    });
  });

  test('creates the DLQ', () => {
    template.hasResourceProperties('AWS::SQS::Queue', {
      QueueName: 'architect-dlq',
      MessageRetentionPeriod: 1209600,
    });
  });

  test('work queue has DLQ configured with maxReceiveCount 5', () => {
    template.hasResourceProperties('AWS::SQS::Queue', {
      QueueName: 'architect-work',
      RedrivePolicy: Match.objectLike({
        maxReceiveCount: 5,
      }),
    });
  });

  test('creates exactly 2 SQS queues', () => {
    template.resourceCountIs('AWS::SQS::Queue', 2);
  });
});

// ── S3 private bucket ─────────────────────────────────────────────────────────
describe('S3 private bucket', () => {
  test('bucket name includes bot name', () => {
    template.hasResourceProperties('AWS::S3::Bucket', {
      BucketName: 'baobotprivate-architect-123456789012-us-east-1',
    });
  });

  test('bucket has versioning enabled', () => {
    template.hasResourceProperties('AWS::S3::Bucket', {
      VersioningConfiguration: { Status: 'Enabled' },
    });
  });

  test('bucket blocks all public access', () => {
    template.hasResourceProperties('AWS::S3::Bucket', {
      PublicAccessBlockConfiguration: {
        BlockPublicAcls: true,
        BlockPublicPolicy: true,
        IgnorePublicAcls: true,
        RestrictPublicBuckets: true,
      },
    });
  });
});

// ── IAM ───────────────────────────────────────────────────────────────────────
describe('IAM task role', () => {
  test('creates a task role assumed by ECS tasks', () => {
    template.hasResourceProperties('AWS::IAM::Role', {
      RoleName: 'baobot-architect-task-role',
      AssumeRolePolicyDocument: {
        Statement: Match.arrayWith([
          Match.objectLike({
            Principal: { Service: 'ecs-tasks.amazonaws.com' },
            Action: 'sts:AssumeRole',
          }),
        ]),
      },
    });
  });

  test('inline policy restricts S3 access to the private bucket', () => {
    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: {
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['s3:GetObject', 's3:PutObject', 's3:DeleteObject']),
            Effect: 'Allow',
          }),
        ]),
      },
    });
  });
});

// ── ECS Task Definition ───────────────────────────────────────────────────────
describe('ECS task definition', () => {
  test('creates a Fargate task definition', () => {
    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      Family: 'baobot-architect',
      RequiresCompatibilities: ['FARGATE'],
      Cpu: '512',
      Memory: '1024',
    });
  });

  test('main container has required environment variables', () => {
    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      ContainerDefinitions: Match.arrayWith([
        Match.objectLike({
          Environment: Match.arrayWith([
            Match.objectLike({ Name: 'BOT_NAME', Value: 'architect' }),
            Match.objectLike({ Name: 'BOT_ROLE', Value: 'architect' }),
            Match.objectLike({ Name: 'AWS_REGION', Value: 'us-east-1' }),
          ]),
        }),
      ]),
    });
  });

  test('task definition includes ADOT sidecar container', () => {
    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      ContainerDefinitions: Match.arrayWith([
        Match.objectLike({
          Image: Match.stringLikeRegexp('aws-otel-collector'),
          Essential: false,
        }),
      ]),
    });
  });

  test('containers use awslogs log driver', () => {
    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      ContainerDefinitions: Match.arrayWith([
        Match.objectLike({
          LogConfiguration: {
            LogDriver: 'awslogs',
          },
        }),
      ]),
    });
  });
});

// ── ECS Fargate Service ───────────────────────────────────────────────────────
describe('ECS Fargate service', () => {
  test('creates a Fargate service for the bot', () => {
    template.hasResourceProperties('AWS::ECS::Service', {
      ServiceName: 'baobot-architect',
      DesiredCount: 1,
      LaunchType: 'FARGATE',
    });
  });
});

// ── CloudWatch Alarm ──────────────────────────────────────────────────────────
describe('CloudWatch DLQ alarm', () => {
  test('creates a DLQ depth alarm', () => {
    template.hasResourceProperties('AWS::CloudWatch::Alarm', {
      AlarmName: 'architect-dlq-depth',
      ComparisonOperator: 'GreaterThanThreshold',
      Threshold: 0,
    });
  });

  test('alarm sends to SNS', () => {
    template.hasResourceProperties('AWS::CloudWatch::Alarm', {
      AlarmActions: Match.anyValue(),
    });
  });
});

// ── SSM Parameter ─────────────────────────────────────────────────────────────
describe('SSM ADOT config parameter', () => {
  test('creates SSM parameter for ADOT config', () => {
    template.hasResourceProperties('AWS::SSM::Parameter', {
      Name: '/baobotRuntime/architect/otelConfig',
      Type: 'String',
    });
  });

  test('ADOT config contains CloudWatch exporter config', () => {
    template.hasResourceProperties('AWS::SSM::Parameter', {
      Value: Match.stringLikeRegexp('awsemf'),
    });
  });
});

// ── Stack outputs ─────────────────────────────────────────────────────────────
describe('Stack outputs', () => {
  test('exports work queue URL', () => {
    template.hasOutput('WorkQueueUrl', {
      Export: { Name: 'Baobot-architect-WorkQueueUrl' },
    });
  });

  test('exports private bucket name', () => {
    template.hasOutput('PrivateBucketName', {
      Export: { Name: 'Baobot-architect-PrivateBucketName' },
    });
  });

  test('exports service ARN', () => {
    template.hasOutput('ServiceArn', {
      Export: { Name: 'Baobot-architect-ServiceArn' },
    });
  });
});
