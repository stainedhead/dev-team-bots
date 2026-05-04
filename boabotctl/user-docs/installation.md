# Installation — baobotctl

## Download

Download the latest binary for your platform from [GitHub Releases](../../releases).

### macOS (Apple Silicon)
```bash
curl -L https://github.com/<org>/dev-team-bots/releases/latest/download/baobotctl-darwin-arm64 -o baobotctl
chmod +x baobotctl && mv baobotctl /usr/local/bin/
```

### macOS (Intel)
```bash
curl -L https://github.com/<org>/dev-team-bots/releases/latest/download/baobotctl-darwin-amd64 -o baobotctl
chmod +x baobotctl && mv baobotctl /usr/local/bin/
```

### Linux (amd64)
```bash
curl -L https://github.com/<org>/dev-team-bots/releases/latest/download/baobotctl-linux-amd64 -o baobotctl
chmod +x baobotctl && mv baobotctl /usr/local/bin/
```

## Verify

```bash
baobotctl --version
```

## Configure

```bash
baobotctl config set endpoint https://<orchestrator-url>
baobotctl login
```

Your orchestrator URL is provided by your team Admin. See [`baobotctl.md`](baobotctl.md) for the full command reference.
