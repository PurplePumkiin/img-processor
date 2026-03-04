package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	http.HandleFunc("/public/", handleImage)
	http.HandleFunc("/private/", handlePrivate)
	http.HandleFunc("/api/", handleAPI)
	log.Println("Server Started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// These are the 3 major handlers for the server
// handleImage is reponsible for pulling images, manipulating them, and sending them back.
func handleImage(w http.ResponseWriter, r *http.Request) {
	imgKey := strings.TrimPrefix(r.URL.Path, "/public/")

	// Fetch the image (for now from the local fs)
	fullPath := filepath.Join("testData", imgKey)
	fileData, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}

	//Response
	// return headers
	contentType := http.DetectContentType(fileData)
	w.Header().Set("Content-Type", contentType)

	// return the image
	w.Write(fileData)
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
