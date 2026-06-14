package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"
)

var (
	ErrNERUnavailable            = errors.New("ner unavailable")
	ErrNERCircuitOpen            = errors.New("ner circuit open")
	ErrDesensitizationIncomplete = errors.New("desensitization incomplete")
)

const defaultNERHTTPTimeout = 5 * time.Second

type NERClient interface {
	Recognize(context.Context, string) ([]Hit, error)
}

type HTTPNERClient struct {
	endpoint   string
	httpClient *http.Client
}

func NewHTTPNERClient(endpoint string, httpClient *http.Client) HTTPNERClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultNERHTTPTimeout}
	} else if httpClient.Timeout <= 0 {
		copied := *httpClient
		copied.Timeout = defaultNERHTTPTimeout
		httpClient = &copied
	}
	return HTTPNERClient{endpoint: endpoint, httpClient: httpClient}
}

func (c HTTPNERClient) Recognize(ctx context.Context, text string) ([]Hit, error) {
	body, err := json.Marshal(nerRequest{Text: text})
	if err != nil {
		return nil, fmt.Errorf("%w: encode request: %v", ErrNERUnavailable, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrNERUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNERUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status %d", ErrNERUnavailable, resp.StatusCode)
	}

	var decoded rawNERResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrNERUnavailable, err)
	}
	if len(decoded.Entities) == 0 || string(decoded.Entities) == "null" {
		return nil, fmt.Errorf("%w: missing entities", ErrNERUnavailable)
	}
	var entities []nerEntity
	if err := json.Unmarshal(decoded.Entities, &entities); err != nil {
		return nil, fmt.Errorf("%w: decode entities: %v", ErrNERUnavailable, err)
	}
	if entities == nil {
		return nil, fmt.Errorf("%w: missing entities", ErrNERUnavailable)
	}

	hits := make([]Hit, 0, len(entities))
	for _, entity := range entities {
		if entity.Start == nil || entity.End == nil || entity.Type == nil || *entity.Type == "" {
			return nil, fmt.Errorf("%w: incomplete entity", ErrNERUnavailable)
		}
		start, end := *entity.Start, *entity.End
		if !validNERSpan(text, start, end) {
			return nil, fmt.Errorf("%w: invalid span %d-%d", ErrNERUnavailable, start, end)
		}
		hits = append(hits, Hit{
			Start:  start,
			End:    end,
			Text:   text[start:end],
			Type:   entityTypeFromNER(*entity.Type),
			Source: SourceNER,
		})
	}
	return hits, nil
}

type nerRequest struct {
	Text string `json:"text"`
}

type rawNERResponse struct {
	Entities json.RawMessage `json:"entities"`
}

type nerEntity struct {
	Start *int    `json:"start"`
	End   *int    `json:"end"`
	Type  *string `json:"type"`
}

func entityTypeFromNER(entityType string) EntityType {
	switch entityType {
	case "person":
		return EntityTypePerson
	case "organization", "org":
		return EntityTypeOrganization
	case "project_code":
		return EntityTypeProjectCode
	default:
		return EntityTypeNamedEntity
	}
}

func validNERSpan(text string, start, end int) bool {
	if start < 0 || end > len(text) || start >= end {
		return false
	}
	return isUTF8Boundary(text, start) &&
		isUTF8Boundary(text, end) &&
		utf8.ValidString(text[start:end])
}

func isUTF8Boundary(text string, offset int) bool {
	if offset == 0 || offset == len(text) {
		return true
	}
	if offset < 0 || offset > len(text) {
		return false
	}
	return utf8.RuneStart(text[offset])
}

func isNERUnavailable(err error) bool {
	return errors.Is(err, ErrNERUnavailable) || errors.Is(err, ErrNERCircuitOpen)
}
