package sentiment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/allegro/bigcache"
	gax "github.com/googleapis/gax-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	languagepb "google.golang.org/genproto/googleapis/cloud/language/v1"
)

func TestProcessAPIResult(t *testing.T) {
	apiResult := &languagepb.AnalyzeSentimentResponse{
		Sentences: []*languagepb.Sentence{
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word1"},
				Sentiment: &languagepb.Sentiment{Magnitude: 3.0, Score: 0.8},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word2"},
				Sentiment: &languagepb.Sentiment{Magnitude: 1.0, Score: 0.8},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word3"},
				Sentiment: &languagepb.Sentiment{Magnitude: 2.2, Score: 0.2},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word4"},
				Sentiment: &languagepb.Sentiment{Magnitude: 1.0, Score: -0.8},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word5"},
				Sentiment: &languagepb.Sentiment{Magnitude: 1.0, Score: 0.0},
			},
		},
	}

	svc := &Service{}

	testCases := []struct {
		name             string
		apiResult        *languagepb.AnalyzeSentimentResponse
		sortOrder        SortOrder
		limit            int
		expectedResponse Response
		expectedError    error
	}{
		{
			name:      "sort_ascending",
			apiResult: apiResult,
			sortOrder: Ascending,
			limit:     3,
			expectedResponse: Response([]map[string]float32{
				map[string]float32{"word4": -0.8},
				map[string]float32{"word5": 0.0},
				map[string]float32{"word3": 0.2},
			}),
		},
		{
			name:      "sort_descending",
			apiResult: apiResult,
			sortOrder: Descending,
			limit:     3,
			expectedResponse: Response([]map[string]float32{
				map[string]float32{"word1": 0.8},
				map[string]float32{"word2": 0.8},
				map[string]float32{"word3": 0.2},
			}),
		},
		{
			name:      "limit_larger_than_num_results",
			apiResult: apiResult,
			sortOrder: Descending,
			limit:     6,
			expectedResponse: Response([]map[string]float32{
				map[string]float32{"word1": 0.8},
				map[string]float32{"word2": 0.8},
				map[string]float32{"word3": 0.2},
				map[string]float32{"word5": 0.0},
				map[string]float32{"word4": -0.8},
			}),
		},
		{
			name:      "negative_limit",
			apiResult: apiResult,
			sortOrder: Descending,
			limit:     -1,
			expectedResponse: Response([]map[string]float32{
				map[string]float32{"word1": 0.8},
				map[string]float32{"word2": 0.8},
				map[string]float32{"word3": 0.2},
				map[string]float32{"word5": 0.0},
				map[string]float32{"word4": -0.8},
			}),
		},
		{
			name:      "nil_api_result",
			sortOrder: Descending,
			limit:     3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.processAPIResult(context.Background(), tc.apiResult, tc.sortOrder, tc.limit)
			if tc.expectedError == nil {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedResponse, resp)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

type mockLanguageClient struct {
	mock.Mock
}

func (m *mockLanguageClient) AnalyzeSentiment(ctx context.Context, req *languagepb.AnalyzeSentimentRequest, opts ...gax.CallOption) (*languagepb.AnalyzeSentimentResponse, error) {
	args := m.MethodCalled("AnalyzeSentiment", ctx, req, opts)
	if resp := args.Get(0); resp != nil {
		return resp.(*languagepb.AnalyzeSentimentResponse), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *mockLanguageClient) Close() error {
	args := m.MethodCalled("Close")
	return args.Error(0)
}

func createMocks(t *testing.T) (*mockLanguageClient, *Service) {
	mockClient := &mockLanguageClient{}
	conf := &config{requestTimeout: 1 * time.Second}
	cache, err := bigcache.NewBigCache(bigcache.DefaultConfig(10 * time.Minute))
	assert.NoError(t, err)

	svc := &Service{conf: conf, client: mockClient, cache: cache}

	return mockClient, svc
}

func TestServiceCall(t *testing.T) {
	expectedRequest := &languagepb.AnalyzeSentimentRequest{
		Document: &languagepb.Document{
			Source: &languagepb.Document_Content{
				Content: "word1 word2 word3 word4 word5",
			},
			Type: languagepb.Document_PLAIN_TEXT,
		},
	}

	expectedResponse := &languagepb.AnalyzeSentimentResponse{
		Sentences: []*languagepb.Sentence{
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word1"},
				Sentiment: &languagepb.Sentiment{Magnitude: 3.0, Score: 0.8},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word2"},
				Sentiment: &languagepb.Sentiment{Magnitude: 1.0, Score: 0.8},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word3"},
				Sentiment: &languagepb.Sentiment{Magnitude: 2.2, Score: 0.2},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word4"},
				Sentiment: &languagepb.Sentiment{Magnitude: 1.0, Score: -0.8},
			},
			&languagepb.Sentence{
				Text:      &languagepb.TextSpan{Content: "word5"},
				Sentiment: &languagepb.Sentiment{Magnitude: 1.0, Score: 0.0},
			},
		},
	}

	t.Run("process_sentiment_success", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(expectedResponse, nil)

		expectedResult := Response([]map[string]float32{
			map[string]float32{"word4": -0.8},
			map[string]float32{"word5": 0.0},
			map[string]float32{"word3": 0.2},
		})

		resp, err := svc.ProcessSentiment(context.Background(), "word1 word2 word3 word4 word5", Ascending, 3)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, expectedResult, resp)
		mockClient.AssertExpectations(t)
	})

	t.Run("process_sentiment_error", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(nil, fmt.Errorf("error"))

		_, err := svc.ProcessSentiment(context.Background(), "word1 word2 word3 word4 word5", Ascending, 3)
		assert.Error(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("http_request_default_limit", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(expectedResponse, nil)

		responseRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api?order=desc", strings.NewReader(`{"content":"word1 word2 word3 word4 word5"}`))
		svc.handleHTTPRequest(responseRecorder, request)
		result := responseRecorder.Result()

		assert.Equal(t, http.StatusOK, result.StatusCode)

		var output Response
		assert.NoError(t, json.NewDecoder(result.Body).Decode(&output))

		expectedOutput := Response([]map[string]float32{
			map[string]float32{"word1": 0.8},
			map[string]float32{"word2": 0.8},
			map[string]float32{"word3": 0.2},
			map[string]float32{"word5": 0.0},
			map[string]float32{"word4": -0.8},
		})

		assert.Equal(t, expectedOutput, output)
	})

	t.Run("http_request_explicit_limit", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(expectedResponse, nil)

		responseRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api?limit=3", strings.NewReader(`{"content":"word1 word2 word3 word4 word5"}`))
		svc.handleHTTPRequest(responseRecorder, request)
		result := responseRecorder.Result()

		assert.Equal(t, http.StatusOK, result.StatusCode)

		var output Response
		assert.NoError(t, json.NewDecoder(result.Body).Decode(&output))

		expectedOutput := Response([]map[string]float32{
			map[string]float32{"word4": -0.8},
			map[string]float32{"word5": 0.0},
			map[string]float32{"word3": 0.2},
		})

		assert.Equal(t, expectedOutput, output)
	})

	t.Run("http_request_invalid_limit", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(expectedResponse, nil)

		responseRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api?limit=xxx", strings.NewReader(`{"content":"word1 word2 word3 word4 word5"}`))
		svc.handleHTTPRequest(responseRecorder, request)
		result := responseRecorder.Result()

		assert.Equal(t, http.StatusOK, result.StatusCode)

		var output Response
		assert.NoError(t, json.NewDecoder(result.Body).Decode(&output))

		expectedOutput := Response([]map[string]float32{
			map[string]float32{"word4": -0.8},
			map[string]float32{"word5": 0.0},
			map[string]float32{"word3": 0.2},
			map[string]float32{"word1": 0.8},
			map[string]float32{"word2": 0.8},
		})

		assert.Equal(t, expectedOutput, output)
	})

	t.Run("http_request_invalid_method", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(expectedResponse, nil)

		responseRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api", strings.NewReader(`{"content":"word1 word2 word3 word4 word5"}`))
		svc.handleHTTPRequest(responseRecorder, request)
		result := responseRecorder.Result()

		assert.Equal(t, http.StatusMethodNotAllowed, result.StatusCode)
	})

	t.Run("http_request_remote_failure", func(t *testing.T) {
		mockClient, svc := createMocks(t)
		mockClient.On("AnalyzeSentiment", mock.Anything, expectedRequest, mock.Anything).Return(nil, fmt.Errorf("error"))

		responseRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(`{"content":"word1 word2 word3 word4 word5"}`))
		svc.handleHTTPRequest(responseRecorder, request)
		result := responseRecorder.Result()

		assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
	})
}
