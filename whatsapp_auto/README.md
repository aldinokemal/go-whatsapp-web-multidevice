# AI-Powered WhatsApp Bot Service

An intelligent WhatsApp bot that uses Ollama (local AI models) to provide context-aware responses to messages. This service integrates with your existing Go WhatsApp MCP server to provide AI-powered conversations.

## Features

- 🤖 **Local AI Integration**: Uses Ollama for local AI model inference
- 💬 **Context-Aware Conversations**: Maintains conversation history and context
- 👥 **Group Chat Support**: Handles group conversations intelligently
- 🔄 **MCP Integration**: Seamlessly integrates with your existing Go MCP server
- 🚀 **FastAPI Backend**: Modern Python web service with async support
- 🐳 **Docker Ready**: Complete containerization setup

## Architecture

```
┌─────────────────┐    HTTP/gRPC    ┌──────────────────┐
│   Go MCP Server │ ◄──────────────► │  Python AI Service │
│  (WhatsApp)     │                 │   (Ollama)        │
└─────────────────┘                 └──────────────────┘
                                              │
                                              ▼
                                    ┌──────────────────┐
                                    │   Ollama Models  │
                                    │  (llama3.2, etc.)  │
                                    └──────────────────┘
```

## Prerequisites

- Docker and Docker Compose
- Python 3.11+ (for local development)
- Ollama installed locally (for testing)

## Quick Start

### 1. Clone and Setup

```bash
cd whatsapp_auto
pip install -r requirements.txt
```

### 2. Start Ollama Service

```bash
# Start Ollama locally
ollama serve

# Pull a model (in another terminal)
ollama pull llama2
```

### 3. Start AI Service

```bash
# Set environment variables
export OLLAMA_URL=http://localhost:11434
export OLLAMA_MODEL=llama2

# Start the service
python main.py
```

The service will be available at `http://localhost:8000`

### 4. Test the Service

```bash
# Health check
curl http://localhost:8000/health

# Process a test message
curl -X POST http://localhost:8000/api/process-message \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "id": "test123",
      "text": "Hello, how are you?",
      "sender_id": "1234567890",
      "chat_id": "chat123",
      "timestamp": "2024-01-01T00:00:00Z",
      "is_group": false
    },
    "conversation": {
      "chat_id": "chat123",
      "is_group": false,
      "last_updated": "2024-01-01T00:00:00Z"
    }
  }'
```

## Docker Deployment

### 1. Build and Run

```bash
# Build the AI service
docker-compose build

# Start all services
docker-compose up -d
```

### 2. Check Services

```bash
# Check service status
docker-compose ps

# View logs
docker-compose logs -f ai-whatsapp-bot
```

## Integration with Go MCP Server

### 1. Add AI Bridge to Your Go MCP Server

```go
// In your mcp.go file
import (
    "github.com/your-project/whatsapp_auto/integration"
)

func mcpServer(_ *cobra.Command, _ []string) {
    // ... existing code ...

    // Initialize AI bridge
    aiBridge := integration.NewAIBridge("http://ai-whatsapp-bot:8000")

    // Add AI tools to MCP server
    aiBridge.AddAITools(mcpServer)

    // ... rest of your code ...
}
```

### 2. Use AI Tools in MCP

Your MCP server now has these AI tools available:

- `ai_process_message`: Process messages with AI
- `whatsapp_send_text`: Send AI-generated responses

## Configuration

### Environment Variables

| Variable              | Default                  | Description               |
| --------------------- | ------------------------ | ------------------------- |
| `OLLAMA_URL`          | `http://localhost:11434` | Ollama service URL        |
| `OLLAMA_MODEL`        | `llama2`                 | AI model to use           |
| `AI_TEMPERATURE`      | `0.7`                    | AI creativity (0.0-2.0)   |
| `AI_MAX_TOKENS`       | `500`                    | Maximum response length   |
| `AI_PERSONALITY`      | `helpful`                | AI personality type       |
| `AI_ENABLE_QUESTIONS` | `true`                   | Allow AI to ask questions |
| `PORT`                | `8000`                   | Service port              |
| `HOST`                | `0.0.0.0`                | Service host              |

### AI Models

Supported Ollama models:

- `llama2` - Meta's Llama 2 (recommended)
- `mistral` - Mistral AI's model
- `codellama` - Code-focused model
- `llama2-uncensored` - Uncensored version

## API Endpoints

### Core Endpoints

- `GET /` - Service info
- `GET /health` - Health check
- `POST /api/process-message` - Process message with AI

### Conversation Management

- `GET /api/conversations` - List all conversations
- `GET /api/conversations/{chat_id}` - Get specific conversation
- `DELETE /api/conversations/{chat_id}` - Delete conversation
- `POST /api/conversations/{chat_id}/clear` - Clear context

### AI Management

- `GET /api/models` - List available Ollama models

## Usage Examples

### 1. Basic Message Processing

```python
import requests

# Process a message
response = requests.post("http://localhost:8000/api/process-message", json={
    "message": {
        "id": "msg123",
        "text": "What's the weather like?",
        "sender_id": "user123",
        "chat_id": "chat456",
        "timestamp": "2024-01-01T12:00:00Z",
        "is_group": False
    },
    "conversation": {
        "chat_id": "chat456",
        "is_group": False,
        "last_updated": "2024-01-01T12:00:00Z"
    }
})

ai_response = response.json()
print(f"AI Response: {ai_response['ai_response']['text']}")
print(f"Should Reply: {ai_response['ai_response']['should_reply']}")
```

### 2. Group Chat Handling

```python
# Group message processing
response = requests.post("http://localhost:8000/api/process-message", json={
    "message": {
        "id": "msg456",
        "text": "@ai help me with this project",
        "sender_id": "user789",
        "chat_id": "group123",
        "timestamp": "2024-01-01T12:00:00Z",
        "is_group": True,
        "group_name": "Project Team"
    },
    "conversation": {
        "chat_id": "group123",
        "is_group": True,
        "group_name": "Project Team",
        "last_updated": "2024-01-01T12:00:00Z"
    }
})
```

## Development

### Project Structure

```
whatsapp_auto/
├── ai_service/           # Core AI service modules
│   ├── models.py        # Data models
│   ├── ollama_client.py # Ollama integration
│   └── ai_service.py    # Main AI logic
├── integration/          # Go MCP integration
│   └── mcp_bridge.go    # MCP bridge
├── main.py              # FastAPI application
├── requirements.txt      # Python dependencies
├── Dockerfile           # Container configuration
├── docker-compose.yml   # Service orchestration
└── README.md            # This file
```

### Local Development

```bash
# Install dependencies
pip install -r requirements.txt

# Run with auto-reload
uvicorn main:app --reload --host 0.0.0.0 --port 8000

# Run tests
pytest

# Format code
black .
isort .
```

### Testing

```bash
# Run the test suite
pytest

# Run with coverage
pytest --cov=ai_service

# Run specific tests
pytest tests/test_ai_service.py -v
```

## Troubleshooting

### Common Issues

1. **Ollama Connection Failed**

   - Check if Ollama is running: `ollama list`
   - Verify URL in environment variables
   - Check firewall settings

2. **Model Not Found**

   - Pull the model: `ollama pull llama2`
   - Check available models: `ollama list`

3. **Service Won't Start**
   - Check logs: `docker-compose logs ai-whatsapp-bot`
   - Verify port availability
   - Check environment variables

### Logs

```bash
# View service logs
docker-compose logs -f ai-whatsapp-bot

# View Ollama logs
docker-compose logs -f ollama

# Check service health
curl http://localhost:8000/health
```

## Performance Tuning

### AI Model Optimization

- Use smaller models for faster responses
- Adjust `temperature` for creativity vs consistency
- Tune `max_tokens` for response length

### Memory Management

- Monitor conversation context size
- Implement conversation cleanup
- Use streaming responses for long conversations

## Security Considerations

- Restrict CORS origins in production
- Implement authentication for API endpoints
- Validate all input messages
- Monitor AI responses for inappropriate content

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the same license as your main WhatsApp project.

## Support

For issues and questions:

1. Check the troubleshooting section
2. Review the logs
3. Open an issue in the repository
4. Check Ollama documentation for model-specific issues

## Roadmap

- [ ] Database persistence for conversations
- [ ] User preference management
- [ ] Advanced context analysis
- [ ] Multi-language support
- [ ] Response templates
- [ ] Analytics dashboard
- [ ] Webhook integration
- [ ] Rate limiting
- [ ] A/B testing for responses
