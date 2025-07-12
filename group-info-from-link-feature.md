# Group Info from Link Feature

This document describes the new feature that allows obtaining information about a WhatsApp group before joining it through a group invitation link.

## Overview

The new API endpoint `/group/info-from-link` provides information about a WhatsApp group without requiring the user to join the group. This feature addresses issue #332 by allowing users to preview group details before deciding to join.

## API Endpoint

### GET /group/info-from-link

Retrieves information about a WhatsApp group using its invitation link.

#### Request Parameters

| Parameter | Type   | Required | Description                    |
|-----------|--------|----------|--------------------------------|
| `link`    | string | Yes      | WhatsApp group invitation link |

#### Example Request

```bash
curl -X GET "http://localhost:3000/group/info-from-link?link=https://chat.whatsapp.com/ABC123DEF456" \
  -H "Authorization: Bearer YOUR_AUTH_TOKEN"
```

#### Response Format

```json
{
    "status": 200,
    "code": "SUCCESS",
    "message": "Success get group info from link",
    "results": {
        "group_id": "120363123456789012@g.us",
        "name": "Example Group",
        "topic": "This is the group description",
        "created_at": "2024-01-15T10:30:00Z",
        "participant_count": 25,
        "is_locked": false,
        "is_announce": false,
        "is_ephemeral": false,
        "description": "This is the group description"
    }
}
```

#### Response Fields

| Field              | Type      | Description                                    |
|--------------------|-----------|------------------------------------------------|
| `group_id`         | string    | Unique identifier of the group                 |
| `name`             | string    | Name of the group                              |
| `topic`            | string    | Group topic/description                        |
| `created_at`       | string    | ISO 8601 timestamp of group creation          |
| `participant_count`| integer   | Number of participants in the group            |
| `is_locked`        | boolean   | Whether group info can only be edited by admins |
| `is_announce`      | boolean   | Whether only admins can send messages          |
| `is_ephemeral`     | boolean   | Whether disappearing messages are enabled      |
| `description`      | string    | Group description (same as topic)              |

## Error Handling

The API will return appropriate error responses for various scenarios:

- **400 Bad Request**: Invalid or missing invitation link
- **401 Unauthorized**: Invalid authentication token
- **404 Not Found**: Group not found or link expired
- **410 Gone**: Invitation link has been revoked

## Usage Examples

### JavaScript/Node.js

```javascript
const axios = require('axios');

async function getGroupInfo(invitationLink) {
    try {
        const response = await axios.get('/group/info-from-link', {
            params: { link: invitationLink },
            headers: { Authorization: 'Bearer YOUR_AUTH_TOKEN' }
        });
        
        const groupInfo = response.data.results;
        console.log(`Group: ${groupInfo.name}`);
        console.log(`Members: ${groupInfo.participant_count}`);
        console.log(`Description: ${groupInfo.description}`);
        
        return groupInfo;
    } catch (error) {
        console.error('Error fetching group info:', error.response?.data || error.message);
        throw error;
    }
}
```

### Python

```python
import requests

def get_group_info(invitation_link, auth_token):
    url = "http://localhost:3000/group/info-from-link"
    params = {"link": invitation_link}
    headers = {"Authorization": f"Bearer {auth_token}"}
    
    try:
        response = requests.get(url, params=params, headers=headers)
        response.raise_for_status()
        
        group_info = response.json()["results"]
        print(f"Group: {group_info['name']}")
        print(f"Members: {group_info['participant_count']}")
        print(f"Description: {group_info['description']}")
        
        return group_info
    except requests.exceptions.RequestException as e:
        print(f"Error fetching group info: {e}")
        raise
```

### Go

```go
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
)

type GroupInfo struct {
    GroupID         string `json:"group_id"`
    Name            string `json:"name"`
    Topic           string `json:"topic"`
    CreatedAt       string `json:"created_at"`
    ParticipantCount int    `json:"participant_count"`
    IsLocked        bool   `json:"is_locked"`
    IsAnnounce      bool   `json:"is_announce"`
    IsEphemeral     bool   `json:"is_ephemeral"`
    Description     string `json:"description"`
}

func getGroupInfo(invitationLink, authToken string) (*GroupInfo, error) {
    baseURL := "http://localhost:3000/group/info-from-link"
    params := url.Values{"link": {invitationLink}}
    
    req, err := http.NewRequest("GET", baseURL+"?"+params.Encode(), nil)
    if err != nil {
        return nil, err
    }
    
    req.Header.Set("Authorization", "Bearer "+authToken)
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var result struct {
        Results GroupInfo `json:"results"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }
    
    return &result.Results, nil
}
```

## Security Considerations

1. **Authentication**: The endpoint requires proper authentication via Bearer token
2. **Rate Limiting**: Consider implementing rate limiting to prevent abuse
3. **Link Validation**: The API validates the invitation link format before processing
4. **Privacy**: No personal information about group members is exposed

## Technical Implementation

The feature is implemented using the following components:

1. **Domain Layer**: `GetGroupInfoFromLinkRequest` and `GetGroupInfoFromLinkResponse` structs
2. **Interface Layer**: `GetGroupInfoFromLink` method in `IGroupManagement` interface
3. **Use Case Layer**: Implementation using whatsmeow's `GetGroupInfoFromLink` method
4. **Validation Layer**: Input validation for the invitation link
5. **REST Layer**: HTTP endpoint handler with proper error handling

## Testing

To test the new feature:

1. Obtain a valid WhatsApp group invitation link
2. Make a GET request to `/group/info-from-link?link=<invitation_link>`
3. Verify the response contains the expected group information
4. Test with invalid links to ensure proper error handling

## Limitations

- The feature requires an active WhatsApp Web session
- Some group information may be limited based on group privacy settings
- The invitation link must be valid and not expired
- Rate limiting may apply to prevent excessive API calls

## Future Enhancements

Potential improvements for future versions:

1. Caching of group information to reduce API calls
2. Support for batch processing of multiple invitation links
3. Additional group metadata (creation date, admin information)
4. Integration with webhook notifications for group updates