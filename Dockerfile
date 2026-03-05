# Stage 1: Build the Go application
FROM golang:1.24-alpine AS builder

# Set working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum first (better caching - only re-download if these change)
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary
# CGO_ENABLED=0 means no C dependencies (fully static binary)
# -a forces rebuild of all packages
# -installsuffix cgo adds a suffix to package directories (for cgo vs non-cgo)
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o img-processor .

# Stage 2: Create minimal runtime image
FROM alpine:latest

# Install CA certificates (needed for HTTPS requests to S3)
RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Set working directory
WORKDIR /home/appuser

# Copy the binary from the builder stage
COPY --from=builder /app/img-processor .

# Change ownership to non-root user
RUN chown appuser:appgroup img-processor

# Switch to non-root user
USER appuser

# Expose port 8080
EXPOSE 8080

# Run the application
CMD ["./img-processor"]
