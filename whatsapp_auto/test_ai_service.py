#!/usr/bin/env python3
"""
Test script for AI WhatsApp Bot Service
"""

import asyncio
import json
import sys
from datetime import datetime, timezone
from typing import Dict, Any

import httpx


async def test_health_check(base_url: str) -> bool:
    """Test the health check endpoint."""
    try:
        async with httpx.AsyncClient() as client:
            response = await client.get(f"{base_url}/health")
            if response.status_code == 200:
                data = response.json()
                print(f"✅ Health check passed: {data['status']}")
                print(f"   Ollama healthy: {data['ollama_healthy']}")
                print(f"   Active conversations: {data['active_conversations']}")
                print(f"   Available models: {data['available_models']}")
                return True
            else:
                print(f"❌ Health check failed: {response.status_code}")
                return False
    except Exception as e:
        print(f"❌ Health check error: {e}")
        return False


async def test_message_processing(base_url: str) -> bool:
    """Test message processing with AI."""
    try:
        # Test message
        test_message = {
            "message": {
                "id": "test_msg_001",
                "text": "Hello! Can you help me with a question?",
                "sender_id": "1234567890",
                "chat_id": "test_chat_001",
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "is_group": False,
                "message_type": "text"
            },
            "conversation": {
                "chat_id": "test_chat_001",
                "is_group": False,
                "last_updated": datetime.now(timezone.utc).isoformat()
            }
        }

        async with httpx.AsyncClient() as client:
            response = await client.post(
                f"{base_url}/api/process-message",
                json=test_message,
                timeout=60.0  # Longer timeout for AI processing
            )

            if response.status_code == 200:
                data = response.json()
                if data.get("success"):
                    ai_response = data.get("ai_response", {})
                    print(f"✅ Message processing successful!")
                    print(f"   AI Response: {ai_response.get('text', 'N/A')[:100]}...")
                    print(f"   Should Reply: {ai_response.get('should_reply', False)}")
                    print(f"   Confidence: {ai_response.get('confidence', 0.0):.2f}")
                    if ai_response.get('questions'):
                        print(f"   Questions: {ai_response.get('questions')}")
                    return True
                else:
                    print(f"❌ Message processing failed: {data.get('error', 'Unknown error')}")
                    return False
            else:
                print(f"❌ Message processing HTTP error: {response.status_code}")
                print(f"   Response: {response.text}")
                return False

    except Exception as e:
        print(f"❌ Message processing error: {e}")
        return False


async def test_group_message(base_url: str) -> bool:
    """Test group message processing."""
    try:
        test_message = {
            "message": {
                "id": "test_group_001",
                "text": "@Grok help me with this project",
                "sender_id": "user123",
                "chat_id": "test_group_001",
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "is_group": True,
                "group_name": "Test Project Team",
                "message_type": "text"
            },
            "conversation": {
                "chat_id": "test_group_001",
                "is_group": True,
                "group_name": "Test Project Team",
                "last_updated": datetime.now(timezone.utc).isoformat()
            }
        }

        async with httpx.AsyncClient() as client:
            response = await client.post(
                f"{base_url}/api/process-message",
                json=test_message,
                timeout=60.0
            )

            if response.status_code == 200:
                data = response.json()
                if data.get("success"):
                    ai_response = data.get("ai_response", {})
                    print(f"✅ Group message processing successful!")
                    print(f"   AI Response: {ai_response.get('text', 'N/A')[:100]}...")
                    print(f"   Should Reply: {ai_response.get('should_reply', False)}")
                    return True
                else:
                    print(f"❌ Group message processing failed: {data.get('error', 'Unknown error')}")
                    return False
            else:
                print(f"❌ Group message HTTP error: {response.status_code}")
                return False

    except Exception as e:
        print(f"❌ Group message processing error: {e}")
        return False


async def test_conversation_management(base_url: str) -> bool:
    """Test conversation management endpoints."""
    try:
        async with httpx.AsyncClient() as client:
            # List conversations
            response = await client.get(f"{base_url}/api/conversations")
            if response.status_code == 200:
                conversations = response.json()
                print(f"✅ Conversations listed: {len(conversations)} active")
                
                # Test specific conversation retrieval
                if conversations:
                    chat_id = list(conversations.keys())[0]
                    conv_response = await client.get(f"{base_url}/api/conversations/{chat_id}")
                    if conv_response.status_code == 200:
                        print(f"✅ Conversation {chat_id} retrieved successfully")
                        return True
                    else:
                        print(f"❌ Failed to retrieve conversation {chat_id}")
                        return False
                else:
                    print("ℹ️  No conversations to test")
                    return True
            else:
                print(f"❌ Failed to list conversations: {response.status_code}")
                return False

    except Exception as e:
        print(f"❌ Conversation management error: {e}")
        return False


async def main():
    """Main test function."""
    base_url = "http://localhost:8000"
    
    print("🤖 Testing AI WhatsApp Bot Service")
    print("=" * 50)
    
    # Test 1: Health Check
    print("\n1. Testing Health Check...")
    if not await test_health_check(base_url):
        print("❌ Service is not healthy. Please check if the service is running.")
        sys.exit(1)
    
    # Test 2: Message Processing
    print("\n2. Testing Message Processing...")
    if not await test_message_processing(base_url):
        print("❌ Message processing failed.")
        sys.exit(1)
    
    # Test 3: Group Message Processing
    print("\n3. Testing Group Message Processing...")
    if not await test_group_message(base_url):
        print("❌ Group message processing failed.")
        sys.exit(1)
    
    # Test 4: Conversation Management
    print("\n4. Testing Conversation Management...")
    if not await test_conversation_management(base_url):
        print("❌ Conversation management failed.")
        sys.exit(1)
    
    print("\n" + "=" * 50)
    print("🎉 All tests passed! AI service is working correctly.")
    print("\nYou can now integrate this with your Go MCP server!")


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n\n⏹️  Testing interrupted by user")
        sys.exit(0)
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")
        sys.exit(1)
