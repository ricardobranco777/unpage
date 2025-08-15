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
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

import flag "github.com/spf13/pflag"

const version = "0.2.0"

var debug bool

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
	for _, chunk := range strings.Split(header, ",") {
		var url, rel string
		for _, piece := range strings.Split(chunk, ";") {
			piece = strings.TrimSpace(piece)
			if strings.HasPrefix(piece, "<") && strings.HasSuffix(piece, ">") {
				url = strings.Trim(piece, "<>")
				continue
			}
			parts := strings.SplitN(piece, "=", 2)
			if len(parts) == 2 {
				key, val := parts[0], strings.Trim(parts[1], `"`)
				if key == "rel" {
					rel = val
				}
			}
		}
		switch rel {
		case "next":
			next = url
		case "last":
			last = url
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

func getPage(ctx context.Context, client *http.Client, urlStr string, headers map[string]string, params map[string]string) (*http.Response, error) {
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

	if debug {
		logResponse(resp)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP request failed with status %d: %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))
	}
	return resp, nil
}

func getInt(body map[string]any, key string) (int, error) {
	if key == "" {
		return 0, nil
	}

	switch v := getNestedValue(body, key).(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("invalid value for %s: %v", key, err)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("unexpected type for %s: %T", key, v)
	}
}

func getString(body map[string]any, key string) (string, error) {
	if key == "" {
		return "", nil
	}

	switch v := getNestedValue(body, key).(type) {
	case string:
		return v, nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("unexpected type for %s: %T", key, v)
	}
}

func getEntries(rawBody any, dataKey string) ([]any, error) {
	var entries []any
	var ok bool

	switch body := rawBody.(type) {
	case map[string]any:
		if entries, ok = getNestedValue(body, dataKey).([]any); !ok {
			return nil, fmt.Errorf("unexpected type for %s: %T", dataKey, dataKey)
		}
	case []any:
		entries = body
	default:
		return nil, fmt.Errorf("wrong JSON type %T", body)
	}

	return entries, nil
}

func unpage(urlStr string, headers map[string]string, paramPage, countKey, dataKey, nextKey, lastKey string) ([]any, error) {
	client := &http.Client{
		Timeout: 120 * time.Second,
	}
	ctx := context.Background()

	// Fetch the first page
	params := make(map[string]string)
	if paramPage != "" {
		params[paramPage] = "1"
	}
	resp, err := getPage(ctx, client, urlStr, headers, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rawBody any
	if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
		return nil, err
	}

	var nextLink, lastLink string
	var entries []any
	var count int

	if entries, err = getEntries(rawBody, dataKey); err != nil {
		return nil, err
	}
	if body, ok := rawBody.(map[string]any); ok {
		// Pagination done via data
		if nextLink, err = getString(body, nextKey); err != nil {
			return nil, err
		}
		if lastLink, err = getString(body, lastKey); err != nil {
			return nil, err
		}
		if count, err = getInt(body, countKey); err != nil {
			return nil, err
		}
	}

	// Pagination done via Link headers
	if nextKey == "" {
		nextLink, lastLink = getNextLastLinks(resp.Header.Get("Link"))
	}

	// If lastLink is available, calculate the number of pages

	var totalPages int

	if lastLink != "" {
		if strings.HasPrefix(lastLink, "/") {
			lastLink = fmt.Sprintf("%s://%s%s", resp.Request.URL.Scheme, resp.Request.URL.Host, lastLink)
		}
		lastURL, err := url.Parse(lastLink)
		if err != nil {
			return nil, err
		}
		if totalPages, err = strconv.Atoi(lastURL.Query().Get(paramPage)); err != nil {
			return nil, err
		}
	} else if count > 0 {
		pageSize := len(entries)
		if pageSize == 0 || count <= pageSize {
			return entries, nil
		}
		totalPages = count / pageSize
		if count%pageSize != 0 {
			totalPages++
		}
	}

	if totalPages > 0 {
		pages := make([][]any, totalPages)
		pages[0] = entries

		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(50)

		// Fetch remaining pages concurrently
		for page := 2; page <= totalPages; page++ {
			page := page
			g.Go(func() error {
				params := map[string]string{
					paramPage: strconv.Itoa(page),
				}
				resp, err := getPage(ctx, client, urlStr, headers, params)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				var entries []any
				var rawBody any

				if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
					return err
				}

				if entries, err = getEntries(rawBody, dataKey); err != nil {
					return err
				}

				pages[page-1] = entries
				return nil
			})
		}

		// Wait for all goroutines to complete
		if err := g.Wait(); err != nil {
			return nil, err
		}

		// Flatten the pages into a single slice
		var all []any
		for i := range pages {
			all = append(all, pages[i]...)
		}
		return all, nil

	}

	// Iterate using next Link
	for nextLink != "" {
		if strings.HasPrefix(nextLink, "/") {
			nextLink = fmt.Sprintf("%s://%s%s", resp.Request.URL.Scheme, resp.Request.URL.Host, nextLink)
		}
		resp, err := getPage(ctx, client, nextLink, headers, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
			return nil, err
		}

		var more []any

		if more, err = getEntries(rawBody, dataKey); err != nil {
			return nil, err
		}
		if body, ok := rawBody.(map[string]any); ok {
			if nextKey, err = getString(body, nextKey); err != nil {
				return nil, err
			}
		}

		if nextKey == "" {
			nextLink, _ = getNextLastLinks(resp.Header.Get("Link"))
		}

		entries = append(entries, more...)
	}
	return entries, nil
}

func init() {
	log.SetFlags(0)
	log.SetPrefix("ERROR: ")
}

func main() {
	var opts struct {
		headers   []string
		countKey  string
		dataKey   string
		lastKey   string
		nextKey   string
		paramPage string
		version   bool
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] URL\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.StringSliceVarP(&opts.headers, "header", "H", nil, "HTTP header (may be specified multiple times)")
	flag.StringVarP(&opts.countKey, "count-key", "C", "", "key to access the count in the JSON response")
	flag.StringVarP(&opts.dataKey, "data-key", "D", "", "key to access the data in the JSON response")
	flag.StringVarP(&opts.nextKey, "next-key", "N", "", "key to access the next page link in the JSON response")
	flag.StringVarP(&opts.lastKey, "last-key", "L", "", "key to access the last page link in the JSON response")
	flag.StringVarP(&opts.paramPage, "param-page", "P", "", "parameter that represents the page number")
	flag.BoolVarP(&opts.version, "version", "", false, "print version and exit")
	flag.Parse()

	if opts.version {
		fmt.Printf("unpage v%s %v %s/%s\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	urlStr := flag.Args()[0]

	debug = os.Getenv("DEBUG") != ""

	headers := map[string]string{
		"Accept":     "application/json",
		"User-Agent": "unpage/" + version,
	}
	for _, header := range opts.headers {
		idx := strings.Index(header, ":")
		if idx <= 0 {
			log.Fatalf("Invalid header: %s", header)
		}
		key := strings.TrimSpace(header[:idx])
		val := strings.TrimSpace(header[idx+1:])
		headers[key] = val
	}

	results, err := unpage(urlStr, headers, opts.paramPage, opts.countKey, opts.dataKey, opts.nextKey, opts.lastKey)
	if err != nil {
		log.Fatal(err)
	}

	output, err := json.Marshal(results)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(output))
}
