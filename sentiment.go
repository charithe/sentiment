package sentiment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	language "cloud.google.com/go/language/apiv1"
	"github.com/allegro/bigcache"
	"github.com/gogo/protobuf/proto"
	gax "github.com/googleapis/gax-go"
	"go.uber.org/zap"
	languagepb "google.golang.org/genproto/googleapis/cloud/language/v1"
)

// Option defines a configuration option that can be set on the sentiment service
type Option func(c *config)

// WithRequestTimeout sets the timeout for each request to the sentiment service
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *config) {
		c.requestTimeout = timeout
	}
}

// WithCacheMaxSizeMB sets the maximum amount of memory to allocate for the cache
func WithCacheMaxSizeMB(maxSize int) Option {
	return func(c *config) {
		c.cacheMaxSizeMB = maxSize
	}
}

// WithCacheEntryTTL sets the life time of a cache entry
func WithCacheEntryTTL(ttl time.Duration) Option {
	return func(c *config) {
		c.cacheEntryTTL = ttl
	}
}

type config struct {
	requestTimeout time.Duration
	cacheMaxSizeMB int
	cacheEntryTTL  time.Duration
}

// SortOrder is an enum defining the sort order of results
type SortOrder int

const (
	// Ascending order
	Ascending SortOrder = iota
	// Descending order
	Descending
)

// Response is the expected output type from the service
type Response []map[string]float32

type input struct {
	Content string `json:"content"`
}

type languageClient interface {
	AnalyzeSentiment(context.Context, *languagepb.AnalyzeSentimentRequest, ...gax.CallOption) (*languagepb.AnalyzeSentimentResponse, error)
	Close() error
}

// Service implements the sentiment analysis API extension
type Service struct {
	conf   *config
	client languageClient
	cache  *bigcache.BigCache
}

// NewService creates a new sentiment analysis API extension with the given options
func NewService(opts ...Option) (*Service, error) {
	conf := &config{
		requestTimeout: 1 * time.Second,
		cacheMaxSizeMB: 64,
		cacheEntryTTL:  10 * time.Minute,
	}

	for _, opt := range opts {
		opt(conf)
	}

	client, err := language.NewClient(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to create Google language client: %+v", err)
	}

	cacheConf := bigcache.DefaultConfig(conf.cacheEntryTTL)
	cacheConf.HardMaxCacheSize = conf.cacheMaxSizeMB
	cache, err := bigcache.NewBigCache(cacheConf)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %+v", err)
	}

	return &Service{
		conf:   conf,
		client: client,
		cache:  cache,
	}, nil
}

// Close terminates the service
func (svc *Service) Close() error {
	if svc.client != nil {
		return svc.client.Close()
	}
	return nil
}

// RESTHandler implements the http Handler interface to provide sentiment analysis services
func (svc *Service) RESTHandler() http.Handler {
	mux := http.NewServeMux()
	// api handler
	mux.HandleFunc("/api", svc.handleHTTPRequest)
	// health handler for Kubernetes liveness check
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer func() {
				io.Copy(ioutil.Discard, r.Body)
				r.Body.Close()
			}()
		}

		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func (svc *Service) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer func() {
			io.Copy(ioutil.Discard, r.Body)
			r.Body.Close()
		}()
	}

	if r.Method != http.MethodPost {
		zap.S().Warnw("Bad request method")
		http.Error(w, "Bad request method", http.StatusMethodNotAllowed)
		return
	}

	var inp input
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		zap.S().Errorw("Failed to parse request body", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	params := r.URL.Query()

	sortOrder := Ascending
	if so := params.Get("order"); so != "" && strings.ToLower(so) == "desc" {
		sortOrder = Descending
	}

	limit := -1
	if l := params.Get("limit"); l != "" {
		limitVal, err := strconv.Atoi(l)
		if err != nil {
			zap.S().Warnw("Invalid limit parameter", "limit", l, "error", err)
		} else {
			limit = limitVal
		}
	}

	resp, err := svc.ProcessSentiment(r.Context(), inp.Content, sortOrder, limit)
	if err != nil {
		zap.S().Errorw("Request failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.S().Errorw("Failed to marshal response", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
}

// ProcessSentiment implements the logic of processing a sentiment analysis request
func (svc *Service) ProcessSentiment(ctx context.Context, input string, sort SortOrder, limit int) (Response, error) {
	if err := ctx.Err(); err != nil {
		zap.S().Warnw("Context cancelled", "error", err, "input", input)
		return nil, err
	}

	sanitizedInput := strings.ToLower(strings.TrimSpace(input))

	// if the result is already in the cache, skip the remote API call
	if cachedResult := svc.getCachedResult(sanitizedInput); cachedResult != nil {
		return svc.processAPIResult(ctx, cachedResult, sort, limit)
	}

	// make the remote API call
	resp, err := svc.client.AnalyzeSentiment(ctx, &languagepb.AnalyzeSentimentRequest{
		Document: &languagepb.Document{
			Source: &languagepb.Document_Content{
				Content: input,
			},
			Type: languagepb.Document_PLAIN_TEXT,
		},
	})

	if err != nil {
		zap.S().Errorw("Remote API call failure", "error", err, "input", input)
		return nil, err
	}

	// save the result in the cache
	if respBytes, err := proto.Marshal(resp); err == nil {
		svc.cache.Set(sanitizedInput, respBytes)
	}

	return svc.processAPIResult(ctx, resp, sort, limit)
}

func (svc *Service) getCachedResult(key string) *languagepb.AnalyzeSentimentResponse {
	entry, err := svc.cache.Get(key)
	if err != nil {
		return nil
	}

	var result languagepb.AnalyzeSentimentResponse
	if err = proto.Unmarshal(entry, &result); err != nil {
		return nil
	}

	return &result
}

func (svc *Service) processAPIResult(ctx context.Context, result *languagepb.AnalyzeSentimentResponse, sortOrder SortOrder, limit int) (Response, error) {
	if err := ctx.Err(); err != nil {
		zap.S().Errorw("Context cancelled", "error", err)
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	switch sortOrder {
	case Ascending:
		sort.Sort(byScoreAsc(result.Sentences))
	case Descending:
		sort.Sort(byScoreDesc(result.Sentences))
	}

	arraySize := limit
	if arraySize < 0 {
		arraySize = len(result.Sentences)
	} else if len(result.Sentences) < arraySize {
		arraySize = len(result.Sentences)
	}

	resp := make([]map[string]float32, arraySize)
	for i := 0; i < arraySize; i++ {
		resp[i] = map[string]float32{result.Sentences[i].Text.Content: result.Sentences[i].Sentiment.Score}
	}

	return Response(resp), nil
}

// Sort interface implementation for sorting entities by ascending order of sentiment score
type byScoreAsc []*languagepb.Sentence

func (b byScoreAsc) Len() int { return len(b) }

func (b byScoreAsc) Swap(i, j int) { b[i], b[j] = b[j], b[i] }

func (b byScoreAsc) Less(i, j int) bool { return b[i].Sentiment.Score < b[j].Sentiment.Score }

// Sort interface implementation for sorting entities by descending order of sentiment score
type byScoreDesc []*languagepb.Sentence

func (b byScoreDesc) Len() int { return len(b) }

func (b byScoreDesc) Swap(i, j int) { b[i], b[j] = b[j], b[i] }

func (b byScoreDesc) Less(i, j int) bool { return b[i].Sentiment.Score > b[j].Sentiment.Score }
