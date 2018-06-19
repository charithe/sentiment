package sentiment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
