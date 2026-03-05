package main

import (
	"bytes"
	"image"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"context"
	"io"
	"crypto/md5"
	"fmt"
	"time"
	"sync"

	"github.com/disintegration/imaging"
	"github.com/joho/godotenv"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)
// Memory CacheEntry struct for in memory caching
type CacheEntry struct {
	Data	  []byte
	createdAt time.Time
}

var (
	imageCache   = make(map[string]*CacheEntry)
	cacheMutex     sync.RWMutex
	totalCacheSize int64

	cacheTTL     time.Duration
	maxCacheSize int64
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

	log.Printf("cache Configured: TTL=%v, MaxSize=%dMB\n", cacheTTL, maxCacheSizeMB)
	
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
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options){
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
	query := r.URL.Query()

	// Fetch query params for manipulation

	width, err := strconv.Atoi(query.Get("w"))
	if err != nil || width > widthMax {
		width = 0 // default width (no resizing)
	}
	height, err := strconv.Atoi(query.Get("h"))
	if err != nil || height > heightMax {
		height = 0 // default height (no resizing)
	}
	quality, err := strconv.Atoi(query.Get("q"))
	if err != nil {
		quality, err = strconv.Atoi(os.Getenv("DEFAULT_QUALITY")) // default quality
		if err != nil {
			quality = 85 // fallback default quality
		}
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

	// Fetch and read image from S3
	result, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
		Key:    aws.String(imgKey),
	})
	if err != nil {
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}
	defer result.Body.Close()
	fileData, err := io.ReadAll(result.Body)
	if err != nil {
		http.Error(w, "Error reading image data", http.StatusInternalServerError)
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
	switch finalFormat {
	case "jpeg", "jpg":
		imaging.Encode(&buf, resizedImg, imaging.JPEG, imaging.JPEGQuality(quality))
	case "png":
		imaging.Encode(&buf, resizedImg, imaging.PNG)
	case "gif":
		imaging.Encode(&buf, resizedImg, imaging.GIF)
	default:
		imaging.Encode(&buf, resizedImg, imaging.JPEG, imaging.JPEGQuality(quality))
		finalFormat = "jpeg" // default to jpeg if format is unrecognized
	}
	processedData := buf.Bytes()

	// Hit Cache
	cacheMutex.Lock()
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
