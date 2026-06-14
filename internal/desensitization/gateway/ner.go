package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrNERUnavailable            = errors.New("ner unavailable")
	ErrNERCircuitOpen            = errors.New("ner circuit open")
	ErrDesensitizationIncomplete = errors.New("desensitization incomplete")
)

type NERClient interface {
	Recognize(context.Context, string) ([]Hit, error)
}

type HTTPNERClient struct {
	endpoint   string
	httpClient *http.Client
}

func NewHTTPNERClient(endpoint string, httpClient *http.Client) HTTPNERClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
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

	var decoded nerResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrNERUnavailable, err)
	}
	hits := make([]Hit, 0, len(decoded.Entities))
	for _, entity := range decoded.Entities {
		if entity.Start < 0 || entity.End > len(text) || entity.Start >= entity.End {
			return nil, fmt.Errorf("%w: invalid span %d-%d", ErrNERUnavailable, entity.Start, entity.End)
		}
		hits = append(hits, Hit{
			Start:  entity.Start,
			End:    entity.End,
			Text:   text[entity.Start:entity.End],
			Type:   entityTypeFromNER(entity.Type),
			Source: SourceNER,
		})
	}
	return hits, nil
}

type nerRequest struct {
	Text string `json:"text"`
}

type nerResponse struct {
	Entities []nerEntity `json:"entities"`
}

type nerEntity struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Type  string `json:"type"`
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

func isNERUnavailable(err error) bool {
	return errors.Is(err, ErrNERUnavailable) || errors.Is(err, ErrNERCircuitOpen)
}
