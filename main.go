package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/disintegration/imaging"
	"github.com/joho/godotenv"
)

// Memory CacheEntry struct for in memory caching
type CacheEntry struct {
	Data      []byte
	createdAt time.Time
}

var (
	imageCache     = make(map[string]*CacheEntry)
	cacheMutex     sync.RWMutex
	totalCacheSize int64

	cacheTTL     time.Duration
	maxCacheSize int64
	maxFileSize  int64 // Maximum file size to download from S3
)

func main() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file")
	}

	// Pull cache info and set up in-memory cache
	cacheTTLseconds, err := strconv.Atoi(os.Getenv("CACHE_TTL_SECONDS"))
	if err != nil {
		cacheTTLseconds = 2592000 // fallback default TTL (30 days)
	}
	cacheTTL = time.Duration(cacheTTLseconds) * time.Second

	maxCacheSizeMB, err := strconv.Atoi(os.Getenv("CACHE_MEMORY_SIZE_MB"))
	if err != nil {
		maxCacheSizeMB = 512 // fallback default max cache size (MB)
	}
	maxCacheSize = int64(maxCacheSizeMB) * 1024 * 1024

	// Set max file size limit (default 50MB) to prevent memory exhaustion from large files
	maxFileSizeMB, err := strconv.Atoi(os.Getenv("MAX_FILE_SIZE_MB"))
	if err != nil {
		maxFileSizeMB = 50 // fallback default max file size (MB)
	}
	maxFileSize = int64(maxFileSizeMB) * 1024 * 1024

	log.Printf("cache Configured: TTL=%v, MaxSize=%dMB, MaxFileSize=%dMB\n", cacheTTL, maxCacheSizeMB, maxFileSizeMB)

	// Start background goroutine to periodically clean up expired cache entries
	go cacheCleanupWorker()

	// Setup S3 Client
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(os.Getenv("S3_REGION")),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			os.Getenv("S3_ACCESS_KEY"),
			os.Getenv("S3_SECRET_KEY"),
			"",
		)),
	)
	if err != nil {
		log.Fatal("Failed to load S3 config", err)
	}
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(os.Getenv("S3_ENDPOINT"))
		o.UsePathStyle = true
	})

	http.HandleFunc("/public/", func(w http.ResponseWriter, r *http.Request) {
		handleImage(w, r, s3Client)
	})
	http.HandleFunc("/private/", handlePrivate)
	http.HandleFunc("/api/", handleAPI)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("Server Started on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// These are the 3 major handlers for the server
// handleImage is reponsible for pulling images, manipulating them, and sending them back.
func handleImage(w http.ResponseWriter, r *http.Request, s3Client *s3.Client) {
	widthMax, err := strconv.Atoi(os.Getenv("WIDTH_MAX"))
	if err != nil {
		widthMax = 4096 // fallback default max width
	}
	heightMax, err := strconv.Atoi(os.Getenv("HEIGHT_MAX"))
	if err != nil {
		heightMax = 4096 // fallback default max height
	}

	imgKey := strings.TrimPrefix(r.URL.Path, "/public/")

	// Security: Validate path to prevent directory traversal attacks
	imgKey = path.Clean(imgKey)
	if strings.Contains(imgKey, "..") || strings.HasPrefix(imgKey, "/") || strings.HasPrefix(imgKey, "\\") {
		log.Printf("Security: Blocked directory traversal attempt: %s", imgKey)
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if imgKey == "." || imgKey == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Always access files under public/ prefix in S3 bucket
	imgKey = "public/" + imgKey

	query := r.URL.Query()

	// Fetch query params for manipulation

	// Validate width parameter: must be positive and within max limit
	width, err := strconv.Atoi(query.Get("w"))
	if err != nil || width > widthMax || width < 0 {
		width = 0 // default width (no resizing)
	}
	// Validate height parameter: must be positive and within max limit
	height, err := strconv.Atoi(query.Get("h"))
	if err != nil || height > heightMax || height < 0 {
		height = 0 // default height (no resizing)
	}
	// Validate quality parameter: must be between 1-100
	quality, err := strconv.Atoi(query.Get("q"))
	if err != nil {
		quality, err = strconv.Atoi(os.Getenv("DEFAULT_QUALITY")) // default quality
		if err != nil {
			quality = 85 // fallback default quality
		}
	}
	// Clamp quality to valid JPEG range (1-100)
	if quality < 1 {
		quality = 1
	} else if quality > 100 {
		quality = 100
	}

	form := strings.ToLower(query.Get("f"))

	// Generate key to check cache
	cacheKey := getCacheKey(imgKey, width, height, quality, form)
	// Check in memory cache
	cacheMutex.RLock()
	if entry, exists := imageCache[cacheKey]; exists {
		if time.Since(entry.createdAt) < cacheTTL {
			cacheMutex.RUnlock()

			// Cache hit, return cached image
			log.Println("Cache hit for key:", cacheKey)

			hash := fmt.Sprintf(`"%x"`, md5.Sum(entry.Data))
			w.Header().Set("Content-Type", http.DetectContentType(entry.Data))
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("Content-Length", strconv.Itoa(len(entry.Data)))
			w.Header().Set("ETag", hash)
			w.Header().Set("Expires", time.Now().Add(365*24*time.Hour).UTC().Format(http.TimeFormat))
			w.Write(entry.Data)
			return
		}
	}
	cacheMutex.RUnlock()
	log.Println("Cache miss for key:", cacheKey)

	// Use context with timeout to prevent hanging on slow S3 responses
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch and read image from S3
	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
		Key:    aws.String(imgKey),
	})
	if err != nil {
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}
	defer result.Body.Close()

	// Check file size before downloading to prevent memory exhaustion
	if result.ContentLength != nil && *result.ContentLength > maxFileSize {
		log.Printf("File too large: %d bytes (max: %d)", *result.ContentLength, maxFileSize)
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Use LimitReader to enforce max size even if ContentLength is not set
	fileData, err := io.ReadAll(io.LimitReader(result.Body, maxFileSize+1))
	if err != nil {
		http.Error(w, "Error reading image data", http.StatusInternalServerError)
		return
	}
	// Check if we hit the limit (file too large)
	if int64(len(fileData)) > maxFileSize {
		log.Printf("File exceeded size limit during read: %d bytes", len(fileData))
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Fetch original file info & define defaults
	config, format, err := image.DecodeConfig(bytes.NewReader(fileData))
	if err != nil {
		http.Error(w, "Error decoding image", http.StatusInternalServerError)
		return
	}
	originalWidth := config.Width
	originalHeight := config.Height
	originalFormat := format

	// Define output format, default to original if not specified
	var finalFormat string
	if form == "" {
		finalFormat = originalFormat
	} else {
		finalFormat = strings.ToLower(form)
	}
	// If both width and height are 0, use original dimensions
	if width == 0 && height == 0 {
		width = originalWidth
		height = originalHeight
	}

	// Decode the image into bytes
	img, err := imaging.Decode(bytes.NewReader(fileData))
	if err != nil {
		http.Error(w, "Error decoding image", http.StatusInternalServerError)
		return
	}
	// Manipulate the image based on query params
	resizedImg := imaging.Resize(img, width, height, imaging.Lanczos)
	// Encode the manipulated image back to bytes
	var buf bytes.Buffer
	var encodeErr error
	switch finalFormat {
	case "jpeg", "jpg":
		encodeErr = imaging.Encode(&buf, resizedImg, imaging.JPEG, imaging.JPEGQuality(quality))
	case "png":
		encodeErr = imaging.Encode(&buf, resizedImg, imaging.PNG)
	case "gif":
		encodeErr = imaging.Encode(&buf, resizedImg, imaging.GIF)
	default:
		encodeErr = imaging.Encode(&buf, resizedImg, imaging.JPEG, imaging.JPEGQuality(quality))
		finalFormat = "jpeg" // default to jpeg if format is unrecognized
	}
	// Check for encoding errors to avoid serving corrupted data
	if encodeErr != nil {
		log.Printf("Error encoding image: %v", encodeErr)
		http.Error(w, "Error processing image", http.StatusInternalServerError)
		return
	}
	processedData := buf.Bytes()

	// Add to cache with eviction if necessary
	cacheMutex.Lock()
	// Evict entries if cache is too large before adding new entry
	for totalCacheSize+int64(len(processedData)) > maxCacheSize && len(imageCache) > 0 {
		evictOldestCacheEntry()
	}
	imageCache[cacheKey] = &CacheEntry{
		Data:      processedData,
		createdAt: time.Now(),
	}
	totalCacheSize += int64(len(processedData))
	cacheMutex.Unlock()

	log.Printf("Cached: %s (size: %d bytes, total cache: %dMB)\n", cacheKey, len(processedData), totalCacheSize/(1024*1024))

	//Response
	// return headers
	hash := fmt.Sprintf(`"%x"`, md5.Sum(processedData))
	contentType := http.DetectContentType(processedData)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Cache for 1 year
	w.Header().Set("Content-Length", strconv.Itoa(len(processedData)))
	w.Header().Set("ETag", hash)
	w.Header().Set("Expires", time.Now().Add(365*24*time.Hour).UTC().Format(http.TimeFormat))

	// return the image
	w.Write(processedData)
	log.Println("Served image:", imgKey, "with width:", width, "height:", height, "quality:", quality)
}

func getCacheKey(imgKey string, keyWidth, keyHeight, quality int, format string) string {
	return fmt.Sprintf("%s_w%d_h%d_q%d_f%s", imgKey, keyWidth, keyHeight, quality, format)
}

// cacheCleanupWorker runs periodically to remove expired cache entries
// This prevents memory leaks from expired entries staying in cache forever
func cacheCleanupWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cacheMutex.Lock()
		expiredCount := 0
		for key, entry := range imageCache {
			if time.Since(entry.createdAt) > cacheTTL {
				totalCacheSize -= int64(len(entry.Data))
				delete(imageCache, key)
				expiredCount++
			}
		}
		cacheMutex.Unlock()
		if expiredCount > 0 {
			log.Printf("Cache cleanup: removed %d expired entries, total cache: %dMB\n", expiredCount, totalCacheSize/(1024*1024))
		}
	}
}

// evictOldestCacheEntry removes the oldest cache entry to make room for new ones
// Must be called with cacheMutex locked
func evictOldestCacheEntry() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range imageCache {
		if first || entry.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.createdAt
			first = false
		}
	}

	if oldestKey != "" {
		totalCacheSize -= int64(len(imageCache[oldestKey].Data))
		delete(imageCache, oldestKey)
		log.Printf("Cache eviction: removed oldest entry %s\n", oldestKey)
	}
}

// handlePrivate is responsible for authenticated images, like profiles or account specific data.
func handlePrivate(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("This feature will be available in future updates..."))
}

// handleAPI is responsible for telemetry. i.e server load, request info, ect.
func handleAPI(w http.ResponseWriter, r *http.Request) {
	apiPath := strings.TrimPrefix(r.URL.Path, "/api/")
	if apiPath == "ping" {
		w.Write([]byte("pong"))
		return
	} else {
		w.Write([]byte("This feature will be available in future updates..."))
	}
}
