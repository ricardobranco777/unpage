package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tomnomnom/linkheader"
	"golang.org/x/sync/errgroup"
)

import flag "github.com/spf13/pflag"

const version = "0.1.0"

func getNestedValue(data map[string]any, key string) any {
	keys := strings.Split(key, ".")
	var value any = data

	for _, k := range keys {
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value, ok = m[k]
		if !ok {
			return nil
		}
	}
	return value
}

func getNextLastLinks(header string) (next, last string) {
	links := linkheader.Parse(header)
	for _, link := range links {
		if link.Rel == "next" {
			next = link.URL
		} else if link.Rel == "last" {
			last = link.URL
		}
	}
	return next, last
}

func logResponse(resp *http.Response) {
	dump, err := httputil.DumpRequestOut(resp.Request, true)
	if err != nil {
		log.Print(err)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s", string(dump))
	}

	dump, err = httputil.DumpResponse(resp, true)
	if err != nil {
		log.Print(err)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s\n", string(dump))
	}
}

func getPage(ctx context.Context, urlStr string, headers map[string]string, params map[string]string, timeout time.Duration) (*http.Response, error) {
	client := &http.Client{
		Timeout: timeout * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if params != nil {
		q := req.URL.Query()
		for k, v := range params {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if os.Getenv("DEBUG") != "" {
		logResponse(resp)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP request failed with status %d: %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))
	}
	return resp, nil
}

func unpage(ctx context.Context, urlStr string, headers map[string]string, paramPage, dataKey, nextKey, lastKey string, timeout time.Duration) ([]any, error) {
	// Fetch the first page
	params := make(map[string]string)
	if paramPage != "" {
		params[paramPage] = "1"
	}
	resp, err := getPage(ctx, urlStr, headers, params, timeout)
	if err != nil {
		return nil, err
	}
	var rawBody any
	if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
		return nil, err
	}
	resp.Body.Close()

	var nextLink, lastLink string
	var entries []any
	var ok bool
	switch body := rawBody.(type) {
	case map[string]any:
		if entries, ok = getNestedValue(body, dataKey).([]any); !ok {
			return nil, fmt.Errorf("unexpected type for dataKey")
		}
		// Pagination done via data
		if nextKey != "" {
			if nextLink, ok = getNestedValue(body, nextKey).(string); !ok {
				return nil, fmt.Errorf("unexpected value for nextKey")
			}
		}
		if lastKey != "" {
			if lastLink, ok = getNestedValue(body, lastKey).(string); !ok {
				return nil, fmt.Errorf("unexpected value for lastKey")
			}
		}
	case []any:
		entries = body
	default:
		return nil, fmt.Errorf("wrong type %T", body)
	}

	// Pagination done via Link headers
	if nextKey == "" {
		nextLink, lastLink = getNextLastLinks(resp.Header.Get("Link"))
	}

	// If last Link is available, calculate the number of pages
	if lastLink != "" {
		if strings.HasPrefix(lastLink, "/") {
			lastLink = fmt.Sprintf("%s://%s%s", resp.Request.URL.Scheme, resp.Request.URL.Host, lastLink)
		}
		lastURL, err := url.Parse(lastLink)
		if err != nil {
			return nil, err
		}
		lastPage, err := strconv.Atoi(lastURL.Query().Get(paramPage))
		if err != nil {
			return nil, err
		}

		pages := make([][]any, lastPage)
		pages[0] = entries

		var mu sync.Mutex
		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(50)

		// Fetch remaining pages concurrently
		for page := 2; page <= lastPage; page++ {
			page := page
			g.Go(func() error {
				params := map[string]string{
					paramPage: strconv.Itoa(page),
				}
				resp, err := getPage(ctx, urlStr, headers, params, timeout)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				var rawBody any
				if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
					return err
				}

				var more []any
				var ok bool
				switch body := rawBody.(type) {
				case map[string]any:
					if more, ok = getNestedValue(body, dataKey).([]any); !ok {
						return fmt.Errorf("unexpected type for dataKey")
					}
				case []any:
					more = body
				default:
					return fmt.Errorf("wrong type %T", body)
				}

				mu.Lock()
				pages[page-1] = more
				entries = append(entries, more...)
				mu.Unlock()
				return nil
			})
		}

		// Wait for all goroutines to complete
		if err := g.Wait(); err != nil {
			return nil, err
		}

		// Flatten the pages into a single slice
		var allEntries []any
		for i := range pages {
			allEntries = append(allEntries, pages[i]...)
		}
		return allEntries, nil

	}

	// Iterate using next Link
	for nextLink != "" {
		if strings.HasPrefix(nextLink, "/") {
			nextLink = fmt.Sprintf("%s://%s%s", resp.Request.URL.Scheme, resp.Request.URL.Host, nextLink)
		}
		resp, err := getPage(ctx, nextLink, headers, nil, timeout)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
			return nil, err
		}

		var more []any
		switch body := rawBody.(type) {
		case map[string]any:
			if more, ok = getNestedValue(body, dataKey).([]any); !ok {
				return nil, fmt.Errorf("unexpected type for dataKey")
			}
			if nextKey != "" {
				switch link := getNestedValue(body, nextKey).(type) {
				case string:
					nextLink = link
				case nil:
					nextLink = ""
				default:
					return nil, fmt.Errorf("unexpected type for nextKey")
				}
			}
		case []any:
			more = body
		default:
			return nil, fmt.Errorf("wrong type %T", body)
		}

		if nextKey == "" {
			nextLink, _ = getNextLastLinks(resp.Header.Get("Link"))
		}

		entries = append(entries, more...)
	}
	return entries, nil
}

func main() {
	var headerValues []string
	var paramPage, dataKey, nextKey, lastKey string
	var timeoutInt int

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] URL\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.StringSliceVarP(&headerValues, "header", "H", nil, "HTTP header (may be specified multiple times")
	flag.StringVarP(&paramPage, "param-page", "P", "page", "Name of the parameter that represents the page number")
	flag.StringVarP(&dataKey, "data-key", "D", "", "Key to access the data in the JSON response")
	flag.StringVarP(&nextKey, "next-key", "N", "", "Key to access the next page link in the JSON response")
	flag.StringVarP(&lastKey, "last-key", "L", "", "Key to access the last page link in the JSON response")
	flag.IntVarP(&timeoutInt, "timeout", "t", 60, "Timeout")

	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	urlStr := flag.Args()[0]

	headers := map[string]string{
		"Accept":     "application/json",
		"User-Agent": fmt.Sprintf("unpage/%s", version),
	}
	for _, header := range headerValues {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "ERROR: Invalid header: %s\n", header)
			os.Exit(1)
		}
		headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	timeout := time.Duration(timeoutInt)
	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Second)
	defer cancel()
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	results, err := unpage(ctx, urlStr, headers, paramPage, dataKey, nextKey, lastKey, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	output, err := json.Marshal(results)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(output))
}
