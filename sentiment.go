package sentiment

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	language "cloud.google.com/go/language/apiv1"
	"github.com/allegro/bigcache"
	"github.com/gogo/protobuf/proto"
	"go.uber.org/zap"
	"google.golang.org/api/option"
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

// Service implements the sentiment analysis API extension
type Service struct {
	conf   *config
	client *language.Client
	cache  *bigcache.BigCache
}

// NewService creates a new sentiment analysis API extension with the given API key and options
func NewService(apiKey string, opts ...Option) (*Service, error) {
	conf := &config{
		requestTimeout: 1 * time.Second,
		cacheMaxSizeMB: 64,
		cacheEntryTTL:  10 * time.Minute,
	}

	for _, opt := range opts {
		opt(conf)
	}

	client, err := language.NewClient(context.Background(), option.WithAPIKey(apiKey))
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

// Handle is the entrypoint into the service
func (svc *Service) Handle(ctx context.Context, input string, sort SortOrder, limit int) (Response, error) {
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
	if len(result.Sentences) < limit {
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
