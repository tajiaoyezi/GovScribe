package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/gateway"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type Permission string

const PermissionTemplateIngest Permission = "template.ingest"

type Principal struct {
	ID string
}

type Authorizer interface {
	Authorize(context.Context, Principal, Permission) error
}

type IngestStatus string

const (
	IngestStatusSuccess               IngestStatus = "success"
	IngestStatusUnauthorized          IngestStatus = "unauthorized"
	IngestStatusRejected              IngestStatus = "rejected"
	IngestStatusIgnored               IngestStatus = "ignored"
	IngestStatusSkipped               IngestStatus = "skipped"
	IngestStatusDesensitizationFailed IngestStatus = "desensitization_failed"
	IngestStatusStoreFailed           IngestStatus = "store_failed"
)

const (
	ReasonMissingClassification  = "missing_classification"
	ReasonMissingDocumentType    = "missing_document_type"
	ReasonUnauthorized           = "unauthorized"
	ReasonDesensitizationFailed  = "desensitization_failed"
	ReasonAlreadySucceeded       = "already_succeeded"
	ReasonMissingSourceObject    = "missing_source_object"
	ReasonUnsafeAdoptionPayload  = "unsafe_adoption_payload"
	ReasonInvalidAdoptionPayload = "invalid_adoption_payload"
)

var ErrUnauthorized = errors.New("unauthorized corpus operation")

type IngestRequest struct {
	DocumentID       string
	ObjectKey        string
	Content          string
	Classification   string
	DocumentType     string
	DocumentNumber   string
	OrganizationName string
}

type IngestResult struct {
	DocumentID string
	Status     IngestStatus
	Reason     string
	ChunkIDs   []string
	Err        error
}

type BatchIngestRequest struct {
	BatchID   string
	Documents []IngestRequest
}

type BatchCheckpointStore interface {
	AlreadySucceeded(context.Context, string, string) (bool, error)
	RecordResult(context.Context, string, IngestResult) error
}

type AdoptionDecision string

const (
	AdoptionDecisionDirectUse AdoptionDecision = "direct_use"
	AdoptionDecisionMinorEdit AdoptionDecision = "minor_edit"
	AdoptionDecisionMajorEdit AdoptionDecision = "major_edit"
	AdoptionDecisionDiscarded AdoptionDecision = "discarded"
)

type AdoptionSignal struct {
	GeneratedTaskID  string
	AdoptionActionID string
	SourceObjectKey  string
	SourceChunkID    string
	Content          string
	Decision         AdoptionDecision
	Classification   string
	DocumentType     string
}

type AdoptionFeedbackRecord struct {
	GeneratedTaskID    string
	AdoptionActionID   string
	SourceObjectKey    string
	SourceChunkID      string
	Decision           AdoptionDecision
	Classification     string
	DocumentType       string
	IngestedDocumentID string
}

type SanitizedDocument struct {
	Text    string
	Matches []gateway.MatchDetail
}

type DocumentSanitizer interface {
	SanitizeDocument(context.Context, string) (SanitizedDocument, error)
}

type GatewaySanitizer struct {
	Processor gateway.ContextTextProcessor
}

func (s GatewaySanitizer) SanitizeDocument(ctx context.Context, content string) (SanitizedDocument, error) {
	if s.Processor == nil {
		return SanitizedDocument{}, gateway.ErrDesensitizationIncomplete
	}
	_, result, err := s.Processor.SanitizeMessagesContext(ctx, []llm.Message{{Role: llm.RoleUser, Content: content}})
	if err != nil {
		return SanitizedDocument{}, err
	}
	return SanitizedDocument{Text: result.Text, Matches: result.Matches}, nil
}

type StoredDocument struct {
	DocumentID       string
	ObjectKey        string
	Classification   string
	DocumentType     string
	DocumentNumber   string
	OrganizationName string
	IsDeleted        bool
}

type StoredChunk struct {
	ChunkID          string
	DocumentID       string
	ChunkIndex       int
	ContentText      string
	Classification   string
	DocumentType     string
	DocumentNumber   string
	OrganizationName string
	IsDeleted        bool
	DenseVector      []float32
}

type AuthorityStore interface {
	SaveDocument(context.Context, StoredDocument, []StoredChunk, []OutboxEvent, *AdoptionFeedbackRecord) error
}

type ObjectStore interface {
	PutOriginal(context.Context, string, []byte) error
	DeleteOriginal(context.Context, string) error
}

type OutboxEventType string

const (
	OutboxEventIndexChunk     OutboxEventType = "index_chunk"
	OutboxEventSoftDelete     OutboxEventType = "soft_delete_chunk"
	OutboxEventRebuildChunk   OutboxEventType = "rebuild_chunk"
	OutboxEventAdoptionIngest OutboxEventType = "adoption_ingest"
)

type OutboxEvent struct {
	EventID   int64
	EventType OutboxEventType
	ChunkID   string
	Payload   map[string]string
}

type Outbox interface {
	Enqueue(context.Context, OutboxEvent) error
}

type IngestService struct {
	auth      Authorizer
	sanitizer DocumentSanitizer
	store     AuthorityStore
	objects   ObjectStore
}

func NewIngestService(auth Authorizer, sanitizer DocumentSanitizer, store AuthorityStore, objects ObjectStore, _ Outbox) *IngestService {
	return &IngestService{auth: auth, sanitizer: sanitizer, store: store, objects: objects}
}

func (s *IngestService) BatchIngest(ctx context.Context, principal Principal, docs []IngestRequest) []IngestResult {
	results := make([]IngestResult, 0, len(docs))
	for _, doc := range docs {
		results = append(results, s.IngestOne(ctx, principal, doc))
	}
	return results
}

func (s *IngestService) BatchIngestWithCheckpoint(ctx context.Context, principal Principal, req BatchIngestRequest, checkpoint BatchCheckpointStore) []IngestResult {
	if checkpoint == nil {
		return s.BatchIngest(ctx, principal, req.Documents)
	}
	results := make([]IngestResult, 0, len(req.Documents))
	for _, doc := range req.Documents {
		done, err := checkpoint.AlreadySucceeded(ctx, req.BatchID, doc.DocumentID)
		if err != nil {
			results = append(results, IngestResult{
				DocumentID: doc.DocumentID,
				Status:     IngestStatusStoreFailed,
				Err:        err,
			})
			continue
		}
		if done {
			results = append(results, IngestResult{
				DocumentID: doc.DocumentID,
				Status:     IngestStatusSkipped,
				Reason:     ReasonAlreadySucceeded,
			})
			continue
		}
		result := s.IngestOne(ctx, principal, doc)
		if err := checkpoint.RecordResult(ctx, req.BatchID, result); err != nil {
			result.Status = IngestStatusStoreFailed
			result.Err = err
		}
		results = append(results, result)
	}
	return results
}

func (s *IngestService) IngestOne(ctx context.Context, principal Principal, req IngestRequest) IngestResult {
	result := IngestResult{DocumentID: req.DocumentID}
	if err := s.authorize(ctx, principal); err != nil {
		result.Status = IngestStatusUnauthorized
		result.Reason = ReasonUnauthorized
		result.Err = err
		return result
	}
	return s.ingestValidated(ctx, req, nil)
}

func (s *IngestService) IngestAdoptionFeedback(ctx context.Context, signal AdoptionSignal) IngestResult {
	if signal.Decision != AdoptionDecisionDirectUse && signal.Decision != AdoptionDecisionMinorEdit {
		return IngestResult{DocumentID: adoptionDocumentID(signal), Status: IngestStatusIgnored}
	}
	req := IngestRequest{
		DocumentID:     adoptionDocumentID(signal),
		ObjectKey:      adoptionCorpusObjectKey(signal),
		Content:        signal.Content,
		Classification: signal.Classification,
		DocumentType:   signal.DocumentType,
	}
	feedback := &AdoptionFeedbackRecord{
		GeneratedTaskID:    signal.GeneratedTaskID,
		AdoptionActionID:   signal.AdoptionActionID,
		SourceObjectKey:    signal.SourceObjectKey,
		SourceChunkID:      signal.SourceChunkID,
		Decision:           signal.Decision,
		Classification:     signal.Classification,
		DocumentType:       signal.DocumentType,
		IngestedDocumentID: req.DocumentID,
	}
	return s.ingestValidated(ctx, req, feedback)
}

func (s *IngestService) ingestValidated(ctx context.Context, req IngestRequest, feedback *AdoptionFeedbackRecord) IngestResult {
	result := IngestResult{DocumentID: req.DocumentID}
	if strings.TrimSpace(req.Classification) == "" {
		result.Status = IngestStatusRejected
		result.Reason = ReasonMissingClassification
		return result
	}
	if strings.TrimSpace(req.DocumentType) == "" {
		result.Status = IngestStatusRejected
		result.Reason = ReasonMissingDocumentType
		return result
	}
	if s.sanitizer == nil {
		result.Status = IngestStatusDesensitizationFailed
		result.Reason = ReasonDesensitizationFailed
		result.Err = gateway.ErrDesensitizationIncomplete
		return result
	}

	sanitized, err := s.sanitizer.SanitizeDocument(ctx, req.Content)
	if err != nil {
		result.Status = IngestStatusDesensitizationFailed
		result.Reason = ReasonDesensitizationFailed
		result.Err = err
		return result
	}

	doc := StoredDocument{
		DocumentID:       req.DocumentID,
		ObjectKey:        req.ObjectKey,
		Classification:   req.Classification,
		DocumentType:     req.DocumentType,
		DocumentNumber:   firstNonEmpty(req.DocumentNumber, firstMatch(sanitized.Matches, gateway.EntityTypeDocumentNumber)),
		OrganizationName: firstNonEmpty(req.OrganizationName, firstMatch(sanitized.Matches, gateway.EntityTypeOrganization)),
	}
	chunk := StoredChunk{
		ChunkID:          chunkID(req.DocumentID, 0),
		DocumentID:       req.DocumentID,
		ChunkIndex:       0,
		ContentText:      sanitized.Text,
		Classification:   doc.Classification,
		DocumentType:     doc.DocumentType,
		DocumentNumber:   doc.DocumentNumber,
		OrganizationName: doc.OrganizationName,
		IsDeleted:        false,
	}
	if s.store == nil || s.objects == nil {
		result.Status = IngestStatusStoreFailed
		result.Err = errors.New("corpus stores are required")
		return result
	}
	if err := s.objects.PutOriginal(ctx, req.ObjectKey, []byte(sanitized.Text)); err != nil {
		result.Status = IngestStatusStoreFailed
		result.Err = err
		return result
	}
	indexEvent := OutboxEvent{
		EventType: OutboxEventIndexChunk,
		ChunkID:   chunk.ChunkID,
		Payload: map[string]string{
			"document_id": req.DocumentID,
		},
	}
	var transactionalFeedback *AdoptionFeedbackRecord
	if feedback != nil {
		copied := *feedback
		copied.IngestedDocumentID = req.DocumentID
		transactionalFeedback = &copied
	}
	if err := s.store.SaveDocument(ctx, doc, []StoredChunk{chunk}, []OutboxEvent{indexEvent}, transactionalFeedback); err != nil {
		if compensationErr := s.compensateObjectWrite(ctx, req.ObjectKey); compensationErr != nil {
			err = fmt.Errorf("%w; object compensation failed: %v", err, compensationErr)
		}
		result.Status = IngestStatusStoreFailed
		result.Err = err
		return result
	}
	result.Status = IngestStatusSuccess
	result.ChunkIDs = []string{chunk.ChunkID}
	return result
}

func (s *IngestService) compensateObjectWrite(ctx context.Context, objectKey string) error {
	return s.objects.DeleteOriginal(ctx, objectKey)
}

func (s *IngestService) authorize(ctx context.Context, principal Principal) error {
	if s.auth == nil {
		return ErrUnauthorized
	}
	if err := s.auth.Authorize(ctx, principal, PermissionTemplateIngest); err != nil {
		return ErrUnauthorized
	}
	return nil
}

func firstMatch(matches []gateway.MatchDetail, entityType gateway.EntityType) string {
	for _, match := range matches {
		if match.Type == entityType && strings.TrimSpace(match.Text) != "" {
			return match.Text
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func chunkID(documentID string, index int) string {
	return fmt.Sprintf("%s:%04d", documentID, index)
}

func adoptionDocumentID(signal AdoptionSignal) string {
	return "adoption:" + firstNonEmpty(signal.GeneratedTaskID, "unknown-task") + ":" + firstNonEmpty(signal.AdoptionActionID, "unknown-action")
}

func adoptionCorpusObjectKey(signal AdoptionSignal) string {
	return "corpus/adoptions/" +
		objectKeySegment(firstNonEmpty(signal.GeneratedTaskID, "unknown-task")) +
		"/" +
		objectKeySegment(firstNonEmpty(signal.AdoptionActionID, "unknown-action")) +
		".txt"
}

func objectKeySegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_")
	return replacer.Replace(value)
}

type AdoptionOutboxConsumer struct {
	ingest *IngestService
}

func NewAdoptionOutboxConsumer(ingest *IngestService) *AdoptionOutboxConsumer {
	return &AdoptionOutboxConsumer{ingest: ingest}
}

func (c *AdoptionOutboxConsumer) Consume(ctx context.Context, event OutboxEvent) IngestResult {
	if c == nil || c.ingest == nil || event.EventType != OutboxEventAdoptionIngest {
		return IngestResult{Status: IngestStatusIgnored}
	}
	payload := event.Payload
	if _, ok := payload["content"]; ok {
		return rejectAdoptionPayload(payload, ReasonUnsafeAdoptionPayload, errors.New("adoption outbox payload must not include content"))
	}
	if err := validateAdoptionPayload(payload); err != nil {
		reason := ReasonInvalidAdoptionPayload
		if strings.TrimSpace(payload["source_object_key"]) == "" {
			reason = ReasonMissingSourceObject
		}
		return rejectAdoptionPayload(payload, reason, err)
	}
	sourceObjectKey := strings.TrimSpace(payload["source_object_key"])
	reader, ok := c.ingest.objects.(OriginalReader)
	if !ok {
		return IngestResult{
			DocumentID: adoptionDocumentID(AdoptionSignal{
				GeneratedTaskID:  payload["generated_task_id"],
				AdoptionActionID: payload["adoption_action_id"],
			}),
			Status: IngestStatusStoreFailed,
			Err:    errors.New("adoption source object reader is required"),
		}
	}
	content, err := reader.GetOriginal(ctx, sourceObjectKey)
	if err != nil {
		return IngestResult{
			DocumentID: adoptionDocumentID(AdoptionSignal{
				GeneratedTaskID:  payload["generated_task_id"],
				AdoptionActionID: payload["adoption_action_id"],
			}),
			Status: IngestStatusStoreFailed,
			Err:    err,
		}
	}
	return c.ingest.IngestAdoptionFeedback(ctx, AdoptionSignal{
		GeneratedTaskID:  payload["generated_task_id"],
		AdoptionActionID: payload["adoption_action_id"],
		SourceObjectKey:  sourceObjectKey,
		SourceChunkID:    payload["source_chunk_id"],
		Content:          string(content),
		Decision:         AdoptionDecision(payload["decision"]),
		Classification:   payload["classification"],
		DocumentType:     payload["document_type"],
	})
}

func validateAdoptionPayload(payload map[string]string) error {
	allowed := map[string]struct{}{
		"generated_task_id":  {},
		"adoption_action_id": {},
		"source_object_key":  {},
		"source_chunk_id":    {},
		"decision":           {},
		"classification":     {},
		"document_type":      {},
	}
	for key := range payload {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unexpected adoption payload key %q", key)
		}
	}
	required := []string{
		"generated_task_id",
		"adoption_action_id",
		"source_object_key",
		"decision",
		"classification",
		"document_type",
	}
	for _, key := range required {
		if strings.TrimSpace(payload[key]) == "" {
			return fmt.Errorf("missing adoption payload key %q", key)
		}
	}
	switch AdoptionDecision(payload["decision"]) {
	case AdoptionDecisionDirectUse, AdoptionDecisionMinorEdit, AdoptionDecisionMajorEdit, AdoptionDecisionDiscarded:
		return nil
	default:
		return fmt.Errorf("invalid adoption decision %q", payload["decision"])
	}
}

func rejectAdoptionPayload(payload map[string]string, reason string, err error) IngestResult {
	return IngestResult{
		DocumentID: adoptionDocumentID(AdoptionSignal{
			GeneratedTaskID:  payload["generated_task_id"],
			AdoptionActionID: payload["adoption_action_id"],
		}),
		Status: IngestStatusRejected,
		Reason: reason,
		Err:    err,
	}
}

type IdentifierStoragePolicy struct {
	ProtectDocumentNumber   bool
	ProtectOrganizationName bool
	HashSalt                string
}

func ProjectChunkForDerivedIndex(chunk StoredChunk, policy IdentifierStoragePolicy) StoredChunk {
	projected := chunk
	if policy.ProtectDocumentNumber {
		projected.DocumentNumber = deterministicIdentifierToken("document_number", chunk.DocumentNumber, policy.HashSalt)
	}
	if policy.ProtectOrganizationName {
		projected.OrganizationName = deterministicIdentifierToken("organization_name", chunk.OrganizationName, policy.HashSalt)
	}
	return projected
}

func deterministicIdentifierToken(kind, value, salt string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + salt + "\x00" + value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
