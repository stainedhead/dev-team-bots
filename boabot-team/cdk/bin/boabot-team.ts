#!/usr/bin/env node
import * as cdk from 'aws-cdk-lib';
import * as path from 'path';
import * as fs from 'fs';
import * as yaml from 'js-yaml';
import { BaobotSharedStack } from '../lib/shared-stack';
import { BaobotBotStack } from '../lib/bot-stack';

// ---------------------------------------------------------------------------
// Read team.yaml from the parent directory (boabot-team/)
// ---------------------------------------------------------------------------
interface BotEntry {
  name: string;
  type: string;
  enabled: boolean;
  orchestrator?: boolean;
}

interface TeamYaml {
  team: BotEntry[];
}

const teamYamlPath = path.join(__dirname, '..', '..', 'team.yaml');
const teamConfig = yaml.load(
  fs.readFileSync(teamYamlPath, 'utf8'),
) as TeamYaml;

// ---------------------------------------------------------------------------
// CDK App
// ---------------------------------------------------------------------------
const app = new cdk.App();

const env: cdk.Environment = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region: process.env.CDK_DEFAULT_REGION ?? 'us-east-1',
};

// Stack 1 – shared infrastructure
const sharedStack = new BaobotSharedStack(app, 'BaobotSharedStack', {
  env,
  description: 'BaoBot shared infrastructure: VPC, RDS, DynamoDB, S3, SNS, ECS cluster, ECR',
});

// Stack 2 – one BotStack per enabled bot
for (const bot of teamConfig.team) {
  if (!bot.enabled) {
    continue;
  }

  const stackId = `BaobotBot-${bot.name.charAt(0).toUpperCase()}${bot.name.slice(1)}Stack`;

  new BaobotBotStack(app, stackId, {
    env,
    description: `BaoBot bot stack for ${bot.name} (${bot.type})`,
    botName: bot.name,
    botRole: bot.type,
    sharedStack,
  });
}

app.synth();
