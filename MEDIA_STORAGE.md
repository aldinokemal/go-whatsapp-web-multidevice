# Media Storage Configuration

This application now supports modular media storage with both **local file system** and **S3/MinIO** backends.

## Features

- **Modular Architecture**: Easy to switch between storage providers
- **Local Storage**: Traditional file system storage (default)
- **S3/MinIO Storage**: Cloud object storage for scalability
- **Transparent API**: Same interface for all storage backends
- **Automatic Initialization**: Storage is configured at startup

## Configuration

### Environment Variables

Add the following variables to your `.env` file:

```env
# Media Storage Configuration
# Storage type: "local" for local file system, "s3" for S3/MinIO storage
MEDIA_STORAGE_TYPE=local

# S3/MinIO Configuration (only used when MEDIA_STORAGE_TYPE=s3)
S3_ENDPOINT=https://s3.amazonaws.com
S3_REGION=us-east-1
S3_ACCESS_KEY_ID=your-access-key-id
S3_SECRET_ACCESS_KEY=your-secret-access-key
S3_BUCKET=whatsapp-media
S3_FORCE_PATH_STYLE=false
S3_PUBLIC_URL=
```

### Local Storage (Default)

To use local file system storage:

```env
MEDIA_STORAGE_TYPE=local
```

Media files will be stored in the `statics/media/` directory.

### S3/MinIO Storage

To use S3 or MinIO storage:

```env
MEDIA_STORAGE_TYPE=s3
S3_ENDPOINT=https://s3a.larahq.com
S3_REGION=us-east-1
S3_ACCESS_KEY_ID=your-access-key
S3_SECRET_ACCESS_KEY=your-secret-key
S3_BUCKET=whatsapp-media
S3_FORCE_PATH_STYLE=true
S3_PUBLIC_URL=https://s3a.larahq.com
```

#### S3 Configuration Options

- **S3_ENDPOINT**: The S3-compatible endpoint URL
  - For AWS S3: Use `https://s3.amazonaws.com` (or region-specific endpoint)
  - For MinIO: Use your MinIO server URL (e.g., `https://minio.example.com`)

- **S3_REGION**: AWS region (e.g., `us-east-1`, `eu-west-1`)

- **S3_ACCESS_KEY_ID**: Your S3/MinIO access key

- **S3_SECRET_ACCESS_KEY**: Your S3/MinIO secret key

- **S3_BUCKET**: Name of the bucket to store media files

- **S3_FORCE_PATH_STYLE**:
  - Set to `true` for MinIO or path-style URLs
  - Set to `false` for AWS S3 virtual-hosted-style URLs

- **S3_PUBLIC_URL**: (Optional) Custom public URL for accessing files
  - Leave empty to use default S3 URLs
  - Set to your CDN or custom domain if configured

## MinIO Example

For a MinIO setup running on `https://s3a.larahq.com`:

```env
MEDIA_STORAGE_TYPE=s3
S3_ENDPOINT=https://s3a.larahq.com
S3_REGION=us-east-1
S3_ACCESS_KEY_ID=your-access-key
S3_SECRET_ACCESS_KEY=your-secret-key
S3_BUCKET=id1
S3_FORCE_PATH_STYLE=true
S3_PUBLIC_URL=https://s3a.larahq.com
```

**Important**: The bucket must be created manually before starting the application. The application will not automatically create buckets.

## AWS S3 Example

For AWS S3:

```env
MEDIA_STORAGE_TYPE=s3
S3_ENDPOINT=https://s3.amazonaws.com
S3_REGION=us-east-1
S3_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
S3_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
S3_BUCKET=my-whatsapp-media
S3_FORCE_PATH_STYLE=false
S3_PUBLIC_URL=
```

**Important**: The bucket must be created manually before starting the application. The application will not automatically create buckets.

## How It Works

### Architecture

The storage system uses a modular interface-based design:

1. **Storage Interface** (`pkg/storage/storage.go`): Defines common operations
   - `Save(ctx, data, filename)`: Save media to storage
   - `Get(ctx, path)`: Retrieve media from storage
   - `Delete(ctx, path)`: Remove media from storage
   - `GetURL(path)`: Get publicly accessible URL
   - `SaveStream(ctx, reader, filename)`: Save from stream

2. **Local Storage** (`pkg/storage/local.go`): File system implementation

3. **MinIO Storage** (`pkg/storage/s3_minio.go`): S3-compatible storage using MinIO native SDK (minio-go/v7)
   - Works with MinIO, AWS S3, and all S3-compatible storage providers
   - Full S3 API compatibility with better performance
   - No signature calculation issues

4. **Factory** (`pkg/storage/factory.go`): Creates storage instances based on configuration

### Initialization

Storage is initialized automatically at application startup in `cmd/root.go`:

```go
// Initialize storage
storageType, err := storage.ParseStorageType(config.MediaStorageType)
if err != nil {
    logrus.Fatalf("invalid media storage type: %v", err)
}

var s3Config *storage.S3Config
if storageType == storage.StorageTypeS3 {
    s3Config = &storage.S3Config{
        Endpoint:        config.S3Endpoint,
        Region:          config.S3Region,
        AccessKeyID:     config.S3AccessKeyID,
        SecretAccessKey: config.S3SecretAccessKey,
        Bucket:          config.S3Bucket,
        ForcePathStyle:  config.S3ForcePathStyle,
        PublicURL:       config.S3PublicURL,
    }
}

if err := storage.InitStorage(storageType, config.PathMedia, s3Config); err != nil {
    logrus.Fatalf("failed to initialize media storage: %v", err)
}
```

### Usage in Code

The storage is accessed via the global storage instance:

```go
import "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/storage"

// Get storage instance
mediaStorage := storage.GetStorage()

// Save media
path, err := mediaStorage.Save(ctx, data, filename)
if err != nil {
    return err
}

// Get public URL
url := mediaStorage.GetURL(path)
```

## Migration Guide

### Switching from Local to S3

1. Set up your S3/MinIO bucket
2. Update `.env` with S3 configuration
3. Restart the application
4. New media will be stored in S3
5. Optionally migrate existing media files manually

### Switching from S3 to Local

1. Update `.env` to use `MEDIA_STORAGE_TYPE=local`
2. Restart the application
3. New media will be stored locally
4. Optionally download existing S3 media files

## Troubleshooting

### S3 Connection Issues

If you encounter connection issues:

1. Verify endpoint URL is correct
2. Check access key and secret key
3. Ensure bucket exists or bucket creation permissions are available
4. For MinIO, ensure `S3_FORCE_PATH_STYLE=true`
5. Check network connectivity to S3/MinIO endpoint

### Permission Issues

Ensure S3/MinIO user has these permissions:
- `s3:PutObject` - Upload files
- `s3:GetObject` - Download files
- `s3:DeleteObject` - Delete files
- `s3:ListBucket` - List bucket contents (optional)
- `s3:HeadBucket` - Check bucket exists

### Bucket Not Found

**The application expects the bucket to already exist** and will not attempt to create it automatically. If you get a bucket not found error:

1. Create the bucket manually in S3/MinIO console before starting the application
2. Ensure your credentials have these permissions:
   - `s3:PutObject` - Upload files
   - `s3:GetObject` - Download files
   - `s3:DeleteObject` - Delete files
   - `s3:ListBucket` - List bucket contents (optional)
   - `s3:HeadBucket` - Check bucket exists
3. Verify bucket naming rules are followed (lowercase, no underscores, no spaces)

## Performance Considerations

### Local Storage
- **Pros**: Fast access, no external dependencies, no network latency
- **Cons**: Limited scalability, single point of failure, no CDN support

### S3/MinIO Storage
- **Pros**: Highly scalable, redundant, CDN integration possible, distributed access
- **Cons**: Network latency, external dependency, costs for cloud S3

## Security Considerations

### Local Storage
- Ensure proper file permissions (default: 0644 for files, 0755 for directories)
- Protect the `statics/media/` directory from unauthorized access
- Use web server access controls

### Bucket Access Configuration

Choose between public or private bucket access based on your security requirements:

#### Option 1: Public Bucket (Simple & Fast)

**Best for**: Non-sensitive media, CDN setup, maximum performance

**Configuration**:
```env
S3_USE_SERVER_PROXY=false
```

**Setup**:
```bash
# Make bucket public
mc anonymous set download s3/bucket-name

# Verify
mc anonymous get s3/bucket-name
```

**How It Works**:
- Direct S3/MinIO URLs returned to clients
- No server-side processing
- Maximum performance
- URL format: `https://s3a.larahq.com/id1/{device-id}/{chat-jid}/{message-id}.{ext}`
- Example: `https://s3a.larahq.com/id1/123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif`

**Log Messages**:
```
INFO Initialized MinIO storage with endpoint: s3a.larahq.com, bucket: id1, region: us-east-1, SSL: true
INFO 🌐 Using direct public bucket URLs
```

**Debug Log Messages** (only shown when `APP_DEBUG=true`):
```
DEBUG 📤 Attempting to save media to MinIO: filename=123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif, size=85741 bytes
DEBUG ✅ Successfully saved media to MinIO: bucket=id1, key=123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif, etag=7cce285abed98e5c256f6f23a22510ce, size=85741
DEBUG 🔗 Public URL: https://s3a.larahq.com/id1/123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif
```

#### Option 2: Private Bucket (Secure)

**Best for**: Sensitive media, authentication required, access control needed

**Configuration**:
```env
S3_USE_SERVER_PROXY=true
```

**Setup**:
```bash
# Ensure bucket is private (default)
mc anonymous set none s3/bucket-name

# Verify
mc anonymous get s3/bucket-name
```

**How It Works**:
- Server acts as proxy between clients and S3
- Server uses credentials to fetch from private bucket
- Can integrate with your authentication system
- URL format: `https://your-server.com/media/download/{device-id}/{chat-jid}/{message-id}.{ext}`
- Example: `https://your-server.com/media/download/123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif`

**Log Messages**:
```
INFO Initialized MinIO storage with endpoint: s3a.larahq.com, bucket: id1, region: us-east-1, SSL: true
INFO 🔐 Using server proxy for private bucket access
```

**Debug Log Messages** (only shown when `APP_DEBUG=true`):
```
DEBUG 📤 Attempting to save media to MinIO: filename=123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif, size=85741 bytes
DEBUG ✅ Successfully saved media to MinIO: bucket=id1, key=123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif, etag=7cce285abed98e5c256f6f23a22510ce, size=85741
DEBUG 📥 Downloading media from storage: 123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif
DEBUG ✅ Successfully downloaded media: 123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif, size: 85741 bytes
```

**Security Benefits**:
- ✅ **Private Bucket** - No public access required
- ✅ **Server Authentication** - Control who can access media
- ✅ **No Credential Exposure** - Clients never see S3 credentials
- ✅ **Access Logging** - Track all media downloads

**Path Organization**:
- **Format**: `{device-id}/{chat-jid}/{message-id}.{extension}`
- **Example**: `123456789/6281234567890_s_whatsapp_net/3EB0ABC123.jfif`
- **Benefits**:
  - Easy to locate media by device, chat, or message
  - Better organization for multi-device deployments
  - Clear separation between different conversations
  - Message ID is globally unique - no need for timestamps
  - Clean, short filenames

### Additional Security
- Use IAM policies to restrict access
- Enable encryption at rest if required
- Use HTTPS endpoints only
- Rotate access keys regularly
- Enable bucket versioning for data recovery

## Dependencies

This feature uses the following dependency:

- `github.com/minio/minio-go/v7` - MinIO native Go client for S3-compatible storage

The MinIO SDK provides:
- ✅ Full compatibility with AWS S3, MinIO, and all S3-compatible storage
- ✅ Smaller binary size compared to AWS SDK
- ✅ Better performance and no signature calculation issues
- ✅ Same library used by the official MinIO client (`mc`)

This dependency is automatically installed via `go mod`.

## Troubleshooting

If you encounter S3/MinIO connection issues, see the comprehensive troubleshooting guide:

**[S3_TROUBLESHOOTING.md](S3_TROUBLESHOOTING.md)**

Common issues covered:
- SignatureDoesNotMatch errors
- Credential verification
- Bucket access problems
- Testing with MinIO Client (mc) and AWS CLI
- Clock skew issues

## Future Enhancements

Possible future improvements:

- [ ] Support for Google Cloud Storage (GCS)
- [ ] Support for Azure Blob Storage
- [ ] Automatic media migration between storage backends
- [ ] Pre-signed URL generation for temporary access
- [ ] Media compression before storage
- [ ] Automatic cleanup of old media files
- [ ] Storage usage monitoring and alerts
