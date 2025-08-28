#!/bin/bash

# AI WhatsApp Bot Service Startup Script

set -euo pipefail  # Enable strict mode: exit on error, undefined variables, and pipeline failures

echo "🤖 Starting AI WhatsApp Bot Service..."

# Check if Python is installed
if ! command -v python3 &>/dev/null; then
    echo "❌ Python 3.11+ is not installed."
    echo "   Please install Python 3.11 or higher: https://www.python.org/downloads/"
    exit 1
fi

PYTHON_VERSION=$(python3 --version | cut -d ' ' -f 2)
if [[ "${PYTHON_VERSION}" < "3.11" ]]; then
    echo "❌ Python version ${PYTHON_VERSION} is too old. Please install Python 3.11+."
    exit 1
fi
echo "✅ Python ${PYTHON_VERSION} is installed"

# Check if Ollama is running
echo "🔍 Checking Ollama service..."
OLLAMA_URL=${OLLAMA_URL:-"http://localhost:11434"}
if ! curl -s --fail --connect-timeout 5 "${OLLAMA_URL}/api/tags" &>/dev/null; then
    echo "⚠️  Ollama is not running at ${OLLAMA_URL}."
    echo "   Please start Ollama with: 'ollama serve'"
    echo "   Or install Ollama from: https://ollama.com/download"
    echo "   After installing, pull the model with: 'ollama pull llama3.1:8b'"
    echo ""
    read -p "Press Enter to continue anyway (not recommended) or Ctrl+C to abort..."
else
    echo "✅ Ollama is running at ${OLLAMA_URL}"
fi

# Check if required Python packages are installed
echo "📦 Checking Python dependencies..."
REQUIRED_PACKAGES=("fastapi" "uvicorn" "httpx" "pydantic" "pydantic-settings")
MISSING_PACKAGES=()
for pkg in "${REQUIRED_PACKAGES[@]}"; do
    if ! python3 -c "import ${pkg}" &>/dev/null; then
        MISSING_PACKAGES+=("${pkg}")
    fi
done

if [ ${#MISSING_PACKAGES[@]} -ne 0 ]; then
    echo "📥 Missing Python packages: ${MISSING_PACKAGES[*]}"
    echo "   Installing dependencies from requirements.txt..."
    if ! pip3 install --no-cache-dir -r requirements.txt; then
        echo "❌ Failed to install dependencies."
        exit 1
    fi
else
    echo "✅ All required Python dependencies are installed"
fi

# Set environment variables with defaults
export OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
export OLLAMA_MODEL="${OLLAMA_MODEL:-llama3.1:8b}"  # Updated to a more recent model
export AI_TEMPERATURE="${AI_TEMPERATURE:-0.7}"
export AI_MAX_TOKENS="${AI_MAX_TOKENS:-500}"
export AI_PERSONALITY="${AI_PERSONALITY:-helpful}"
export AI_ENABLE_QUESTIONS="${AI_ENABLE_QUESTIONS:-true}"
export APP_PORT="${APP_PORT:-8000}"  # Aligned with Settings class
export APP_HOST="${APP_HOST:-0.0.0.0}"
export API_KEY="${API_KEY:-}"  # Optional API key for security

# Validate environment variables
if [[ ! "${AI_TEMPERATURE}" =~ ^[0-9]*\.?[0-9]+$ || "${AI_TEMPERATURE}" < 0 || "${AI_TEMPERATURE}" > 2 ]]; then
    echo "❌ AI_TEMPERATURE must be a float between 0.0 and 2.0, got: ${AI_TEMPERATURE}"
    exit 1
fi
if [[ ! "${AI_MAX_TOKENS}" =~ ^[0-9]+$ || "${AI_MAX_TOKENS}" -le 0 ]]; then
    echo "❌ AI_MAX_TOKENS must be a positive integer, got: ${AI_MAX_TOKENS}"
    exit 1
fi
if [[ ! "${APP_PORT}" =~ ^[0-9]+$ || "${APP_PORT}" -le 0 ]]; then
    echo "❌ APP_PORT must be a positive integer, got: ${APP_PORT}"
    exit 1
fi

# Display configuration
echo "⚙️  Configuration:"
echo "   Ollama URL: ${OLLAMA_URL}"
echo "   Model: ${OLLAMA_MODEL}"
echo "   Temperature: ${AI_TEMPERATURE}"
echo "   Max Tokens: ${AI_MAX_TOKENS}"
echo "   Personality: ${AI_PERSONALITY}"
echo "   Questions Enabled: ${AI_ENABLE_QUESTIONS}"
echo "   Host: ${APP_HOST}"
echo "   Port: ${APP_PORT}"
if [[ -n "${API_KEY}" ]]; then
    echo "   API Key: [REDACTED]"
else
    echo "   API Key: Not set"
fi
echo ""

# Start the service with Uvicorn directly
echo "🚀 Starting AI service on http://${APP_HOST}:${APP_PORT}"
echo "📊 Health check: http://${APP_HOST}:${APP_PORT}/health"
echo "📚 API docs: http://${APP_HOST}:${APP_PORT}/docs"
echo ""
echo "Press Ctrl+C to stop the service"
echo ""

exec uvicorn main:app --host "${APP_HOST}" --port "${APP_PORT}" --log-level info