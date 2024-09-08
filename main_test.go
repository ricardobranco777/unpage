package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestUnpage_SinglePage(t *testing.T) {
	// Mock single page response
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{
			"data": []any{
				map[string]any{"id": 1, "name": "Item 1"},
				map[string]any{"id": 2, "name": "Item 2"},
			},
		}
		json.NewEncoder(w).Encode(data)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Test unpage function with single page response
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := map[string]string{}
	paramPage := "page"
	dataKey := "data"
	nextKey := ""
	lastKey := ""

	entries, err := unpage(ctx, server.URL, headers, paramPage, dataKey, nextKey, lastKey, 5)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}
}

func TestUnpage_ErrorResponse(t *testing.T) {
	// Mock error response
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Test unpage function with an error response
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := map[string]string{}
	paramPage := "page"
	dataKey := "data"
	nextKey := ""
	lastKey := ""

	_, err := unpage(ctx, server.URL, headers, paramPage, dataKey, nextKey, lastKey, 5)
	if err == nil {
		t.Fatalf("Expected error, got none")
	}
}

func TestUnpage_PaginationViaLinkHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		if page == "1" {
			w.Header().Set("Link", `</?page=2>; rel="next"`)
			data := map[string]any{
				"data": []any{
					map[string]any{"id": 1, "name": "Item 1"},
					map[string]any{"id": 2, "name": "Item 2"},
				},
			}
			json.NewEncoder(w).Encode(data)
		} else if page == "2" {
			data := map[string]any{
				"data": []any{
					map[string]any{"id": 3, "name": "Item 3"},
					map[string]any{"id": 4, "name": "Item 4"},
				},
			}
			json.NewEncoder(w).Encode(data)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := map[string]string{}
	paramPage := "page"
	dataKey := "data"
	nextKey := ""
	lastKey := ""

	entries, err := unpage(ctx, server.URL, headers, paramPage, dataKey, nextKey, lastKey, 5)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("Expected 4 entries, got %d", len(entries))
	}
}

func TestUnpage_MultiplePages(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var data map[string]any

		scheme := "http" // Use "http" as httptest servers use http, not https.
		if r.TLS != nil {
			scheme = "https"
		}

		if page == 1 || page == 0 {
			// First page, return first set of entries with full URL for "next"
			data = map[string]any{
				"data": []any{
					map[string]any{"id": 1, "name": "Item 1"},
					map[string]any{"id": 2, "name": "Item 2"},
				},
				"links": map[string]any{
					"next": fmt.Sprintf("%s://%s?page=2", scheme, r.Host), // Full URL for the next page
				},
			}
		} else if page == 2 {
			// Second page, return remaining entries, no "next"
			data = map[string]any{
				"data": []any{
					map[string]any{"id": 3, "name": "Item 3"},
					map[string]any{"id": 4, "name": "Item 4"},
				},
				"links": map[string]any{"next": nil},
			}
		} else {
			t.Fatalf("Unexpected page number: %d", page)
		}

		json.NewEncoder(w).Encode(data)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := map[string]string{}
	paramPage := "page"
	dataKey := "data"
	nextKey := "links.next"
	lastKey := ""

	// Construct a full base URL for the test
	baseURL := server.URL

	// Run the unpage function
	entries, err := unpage(ctx, baseURL, headers, paramPage, dataKey, nextKey, lastKey, 5)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Check if all entries were retrieved
	if len(entries) != 4 {
		t.Fatalf("Expected 4 entries, got %d", len(entries))
	}
}

func TestUnpage_WithLastKey(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var data map[string]any

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}

		if page == 1 || page == 0 {
			// First page, return first set of entries with "next" and "last"
			data = map[string]any{
				"data": []any{
					map[string]any{"id": 1, "name": "Item 1"},
					map[string]any{"id": 2, "name": "Item 2"},
				},
				"links": map[string]any{
					"next": fmt.Sprintf("%s://%s?page=2", scheme, r.Host), // Full URL for the next page
					"last": fmt.Sprintf("%s://%s?page=3", scheme, r.Host), // Full URL for the last page
				},
			}
		} else if page == 2 {
			// Second page, return more entries with "next" and "last"
			data = map[string]any{
				"data": []any{
					map[string]any{"id": 3, "name": "Item 3"},
					map[string]any{"id": 4, "name": "Item 4"},
				},
				"links": map[string]any{
					"next": fmt.Sprintf("%s://%s?page=3", scheme, r.Host), // Full URL for the next page
					"last": fmt.Sprintf("%s://%s?page=3", scheme, r.Host), // Full URL for the last page
				},
			}
		} else if page == 3 {
			// Last page, return remaining entries, no "next"
			data = map[string]any{
				"data": []any{
					map[string]any{"id": 5, "name": "Item 5"},
					map[string]any{"id": 6, "name": "Item 6"},
				},
				"links": map[string]any{
					"next": nil, // No more pages
					"last": fmt.Sprintf("%s://%s?page=3", scheme, r.Host),
				},
			}
		} else {
			t.Fatalf("Unexpected page number: %d", page)
		}

		json.NewEncoder(w).Encode(data)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := map[string]string{}
	paramPage := "page"
	dataKey := "data"
	nextKey := "links.next"
	lastKey := "links.last"

	// Construct a full base URL for the test
	baseURL := server.URL

	// Run the unpage function
	entries, err := unpage(ctx, baseURL, headers, paramPage, dataKey, nextKey, lastKey, 5)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Check if all entries were retrieved
	if len(entries) != 6 {
		t.Fatalf("Expected 6 entries, got %d", len(entries))
	}
}
