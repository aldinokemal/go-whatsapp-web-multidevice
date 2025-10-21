# S3/MinIO Troubleshooting Guide

## Understanding the SignatureDoesNotMatch Error

The error you're seeing:
```
SignatureDoesNotMatch: The request signature we calculated does not match the signature you provided.
```

This typically indicates one of these issues:

### 1. **Incorrect Credentials** ⚠️ (Most Common)
The access key or secret key doesn't match what's configured in MinIO.

**How to verify:**
```bash
# Using MinIO Client (mc)
mc alias set myminio https://s3a.larahq.com hbinduni Jakarta123!
mc ls myminio/id1

# Or using AWS CLI with MinIO
aws --endpoint-url https://s3a.larahq.com \
    --region us-east-1 \
    s3 ls s3://id1/
```

**Fix:**
1. Log into your MinIO console at https://s3a.larahq.com
2. Navigate to Access Keys
3. Verify or regenerate your access key and secret
4. Update `src/.env` with the correct values

### 2. **Bucket Must Exist Before Starting**
**The application expects the bucket to already exist** and will not attempt to create it automatically.

**What happens now:**
- The app checks if the bucket exists using `HeadBucket`
- If the bucket exists and is accessible, the app continues normally ✅
- If the bucket doesn't exist, you'll get a clear error message
- You need to create the bucket manually before starting the application

### 3. **Clock Skew**
If your server's clock is off by more than 5 minutes, signatures will fail.

**How to check:**
```bash
date
# Compare with: https://time.is/
```

**Fix:**
```bash
# On Linux
sudo ntpdate -s time.nist.gov
# Or
sudo timedatectl set-ntp on
```

### 4. **Endpoint URL Format**
Make sure your endpoint doesn't include the bucket name.

**Correct:**
```env
S3_ENDPOINT=https://s3a.larahq.com
```

**Incorrect:**
```env
S3_ENDPOINT=https://s3a.larahq.com/id1
S3_ENDPOINT=https://id1.s3a.larahq.com
```

## Testing Your Configuration

### Method 1: Using MinIO Client (mc)

1. **Install MinIO Client:**
```bash
# Linux
wget https://dl.min.io/client/mc/release/linux-amd64/mc
chmod +x mc
sudo mv mc /usr/local/bin/

# macOS
brew install minio/stable/mc

# Windows
# Download from https://dl.min.io/client/mc/release/windows-amd64/mc.exe
```

2. **Configure and Test:**
```bash
# Add your MinIO server
mc alias set larahq https://s3a.larahq.com hbinduni Jakarta123!

# List buckets
mc ls larahq

# List files in id1 bucket
mc ls larahq/id1

# Upload test file
echo "test" > test.txt
mc cp test.txt larahq/id1/test.txt

# Download test file
mc cp larahq/id1/test.txt downloaded.txt

# Clean up
rm test.txt downloaded.txt
mc rm larahq/id1/test.txt
```

### Method 2: Using AWS CLI

1. **Install AWS CLI:**
```bash
# Linux/macOS
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install

# Or via package manager
# Ubuntu/Debian: sudo apt install awscli
# macOS: brew install awscli
```

2. **Configure credentials:**
```bash
# Create ~/.aws/credentials
mkdir -p ~/.aws
cat > ~/.aws/credentials <<EOF
[minio]
aws_access_key_id = hbinduni
aws_secret_access_key = Jakarta123!
EOF
```

3. **Test connection:**
```bash
# List buckets
aws --endpoint-url https://s3a.larahq.com \
    --profile minio \
    s3 ls

# List files in bucket
aws --endpoint-url https://s3a.larahq.com \
    --profile minio \
    s3 ls s3://id1/

# Upload test file
echo "test" > test.txt
aws --endpoint-url https://s3a.larahq.com \
    --profile minio \
    s3 cp test.txt s3://id1/test.txt

# Download test file
aws --endpoint-url https://s3a.larahq.com \
    --profile minio \
    s3 cp s3://id1/test.txt downloaded.txt
```

### Method 3: Using curl (Advanced)

Test raw HTTP requests to MinIO:

```bash
# Check if MinIO is accessible
curl -I https://s3a.larahq.com

# Check bucket (requires proper AWS v4 signature - complex)
# Better to use mc or aws cli
```

## Current Status After Update

With the updated code, here's what happens:

1. ✅ **App starts normally** - Storage initialization completes
2. ⚠️ **Warning logged** - About bucket creation failure (expected if bucket exists)
3. ✅ **Bucket verified** - Secondary check confirms bucket is accessible
4. ✅ **App continues** - Storage is ready to use

The warning you see is **informational only** and doesn't prevent the app from working.

## Verifying It Works

To verify media storage is working:

1. **Start the app:**
```bash
make run
```

2. **Send a message with media** (via API or UI)

3. **Check logs** for successful upload:
```
Successfully uploaded media to S3: ...
```

4. **Verify in MinIO:**
```bash
mc ls larahq/id1/
```

## Common Fixes

### Fix 1: Verify Credentials
```bash
# Test with mc client
mc alias set test https://s3a.larahq.com hbinduni Jakarta123!

# If this fails, your credentials are wrong
# If it works, continue...

mc ls test
```

### Fix 2: Create Bucket Before Starting Application
```bash
# Create bucket using MinIO Client (mc)
mc mb larahq/id1

# Or via AWS CLI
aws --endpoint-url https://s3a.larahq.com s3 mb s3://id1

# Verify bucket was created
mc ls larahq/
```

**Note**: The application will NOT create the bucket automatically. You must create it manually before starting the application.

### Fix 3: Check Permissions
Make sure your MinIO user has these permissions:
- `s3:PutObject` - Upload files
- `s3:GetObject` - Download files
- `s3:DeleteObject` - Delete files
- `s3:ListBucket` - List bucket contents
- `s3:HeadBucket` - Check bucket exists

In MinIO console:
1. Go to Identity → Users
2. Select your user (hbinduni)
3. Check assigned policies
4. Ensure policy allows the above actions on bucket `id1`

### Fix 4: Temporarily Use Local Storage

If S3 continues to have issues, switch to local storage:

```env
# In src/.env
MEDIA_STORAGE_TYPE=local
# Comment out S3 settings
```

Then:
```bash
make run
```

## Getting More Debug Info

Enable debug logging:

```env
# In src/.env
APP_DEBUG=true
```

This will show:
- Detailed S3 request/response logs
- Signature calculation details
- Exact error messages

## Current Implementation: MinIO Native SDK

The application now uses MinIO's native Go SDK (`github.com/minio/minio-go/v7`) instead of AWS SDK v2. This provides:

✅ **Full compatibility** with MinIO servers
✅ **No signature calculation issues**
✅ **Same library used by `mc` client**
✅ **Better performance** with MinIO

The MinIO SDK is automatically selected when you set `MEDIA_STORAGE_TYPE=s3` in your `.env` file.

## Still Having Issues?

If you've tried everything above:

1. **Collect diagnostic info:**
```bash
# Check what MinIO sees
curl -v https://s3a.larahq.com 2>&1 | grep -i "server:"

# Check system time
date && curl -sI https://s3a.larahq.com | grep -i date

# Test with verbose AWS CLI
aws --endpoint-url https://s3a.larahq.com \
    --debug \
    s3 ls 2>&1 | grep -i signature
```

2. **Check MinIO logs** (if you have access to server)

3. **Try regenerating access keys** in MinIO console

4. **Verify MinIO version** - Older versions might have compatibility issues with AWS SDK v2
