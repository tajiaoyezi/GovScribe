package draft

import (
	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type HighFreqDraftRequestInput struct {
	Scenario  doctype.ScenarioContext
	ActorID   string
	RequestID string
}

type HighFreqDraftRequest struct {
	Context   C05ScenarioContext
	ActorID   string
	RequestID string
}

type StructuredDraftBody struct {
	Title      string
	Salutation string
	Recipient  string
	Paragraphs []string
	Signature  string
}

type HighFreqDraftResponseContext struct {
	RequestID            string
	TargetCapability     doctype.TargetCapability
	Doctype              string
	Subtype              string
	Direction            doctype.WritingDirection
	Confidence           float64
	ContentSecurityLevel llm.ContentSecurityLevel
}

type HighFreqDraftResponse struct {
	Body    StructuredDraftBody
	Context HighFreqDraftResponseContext
}

func NewHighFreqDraftRequest(input HighFreqDraftRequestInput) (HighFreqDraftRequest, error) {
	context, err := ConsumeC06ScenarioContext(input.Scenario)
	if err != nil {
		return HighFreqDraftRequest{}, err
	}
	return HighFreqDraftRequest{
		Context:   context,
		ActorID:   input.ActorID,
		RequestID: input.RequestID,
	}, nil
}

func NewHighFreqDraftResponse(request HighFreqDraftRequest, body StructuredDraftBody) HighFreqDraftResponse {
	return HighFreqDraftResponse{
		Body: copyStructuredDraftBody(body),
		Context: HighFreqDraftResponseContext{
			RequestID:            request.RequestID,
			TargetCapability:     request.Context.TargetCapability,
			Doctype:              request.Context.Doctype,
			Subtype:              request.Context.Subtype,
			Direction:            request.Context.Direction,
			Confidence:           request.Context.Confidence,
			ContentSecurityLevel: request.Context.ContentSecurityLevel,
		},
	}
}

func copyStructuredDraftBody(body StructuredDraftBody) StructuredDraftBody {
	body.Paragraphs = append([]string(nil), body.Paragraphs...)
	return body
}
