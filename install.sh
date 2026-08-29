#!/usr/bin/env bash
# Hiroto — full auto-install script
# Usage: curl -fsSL https://raw.githubusercontent.com/hirotomasato/hiroto/main/install.sh | bash
set -euo pipefail

REPO="github.com/hirotomasato/hiroto"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.hiroto"
SKILLS_DIR="$CONFIG_DIR/skills"
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}◆ Hiroto — personal AI agent installer${NC}"
echo ""

# ---- 1. Prerequisites ----
echo -e "${GREEN}[1/5]${NC} Checking prerequisites..."

if ! command -v go &>/dev/null; then
    echo "Go is required. Install from https://go.dev/dl/"
    exit 1
fi
echo "  Go: $(go version)"

if ! command -v node &>/dev/null; then
    echo "  Node.js not found — execute_code (JS) will be unavailable"
else
    echo "  Node: $(node --version)"
fi

if ! command -v python3 &>/dev/null; then
    echo "  Python3 not found — execute_python will be unavailable"
else
    echo "  Python3: $(python3 --version)"
fi

# ---- 2. Install Hiroto ----
echo ""
echo -e "${GREEN}[2/5]${NC} Installing Hiroto..."

go install "${REPO}/cmd/hiroto@latest" 2>/dev/null || {
    echo "  go install failed — cloning and building locally..."
    TMPDIR=$(mktemp -d)
    git clone "https://${REPO}.git" "$TMPDIR" 2>/dev/null
    cd "$TMPDIR"
    go build -o "$INSTALL_DIR/hiroto" ./cmd/hiroto
    cd - >/dev/null
    rm -rf "$TMPDIR"
}

if ! command -v hiroto &>/dev/null; then
    echo "  ERROR: hiroto not found in PATH after install"
    exit 1
fi
echo "  Hiroto: $(hiroto --banner 2>/dev/null | grep 'v[0-9]' | head -1)"

# ---- 3. Setup config ----
echo ""
echo -e "${GREEN}[3/5]${NC} Setting up ~/.hiroto/..."

mkdir -p "$CONFIG_DIR" "$SKILLS_DIR" "$CONFIG_DIR/sessions" "$CONFIG_DIR/memory"

if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    cat > "$CONFIG_DIR/config.yaml" << 'YEOF'
model:
  base_url: http://localhost:20128/v1
  model: your-model-name
  api_key: ${HIROTO_API_KEY}

agent:
  max_turns: 40
  terminal_timeout: 180
  compression_budget_tokens: 25000
  compression_keep_turns: 6

skills:
  dirs: []

plugins:
  dirs:
    - ~/.hiroto/plugins
YEOF
    echo "  Created config.yaml — edit your model and API key!"
fi

# ---- 4. Install external security tools ----
echo ""
echo -e "${GREEN}[4/5]${NC} Installing security tools (background)..."
echo "  This may take a few minutes..."

install_tool() {
    local name="$1"
    local pkg="$2"
    if command -v "$name" &>/dev/null; then
        echo "  $name: already installed"
        return
    fi
    echo "  $name: installing..."
    go install "$pkg"@latest 2>/dev/null && echo "  $name: done" || echo "  $name: SKIPPED"
}

install_tool httpx     github.com/projectdiscovery/httpx/cmd/httpx &
install_tool subfinder github.com/projectdiscovery/subfinder/v2/cmd/subfinder &
install_tool katana    github.com/projectdiscovery/katana/cmd/katana &
install_tool nuclei    github.com/projectdiscovery/nuclei/v3/cmd/nuclei &
install_tool dnsx      github.com/projectdiscovery/dnsx/cmd/dnsx &
install_tool gau        github.com/lc/gau/v2/cmd/gau &
install_tool ffuf       github.com/ffuf/ffuf/v2 &
install_tool waybackurls github.com/tomnomnom/waybackurls &
install_tool hakrawler  github.com/hakluke/hakrawler &
install_tool gobuster   github.com/OJ/gobuster/v3 &

wait
echo "  Security tools: done"

# Ensure ~/go/bin is in PATH
if ! echo "$PATH" | grep -q "$HOME/go/bin"; then
    echo ""
    echo "  ⚠ Add this to your ~/.bashrc or ~/.zshrc:"
    echo "    export PATH=\"\$HOME/go/bin:\$PATH\""
fi

# ---- 5. Done ----
echo ""
echo -e "${GREEN}[5/5]${NC} Done!"
echo ""
echo "  ┌─────────────────────────────────────────────┐"
echo "  │  Hiroto installed!                          │"
echo "  │                                             │"
echo "  │  Run: hiroto                                │"
echo "  │  One-shot: hiroto -q \"your question\"        │"
echo "  │  Gateway: hiroto gateway                    │"
echo "  │  Update:  hiroto --update                   │"
echo "  │                                             │"
echo "  │  Config: ~/.hiroto/config.yaml              │"
echo "  │  Skills: ~/.hiroto/skills/                  │"
echo "  │  Tools:  ~/go/bin/ (10+ security tools)     │"
echo "  └─────────────────────────────────────────────┘"
echo ""