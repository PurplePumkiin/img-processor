# Image Processor
**A self-contained, production-ready image processing service for S3-backed storage**

> **Status:** 🚧 Planned - Development not yet started  
> **License:** MIT  
> **Language:** Go (planned)
> **Container:** Docker

---

## Abstract

A lightweight, containerized image processing service designed to sit between web applications and S3 object storage. The processor fetches images from private S3 buckets, applies on-demand transformations (resize, compress, format conversion), strips privacy-sensitive EXIF metadata, and serves optimized images with aggressive caching for maximum CDN efficiency.

**Built for developers who want:**
- Private S3 compatible buckets (no presigned URLs)
- On-demand image transformations via URL parameters
- EXIF stripping for privacy
- Cacheable, permanent URLs
- Easy deployment via Docker
- Minimal configuration (just S3 credentials)

---

## Planned Capabilities

### Core Features (v1.0 MVP)
- ✅ **S3 Integration** - Fetch from private S3 compatible buckets
- ✅ **EXIF Stripping** - Remove metadata for privacy and size reduction
- ✅ **Smart Caching** - Filesystem-based cache with automatic cleanup
- ✅ **CDN-Friendly** - Immutable URLs with long cache headers
- ✅ **Docker Container** - Self-contained, easy to deploy
- ✅ **Environment Config** - Zero hardcoded values, all via .env
- ✅ **Proper HTTP Headers** - Cache-Control, ETag, Content-Type
- ✅ **Error Handling** - Graceful failures, proper status codes

### Enhanced Features (v1.1+)
- 🔄 **On-Demand Resizing** - URL params: `?w=800&h=600`
- 🔄 **Quality Control** - URL param: `?q=85` (0-100)
- 🔄 **Format Conversion** - URL param: `?format=webp`
- 🔄 **Redis Cache** - Faster distributed caching option
- 🔄 **Custom Endpoints** - MinIO, self-hosted S3-compatible storage

### Production Features (v2.0+)
- 🔄 **Rate Limiting** - Per-IP request throttling
- 🔄 **Access Control** - Token-based auth for private images
- 🔄 **Health Checks** - `/health` endpoint for monitoring
- 🔄 **Metrics** - Prometheus-compatible metrics endpoint
- 🔄 **Request Logging** - Structured JSON logs
- 🔄 **Watermarking** - Optional watermark application
- 🔄 **Smart Crop** - Face detection & focal point crops

### Advanced Features (v3.0+)
- 🔄 **Multi-Region S3** - Fetch from nearest bucket
- 🔄 **Image Optimization** - Auto-optimize based on client
- 🔄 **Accept Header Detection** - Auto WebP for modern browsers
- 🔄 **Blur/Sharpen** - Image effect filters
- 🔄 **Batch Processing** - Process multiple images
- 🔄 **Webhooks** - Notify on processing complete

---

## Architecture

```
┌─────────────┐
│   Browser   │
└──────┬──────┘
       │ 1. Request image
       │ https://image.example.com/myImage.jpg?w=800&q=85
       ▼
┌─────────────────┐
│   (CDN Edge)    │ ← 2. Check CDN cache
│                 │    Cache hit? → Serve immediately
└──────┬──────────┘    Cache miss? → Forward to origin
       │
       ▼
┌─────────────────────┐
│ Image Processor     │ ← 3. Check local cache
│ (Docker Container)  │    Cache hit? → Serve
│                     │    Cache miss? → Continue
└──────┬──────────────┘
       │ 4. Fetch from S3
       ▼
┌─────────────────┐
│   S3 Bucket     │ ← 5. Private bucket, no public access
│   (Private)     │    Return image bytes
└──────┬──────────┘
       │
       ▼
┌─────────────────────┐
│ Image Processor     │ ← 6. Process image
│ - Strip EXIF        │    - Remove metadata
│ - Resize (if ?w=)   │    - Resize if requested
│ - Convert format    │    - Change format if requested
│ - Compress          │    - Optimize quality
└──────┬──────────────┘
       │ 7. Save to cache
       │ 8. Set cache headers
       │    Cache-Control: public, max-age=31536000
       ▼
┌─────────────┐
│   Browser   │ ← 9. Display image
└─────────────┘    (Cached for 1 year)

Next request for same URL:
  → CDN serves from edge (< 10ms)
  → No processing, no S3 fetch
  → Infinite scalability
```

---

## URL Format

### Blog Images (Public)
```
https://img.pumkiin.tech/blog/{s3-key}
```

**Examples:**
```
https://img.pumkiin.tech/blog/blog-images/1970/01/photo.jpg
https://img.pumkiin.tech/blog/blog-images/1970/01/photo.jpg?w=800
https://img.pumkiin.tech/blog/blog-images/1970/01/photo.jpg?w=400&q=75&format=webp
```

### User Images (Private, Auth Required - v2.0+)
```
https://img.pumkiin.tech/private/{s3-key}?token={jwt}
```

**Examples:**
```
https://img.pumkiin.tech/private/profile-pictures/user_123.jpg?w=200&token=sHc...
https://img.pumkiin.tech/private/avatars/user_456.jpg?w=200&token=eyJ...
```

### URL Parameters (v1.1+)

| Parameter | Description | Example | Default |
|-----------|-------------|---------|---------|
| `w` | Width in pixels | `?w=800` | Original |
| `h` | Height in pixels | `?h=600` | Original |
| `q` | Quality (0-100) | `?q=85` | 85 |
| `format` | Output format | `?format=webp` | Original |
| `fit` | Resize mode | `?fit=cover` | inside |
| `token` | Auth token (v2.0) | `?token=eyJ...` | - |

**Resize Modes (v1.1+):**
- `inside` - Fit within dimensions, maintain aspect ratio (default)
- `cover` - Fill dimensions, may crop
- `fill` - Exact dimensions, may distort

---

## Configuration

### Environment Variables

```bash
# S3 Configuration (Required)
S3_PROVIDER=aws                      # aws, digitalocean, wasabi, etc.
S3_REGION=us-east-1                  # AWS region
S3_BUCKET=my-images-bucket           # Bucket name
S3_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE   # IAM access key
S3_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
S3_ENDPOINT=                         # Optional: custom endpoint for non-AWS

# Server Configuration
PORT=8080                            # HTTP listen port
LOG_LEVEL=info                       # debug, info, warn, error

# Cache Configuration
CACHE_TYPE=filesystem                # filesystem, redis, memory
CACHE_DIR=/cache                     # Directory for cached images
CACHE_TTL_DAYS=30                    # Cache expiration in days
REDIS_URL=redis://localhost:6379     # If CACHE_TYPE=redis

# Processing Configuration
STRIP_EXIF=true                      # Remove EXIF metadata
MAX_IMAGE_SIZE_MB=10                 # Max source image size
MAX_WIDTH=4096                       # Max output width
MAX_HEIGHT=4096                      # Max output height
DEFAULT_QUALITY=85                   # Default JPEG quality

# Security (v2.0+)
RATE_LIMIT_PER_MINUTE=100            # Requests per IP per minute
AUTH_REQUIRED=false                  # Require auth for all images
ALLOWED_ORIGINS=*                    # CORS origins

# Advanced (v2.0+)
ENABLE_WATERMARK=false               # Apply watermark
WATERMARK_PATH=/watermark.png        # Path to watermark image
WATERMARK_OPACITY=30                 # Watermark opacity (0-100)
ENABLE_METRICS=true                  # Prometheus metrics endpoint
```

---

## Quick Start (When Available)

### Using Docker

```bash
# Pull image
docker pull PurplePumkiin/img-processor:latest

# Run with environment variables
docker run -d \
  -p 8080:8080 \
  -v ./cache:/cache \
  -e S3_ACCESS_KEY=your_key \
  -e S3_SECRET_KEY=your_secret \
  -e S3_BUCKET=your-bucket \
  -e S3_REGION=us-east-1 \
  PurplePumkiin/img-processor:latest

# Test
curl http://localhost:8080/public/{s3-key}
```

### Using Docker Compose

```yaml
version: '3.8'
services:
  img-processor:
    image: PurplePumkiin/img-processor:latest
    ports:
      - "8080:8080"
    environment:
      - S3_ACCESS_KEY=${S3_ACCESS_KEY}
      - S3_SECRET_KEY=${S3_SECRET_KEY}
      - S3_BUCKET=${S3_BUCKET}
      - S3_REGION=${S3_REGION}
    volumes:
      - ./cache:/cache
    restart: unless-stopped
```

```bash
# Create .env file with credentials
cp .env.example .env
nano .env

# Start
docker-compose up -d

# View logs
docker-compose logs -f

# Stop
docker-compose down
```

### Building from Source

```bash
# Clone repository
git clone https://github.com/PurplePumkiin/img-processor
cd img-processor

# Build Docker image
docker build -t img-processor:latest .

# Run
docker run -p 8080:8080 --env-file .env img-processor:latest
```

---

## Integration Example

### PHP Upload Handler
```php
// Upload to S3
$s3Client->putObject([
    'Bucket' => 'my-bucket',
    'Key' => 'blog-images/1970/01/test.jpg',
    'SourceFile' => $file['tmp_name'],
    'ACL' => 'private'  // Keep private!
]);

// Return image processor URL (not S3 URL)
$imageUrl = 'https://img.pumkiin.tech/blog/blog-images/1970/01/test.jpg';

// Store in database
$stmt->execute([':image_url' => $imageUrl]);
```

### HTML Usage
```html
<!-- Original size -->
<img src="https://img.pumkiin.tech/blog/blog-images/1970/01/test.jpg" 
     alt="Blog post image">

<!-- Responsive sizes (v1.1+) -->
<img srcset="
    https://img.pumkiin.tech/blog/photo.jpg?w=400 400w,
    https://img.pumkiin.tech/blog/photo.jpg?w=800 800w,
    https://img.pumkiin.tech/blog/photo.jpg?w=1200 1200w
" sizes="(max-width: 600px) 400px, (max-width: 900px) 800px, 1200px"
   src="https://img.pumkiin.tech/blog/photo.jpg?w=800"
   alt="Blog post image">

<!-- Modern format with fallback (v1.1+) -->
<picture>
  <source srcset="https://img.pumkiin.tech/blog/photo.jpg?format=webp" 
          type="image/webp">
  <img src="https://img.pumkiin.tech/blog/photo.jpg" 
       alt="Blog post image">
</picture>
```

### JavaScript Usage
```javascript
// Dynamic image loading
const imageUrl = `https://img.pumkiin.tech/blog/${s3Key}`;
const thumbnail = `${imageUrl}?w=300&q=70`;
const fullsize = `${imageUrl}?w=1200`;

// Check if image exists
fetch(imageUrl, { method: 'HEAD' })
  .then(res => {
    if (res.ok) {
      console.log('Image available');
      console.log('Cached:', res.headers.get('X-Cache')); // HIT or MISS
    }
  });
```

---

## Performance Characteristics (Projected)

### Response Times (Target)
- **Cache HIT (CloudFlare):** < 50ms (served from edge)
- **Cache HIT (Local):** < 100ms (served from filesystem)
- **Cache MISS (Fetch + Process):** 200-500ms (fetch S3 + process)

### Resource Usage (Target)
- **Memory:** 30-50MB base, +10MB per concurrent request
- **CPU:** Minimal when cached, ~200ms process time per image
- **Disk:** Cache size = active working set (auto-cleanup)
- **Network:** Only S3 fetches on cache miss

### Scalability
- **Horizontal:** Add more containers behind load balancer
- **Vertical:** Single container handles 1000+ req/s (cached)
- **Cache Hit Rate:** Expected >95% after warmup
- **Cost:** ~$5-10/month for small-medium sites

### Docker Image Size
- **Go:** 20-50MB (multi-stage build)
- **Startup Time:** < 1 second

---

## Why This Approach?

### vs. Presigned URLs
❌ **Presigned URLs**
- Expire after X hours/days
- Can't be cached effectively (changing query params)
- No image processing
- EXIF data exposed

✅ **Image Processor**
- URLs never expire
- Perfect CDN caching
- On-demand transformations
- Privacy-safe (EXIF stripped)

### vs. Public S3 Bucket
❌ **Public S3**
- Anyone can access with URL
- Can't control access after upload
- No processing
- Security risk

✅ **Image Processor**
- S3 stays private
- Access control at processor level
- Process before serving
- Secure by default

### vs. CloudFlare Images / Cloudinary
❌ **SaaS Solutions**
- Monthly cost ($20-200+)
- Vendor lock-in
- Data sent to third party
- Limited customization

✅ **Self-Hosted Processor**
- One-time setup, minimal cost
- Full control
- Data stays with you
- Customize anything

---

## Technical Stack (Planned)

### Core
- **Language:** Go
- **HTTP Server:** net/http
- **Image Library:** disintegration/imaging
- **S3 SDK:** aws-sdk-go-v2

### Dependencies (Go)
```go
github.com/aws/aws-sdk-go-v2/service/s3  // S3 client
github.com/disintegration/imaging        // Image processing
github.com/joho/godotenv                 // .env support
github.com/gin-gonic/gin                 // HTTP framework (optional)
```

### Container
- **Base Image:** Alpine Linux (Go)
- **Size:** 20-50MB 
- **Build:** Multi-stage Dockerfile
- **Runtime:** Single binary

---

## Security Considerations

### Implemented
✅ Private S3 buckets (no public access)  
✅ EXIF stripping (privacy protection)  
✅ Input validation (file size, format)  
✅ Proper error handling (no stack traces exposed)  
✅ Content-Type validation  
✅ Path traversal prevention  

### Planned (v2.0+)
🔄 Rate limiting per IP  
🔄 Authentication for private images  
🔄 Request signing for sensitive operations  
🔄 CORS configuration  
🔄 DDoS protection (via CloudFlare)  

### Best Practices
- Run container as non-root user
- Use read-only S3 credentials (GetObject only)
- Keep S3 bucket private (Block Public Access = ON)
- Enable DDoS protection
- Monitor for abuse (metrics)
- Regular security updates (Docker base image)

---

## Roadmap

### v1.0 - MVP (Target: Week 1-2)
- [x] Project planning
- [ ] Basic S3 fetch + serve
- [ ] EXIF stripping
- [ ] Filesystem cache
- [ ] Docker packaging
- [ ] Documentation
- [ ] GitHub release

### v1.1 - Enhanced Processing (Target: Week 3-4)
- [ ] Resize (width, height)
- [ ] Quality control
- [ ] Format conversion
- [ ] Redis cache support
- [ ] Better error handling

### v2.0 - Production (Target: Month 2)
- [ ] Rate limiting
- [ ] Access control / auth
- [ ] Health checks
- [ ] Prometheus metrics
- [ ] Structured logging
- [ ] Multi-provider S3 support

### v3.0 - Advanced (Target: Month 3+)
- [ ] Smart crop with face detection
- [ ] Watermarking
- [ ] Blur/sharpen effects
- [ ] Batch processing
- [ ] Admin dashboard
- [ ] Auto-optimization

---

## Contributing (When Open-Sourced)

We welcome contributions! Areas where help is needed:
-  Bug reports and fixes
-  Documentation improvements
-  Feature requests and implementations
-  Test coverage
-  Translations
-  Package managers (Homebrew, apt, etc.)

**Contribution Guidelines:** Coming soon in CONTRIBUTING.md

---

## License

**MIT License** - Free for commercial and personal use

```
Copyright (c) 2026 Dennis

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.
```

Full license: [LICENSE](LICENSE)

---

## Support

**Status:** Project not yet started (In Progress)

Once released:
-  Documentation: [GitHub Wiki](coming-soon)
-  Bug Reports: [GitHub Issues](coming-soon)
-  Discussions: [GitHub Discussions](coming-soon)  
-  Security Issues: [security@yourdomain.com](coming-soon)

---

## Acknowledgments

Inspired by:
- [Cloudinary](https://cloudinary.com/) - Image CDN and processing
- [imgix](https://imgix.com/) - Real-time image transformation
- [Thumbor](https://github.com/thumbor/thumbor) - Open-source imaging service
- [imaginary](https://github.com/h2non/imaginary) - Go-based image server

Built with the goal of making professional-grade image processing accessible to
everyone through simple, self-hosted Docker containers.

---

**Status:** 🚧 This project is in the planning phase. Development will begin once
the main site's S3 upload system is complete and tested. Star the repo to follow
progress! (Repository link coming soon)
