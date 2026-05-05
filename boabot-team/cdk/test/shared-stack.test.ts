import * as cdk from 'aws-cdk-lib';
import { Template, Match } from 'aws-cdk-lib/assertions';
import { BaobotSharedStack } from '../lib/shared-stack';

let app: cdk.App;
let stack: BaobotSharedStack;
let template: Template;

beforeAll(() => {
  app = new cdk.App();
  stack = new BaobotSharedStack(app, 'TestSharedStack', {
    env: { account: '123456789012', region: 'us-east-1' },
    notificationEmail: 'test@example.com',
  });
  template = Template.fromStack(stack);
});

// ── VPC ──────────────────────────────────────────────────────────────────────
describe('VPC', () => {
  test('creates a VPC with public and private subnets', () => {
    template.resourceCountIs('AWS::EC2::VPC', 1);
    // 2 AZs × 2 subnet types = 4 subnets
    template.resourceCountIs('AWS::EC2::Subnet', 4);
  });

  test('creates exactly 1 NAT gateway', () => {
    template.resourceCountIs('AWS::EC2::NatGateway', 1);
  });
});

// ── RDS ───────────────────────────────────────────────────────────────────────
describe('RDS Aurora Serverless v2', () => {
  test('creates an RDS DB cluster', () => {
    template.resourceCountIs('AWS::RDS::DBCluster', 1);
  });

  test('cluster uses Aurora PostgreSQL engine', () => {
    template.hasResourceProperties('AWS::RDS::DBCluster', {
      Engine: 'aurora-postgresql',
      DatabaseName: 'baobot',
    });
  });

  test('cluster has serverless v2 scaling configuration', () => {
    template.hasResourceProperties('AWS::RDS::DBCluster', {
      ServerlessV2ScalingConfiguration: {
        MinCapacity: 0.5,
        MaxCapacity: 8,
      },
    });
  });
});

// ── DynamoDB ──────────────────────────────────────────────────────────────────
describe('DynamoDB budget table', () => {
  test('creates the baobotBudget table', () => {
    template.hasResourceProperties('AWS::DynamoDB::Table', {
      TableName: 'baobotBudget',
      BillingMode: 'PAY_PER_REQUEST',
    });
  });

  test('table has correct key schema', () => {
    template.hasResourceProperties('AWS::DynamoDB::Table', {
      KeySchema: Match.arrayWith([
        Match.objectLike({ AttributeName: 'botId', KeyType: 'HASH' }),
        Match.objectLike({ AttributeName: 'date', KeyType: 'RANGE' }),
      ]),
    });
  });

  test('table has TTL enabled on expiresAt', () => {
    template.hasResourceProperties('AWS::DynamoDB::Table', {
      TimeToLiveSpecification: {
        AttributeName: 'expiresAt',
        Enabled: true,
      },
    });
  });
});

// ── S3 shared memory bucket ──────────────────────────────────────────────────
describe('S3 shared memory bucket', () => {
  test('bucket has versioning enabled', () => {
    template.hasResourceProperties('AWS::S3::Bucket', {
      VersioningConfiguration: { Status: 'Enabled' },
    });
  });

  test('bucket has SSE-S3 encryption', () => {
    template.hasResourceProperties('AWS::S3::Bucket', {
      BucketEncryption: {
        ServerSideEncryptionConfiguration: Match.arrayWith([
          Match.objectLike({
            ServerSideEncryptionByDefault: {
              SSEAlgorithm: 'AES256',
            },
          }),
        ]),
      },
    });
  });

  test('bucket blocks public access', () => {
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

// ── SNS ───────────────────────────────────────────────────────────────────────
describe('SNS topic', () => {
  test('creates the baobotNotifications topic', () => {
    template.hasResourceProperties('AWS::SNS::Topic', {
      TopicName: 'baobotNotifications',
    });
  });

  test('creates email subscription when notificationEmail is provided', () => {
    template.resourceCountIs('AWS::SNS::Subscription', 1);
    template.hasResourceProperties('AWS::SNS::Subscription', {
      Protocol: 'email',
      Endpoint: 'test@example.com',
    });
  });
});

// ── ECS Cluster ───────────────────────────────────────────────────────────────
describe('ECS cluster', () => {
  test('creates an ECS cluster', () => {
    template.resourceCountIs('AWS::ECS::Cluster', 1);
  });

  test('container insights is enabled', () => {
    template.hasResourceProperties('AWS::ECS::Cluster', {
      ClusterSettings: Match.arrayWith([
        Match.objectLike({ Name: Match.stringLikeRegexp('containerInsights'), Value: Match.stringLikeRegexp('enabled') }),
      ]),
    });
  });
});

// ── ECR ───────────────────────────────────────────────────────────────────────
describe('ECR repository', () => {
  test('creates ECR repository named baobot-runtime', () => {
    template.hasResourceProperties('AWS::ECR::Repository', {
      RepositoryName: 'baobot-runtime',
      ImageTagMutability: 'IMMUTABLE',
    });
  });

  test('scan on push is enabled', () => {
    template.hasResourceProperties('AWS::ECR::Repository', {
      ImageScanningConfiguration: { ScanOnPush: true },
    });
  });
});

// ── IAM Managed Policy ────────────────────────────────────────────────────────
describe('Shared base IAM managed policy', () => {
  test('creates the BaobotSharedBasePolicy', () => {
    template.hasResourceProperties('AWS::IAM::ManagedPolicy', {
      ManagedPolicyName: 'BaobotSharedBasePolicy',
    });
  });

  test('policy includes Bedrock InvokeModel permission', () => {
    template.hasResourceProperties('AWS::IAM::ManagedPolicy', {
      PolicyDocument: {
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['bedrock:InvokeModel']),
            Effect: 'Allow',
          }),
        ]),
      },
    });
  });

  test('policy includes DynamoDB budget table permissions', () => {
    template.hasResourceProperties('AWS::IAM::ManagedPolicy', {
      PolicyDocument: {
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['dynamodb:GetItem', 'dynamodb:PutItem', 'dynamodb:UpdateItem']),
            Effect: 'Allow',
          }),
        ]),
      },
    });
  });
});

// ── CloudWatch Log Group ──────────────────────────────────────────────────────
describe('CloudWatch log group', () => {
  test('creates /baobotRuntime log group with 30-day retention', () => {
    template.hasResourceProperties('AWS::Logs::LogGroup', {
      LogGroupName: '/baobotRuntime',
      RetentionInDays: 30,
    });
  });
});

// ── Stack outputs ─────────────────────────────────────────────────────────────
describe('Stack outputs', () => {
  test('exports VPC ID', () => {
    template.hasOutput('VpcId', { Export: { Name: 'BaobotVpcId' } });
  });

  test('exports cluster ARN', () => {
    template.hasOutput('ClusterArn', { Export: { Name: 'BaobotClusterArn' } });
  });

  test('exports ECR repository URI', () => {
    template.hasOutput('EcrRepositoryUri', {
      Export: { Name: 'BaobotEcrRepositoryUri' },
    });
  });

  test('exports SNS topic ARN', () => {
    template.hasOutput('SnsTopicArn', { Export: { Name: 'BaobotSnsTopicArn' } });
  });

  test('exports RDS endpoint', () => {
    template.hasOutput('RdsEndpoint', { Export: { Name: 'BaobotRdsEndpoint' } });
  });

  test('exports DynamoDB table name', () => {
    template.hasOutput('DynamoDbTableName', {
      Export: { Name: 'BaobotDynamoDbTableName' },
    });
  });

  test('exports shared memory bucket name', () => {
    template.hasOutput('SharedMemoryBucketName', {
      Export: { Name: 'BaobotSharedMemoryBucketName' },
    });
  });

  test('exports ECS security group ID', () => {
    template.hasOutput('EcsSecurityGroupId', {
      Export: { Name: 'BaobotEcsSecurityGroupId' },
    });
  });
});
