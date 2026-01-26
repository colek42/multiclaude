#!/bin/bash
set -euxo pipefail

# Log everything
exec > >(tee /var/log/user-data.log) 2>&1

echo "=== Starting multiclaude EC2 bootstrap ==="

# System deps
dnf install -y tmux git jq gcc make

# Go 1.25
GO_VERSION="1.25.1"
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tar.gz
rm /tmp/go.tar.gz

# Node.js 20 via NodeSource
curl -fsSL https://rpm.nodesource.com/setup_20.x | bash -
dnf install -y nodejs

# GitHub CLI
dnf install -y 'dnf-command(config-manager)'
dnf config-manager --add-repo https://cli.github.com/packages/rpm/gh-cli.repo
dnf install -y gh

# Tailscale
curl -fsSL https://tailscale.com/install.sh | sh

# Create dev user
useradd -m -s /bin/bash dev
usermod -aG wheel dev

# Set up dev user environment
cat >> /home/dev/.bashrc << 'EOF'
export PATH="/usr/local/go/bin:$HOME/go/bin:$HOME/.local/bin:$PATH"
export GOPATH="$HOME/go"

# Claude environment
export CLAUDE_CONFIG_DIR="$HOME/.claude"
EOF

# Create directories
mkdir -p /home/dev/.claude
mkdir -p /home/dev/.config/systemd/user
mkdir -p /home/dev/.local/bin
mkdir -p /home/dev/go/bin

# fetch-secrets.sh - pulls secrets from Secrets Manager
cat > /home/dev/fetch-secrets.sh << 'EOF'
#!/bin/bash
set -euo pipefail

REGION="us-east-1"

echo "Fetching secrets from AWS Secrets Manager..."

# Claude credentials
echo "Fetching Claude credentials..."
aws secretsmanager get-secret-value \
    --secret-id multiclaude/claude-credentials \
    --query SecretString \
    --output text \
    --region "$REGION" > ~/.claude/.credentials.json
chmod 600 ~/.claude/.credentials.json

# GitHub token
echo "Fetching GitHub token..."
GH_TOKEN=$(aws secretsmanager get-secret-value \
    --secret-id multiclaude/github-token \
    --query SecretString \
    --output text \
    --region "$REGION" | jq -r '.token')
echo "$GH_TOKEN" | gh auth login --with-token
echo "GitHub auth status:"
gh auth status

echo "Secrets configured successfully!"
EOF

# deploy.sh - pulls repo, builds, restarts daemon
cat > /home/dev/deploy.sh << 'EOF'
#!/bin/bash
set -euo pipefail

REPO_URL="${MULTICLAUDE_REPO:-https://github.com/colek42/multiclaude.git}"
REPO_DIR="$HOME/multiclaude"

echo "=== Deploying multiclaude ==="

# Clone or update repo
if [ -d "$REPO_DIR" ]; then
    echo "Updating existing repo..."
    cd "$REPO_DIR"
    git fetch origin
    git reset --hard origin/main
else
    echo "Cloning repo..."
    git clone "$REPO_URL" "$REPO_DIR"
    cd "$REPO_DIR"
fi

# Build
echo "Building multiclaude..."
/usr/local/go/bin/go build -o ~/go/bin/multiclaude ./cmd/multiclaude

# Restart daemon
echo "Restarting daemon..."
systemctl --user stop multiclaude-daemon || true
sleep 1
systemctl --user start multiclaude-daemon

echo "=== Deploy complete ==="
echo "Version: $(~/go/bin/multiclaude version 2>/dev/null || echo 'unknown')"
EOF

# systemd user service for multiclaude daemon
cat > /home/dev/.config/systemd/user/multiclaude-daemon.service << 'EOF'
[Unit]
Description=Multiclaude Daemon
After=network.target

[Service]
Type=simple
ExecStart=%h/go/bin/multiclaude daemon start --foreground
Restart=always
RestartSec=5
Environment=PATH=/usr/local/go/bin:%h/go/bin:%h/.local/bin:/usr/bin:/bin
Environment=HOME=%h
Environment=GOPATH=%h/go
Environment=CLAUDE_CONFIG_DIR=%h/.claude

[Install]
WantedBy=default.target
EOF

# Fix permissions
chmod +x /home/dev/fetch-secrets.sh
chmod +x /home/dev/deploy.sh
chown -R dev:dev /home/dev

# Enable lingering for dev user (allows user services to run without login)
loginctl enable-linger dev

# Start Tailscale and join tailnet
echo "=== Configuring Tailscale ==="

# Get Tailscale auth key from Secrets Manager
TAILSCALE_KEY=$(aws secretsmanager get-secret-value \
    --secret-id multiclaude/tailscale-auth-key \
    --query SecretString \
    --output text \
    --region us-east-1 | jq -r '.key')

if [ -n "$TAILSCALE_KEY" ] && [ "$TAILSCALE_KEY" != "null" ]; then
    systemctl enable --now tailscaled
    tailscale up --authkey="$TAILSCALE_KEY" --hostname=multiclaude-dev --ssh
    echo "Tailscale configured successfully!"
else
    echo "WARNING: Tailscale auth key not found or empty. Set it in Secrets Manager and run: tailscale up --authkey=<key> --hostname=multiclaude-dev --ssh"
    systemctl enable --now tailscaled
fi

# Install Claude CLI globally
echo "=== Installing Claude CLI ==="
npm install -g @anthropic-ai/claude-code

# Initial clone and build (as dev user)
echo "=== Initial build ==="
sudo -u dev bash -c 'source ~/.bashrc && /home/dev/deploy.sh' || echo "Initial build skipped (secrets may not be configured yet)"

echo "=== Bootstrap complete ==="
echo ""
echo "Next steps:"
echo "1. Upload secrets to Secrets Manager"
echo "2. SSH via Tailscale: ssh dev@multiclaude-dev"
echo "3. Run: ./fetch-secrets.sh"
echo "4. Enable daemon: systemctl --user enable --now multiclaude-daemon"
