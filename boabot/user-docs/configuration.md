# Configuration Reference — boabot

The agent reads `config.yaml` from the same directory as the binary by default. Override with `--config <path>`.

Use `config.example.yaml` (in this directory) as a starting point. Never commit a real config file.

## Required Fields

```yaml
bot:
  name: <string>      # unique name for this bot instance
  type: <string>      # bot type — must match a directory in boabot-team/bots/

aws:
  region: <string>           # e.g. us-east-1
  sqs_queue_url: <string>    # injected by CDK at deploy time
  sns_topic_arn: <string>    # injected by CDK at deploy time
  private_bucket: <string>   # injected by CDK at deploy time
  team_bucket: <string>      # injected by CDK at deploy time

models:
  default: <provider-name>   # name of the default provider
  providers:
    - name: <string>
      type: bedrock           # or: openai
      model_id: <string>
      region: <string>        # bedrock only
      endpoint: <url>         # openai only
```

## Orchestrator Mode

```yaml
orchestrator:
  enabled: true     # false by default
  api_port: 8080    # REST API port
  web_port: 8081    # web UI port
```

## Secrets

API keys and database credentials are not in the config file. They are loaded from AWS Secrets Manager at startup using the bot's IAM role. The config file is safe to inspect — it contains no secrets.
