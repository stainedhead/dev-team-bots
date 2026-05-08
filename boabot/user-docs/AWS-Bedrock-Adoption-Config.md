# AWS Bedrock — Authentication Configuration

boabot's Bedrock provider uses the AWS SDK v2 default credential chain. No code changes are needed between environments — authentication is determined entirely by which environment variables and files are present at startup.

## Provider config (same for all environments)

```yaml
models:
  default: claude-bedrock
  providers:
    - name: claude-bedrock
      type: bedrock
      model_id: us.anthropic.claude-3-7-sonnet-20250219-v1:0
```

The `model_id` is the Bedrock cross-region inference profile ID. Adjust the `us.` prefix for your target region family (`eu.`, `ap.`).

---

## Environment 1 — Developer workstation (AWS SSO)

Use SSO-backed temporary credentials managed by the AWS CLI. No secrets are stored on disk.

**One-time setup:**

```bash
aws configure sso --profile boabot-dev
# Follow the prompts: SSO start URL, region, account, role
```

**Each session (after token expiry):**

```bash
aws sso login --profile boabot-dev
```

**Runtime environment:**

```bash
export AWS_PROFILE=boabot-dev
export AWS_DEFAULT_REGION=us-east-1
./bin/boabot
```

The AWS CLI caches the temporary credentials under `~/.aws/sso/cache/`. The SDK refreshes them automatically on expiry within the same session; `aws sso login` is only needed when the cached token itself expires (typically after 8–12 hours depending on your IAM Identity Center session duration).

---

## Environment 2 — Service account (external to AWS)

For servers, CI/CD pipelines, or containers running outside AWS infrastructure.

### Option A — Static IAM credentials

Suitable for simple setups or legacy infrastructure. Prefer Option B where possible.

```bash
export AWS_ACCESS_KEY_ID=AKIA...
export AWS_SECRET_ACCESS_KEY=...
export AWS_DEFAULT_REGION=us-east-1
# AWS_SESSION_TOKEN is required only when using temporary/assumed-role credentials
```

Create a dedicated IAM user with the minimum policy:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "bedrock:InvokeModel",
        "bedrock:InvokeModelWithResponseStream"
      ],
      "Resource": "arn:aws:bedrock:us-east-1::foundation-model/*"
    }
  ]
}
```

### Option B — Web Identity / OIDC federation

Preferred for Kubernetes workloads, GitHub Actions, and any platform that issues OIDC tokens. No long-lived credentials are stored.

```bash
export AWS_ROLE_ARN=arn:aws:iam::123456789012:role/boabot-bedrock-role
export AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/token
export AWS_DEFAULT_REGION=us-east-1
```

The SDK exchanges the OIDC token for short-lived STS credentials and refreshes them automatically. Configure the IAM role's trust policy to accept tokens from your identity provider (GitHub OIDC, your Kubernetes OIDC issuer, etc.).

**Example trust policy for GitHub Actions:**

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"},
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:your-org/your-repo:ref:refs/heads/main"
        }
      }
    }
  ]
}
```

---

## Environment 3 — Machine identity (running inside AWS)

Zero configuration. The SDK detects the execution environment and obtains credentials automatically. No environment variables are required.

| Compute | Credential source | How it works |
|---|---|---|
| **EC2** | Instance IAM role | SDK queries IMDSv2 at `169.254.169.254` |
| **ECS** | Task IAM role | SDK reads `AWS_CONTAINER_CREDENTIALS_RELATIVE_URI` (injected by ECS) |
| **EKS** | IRSA (IAM Roles for Service Accounts) | `AWS_ROLE_ARN` + `AWS_WEB_IDENTITY_TOKEN_FILE` injected by pod admission webhook |
| **Lambda** | Execution role | SDK reads from the Lambda runtime environment |

Attach the Bedrock invocation policy (from Option A above) directly to the EC2 instance profile, ECS task role, or EKS service account IAM role.

**Recommended ECS task definition snippet:**

```json
{
  "taskRoleArn": "arn:aws:iam::123456789012:role/boabot-task-role",
  "environment": [
    {"name": "AWS_DEFAULT_REGION", "value": "us-east-1"}
  ]
}
```

No `AWS_ACCESS_KEY_ID` or `AWS_SECRET_ACCESS_KEY` are needed — the task role credentials are injected by ECS at runtime.

---

## Credential chain priority

When boabot starts, the SDK resolves credentials in this order:

1. `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` environment variables
2. `AWS_PROFILE` → shared credentials file (`~/.aws/credentials`) or SSO session
3. `AWS_ROLE_ARN` + `AWS_WEB_IDENTITY_TOKEN_FILE` → web identity token exchange
4. ECS container credentials (`AWS_CONTAINER_CREDENTIALS_RELATIVE_URI`)
5. EC2 instance metadata (IMDSv2)

The first source that returns valid credentials wins. In practice:
- Developer machines → SSO via `AWS_PROFILE` (source 2)
- External service accounts → static keys (source 1) or OIDC (source 3)
- AWS-hosted workloads → instance/task/pod role (sources 4–5)

## Region resolution

The Bedrock region is resolved from:

1. `AWS_DEFAULT_REGION` or `AWS_REGION` environment variable
2. `region` in `~/.aws/config` for the active profile

At least one must be set. For AWS-hosted workloads without an env var, set the region in the ECS task definition, Kubernetes pod spec, or EC2 launch template user data.
