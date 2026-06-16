package corpus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/gateway"
)

func TestIngestOneRequiresTemplateIngestBeforePipeline(t *testing.T) {
	auth := &recordingAuthorizer{err: errors.New("deny")}
	sanitizer := &recordingSanitizer{}
	store := &recordingAuthorityStore{}
	objects := &recordingObjectStore{}
	outbox := &recordingOutbox{}
	svc := NewIngestService(auth, sanitizer, store, objects, outbox)

	result := svc.IngestOne(context.Background(), Principal{ID: "u1"}, IngestRequest{
		DocumentID:     "doc-1",
		ObjectKey:      "templates/doc-1.docx",
		Content:        "正文",
		Classification: "internal",
		DocumentType:   "通知",
	})

	if result.Status != IngestStatusUnauthorized {
		t.Fatalf("status = %s, want unauthorized", result.Status)
	}
	if auth.lastPermission != PermissionTemplateIngest {
		t.Fatalf("permission = %s, want template.ingest", auth.lastPermission)
	}
	if sanitizer.called || store.saved || objects.saved || outbox.saved {
		t.Fatalf("pipeline ran after authorization denial")
	}
}

func TestIngestOneRejectsMissingClassificationBeforeSanitization(t *testing.T) {
	sanitizer := &recordingSanitizer{}
	store := &recordingAuthorityStore{}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, &recordingObjectStore{}, &recordingOutbox{})

	result := svc.IngestOne(context.Background(), Principal{ID: "u1"}, IngestRequest{
		DocumentID:   "doc-1",
		ObjectKey:    "templates/doc-1.docx",
		Content:      "正文",
		DocumentType: "通知",
	})

	if result.Status != IngestStatusRejected || result.Reason != ReasonMissingClassification {
		t.Fatalf("result = %#v, want missing classification rejection", result)
	}
	if sanitizer.called || store.saved {
		t.Fatalf("pipeline ran for missing classification")
	}
}

func TestIngestOneFailClosedWhenDesensitizationFails(t *testing.T) {
	sanitizer := &recordingSanitizer{err: gateway.ErrDesensitizationIncomplete}
	store := &recordingAuthorityStore{}
	objects := &recordingObjectStore{}
	outbox := &recordingOutbox{}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, outbox)

	result := svc.IngestOne(context.Background(), Principal{ID: "u1"}, validIngestRequest("doc-1"))

	if result.Status != IngestStatusDesensitizationFailed {
		t.Fatalf("status = %s, want desensitization failed", result.Status)
	}
	if store.saved || objects.saved || outbox.saved {
		t.Fatalf("fail-closed violated: store=%v objects=%v outbox=%v", store.saved, objects.saved, outbox.saved)
	}
}

func TestIngestOneWritesSanitizedChunkStructuredFieldsAndOutbox(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{
		Text: "〖ORGANIZATION_01〗印发了〖DOCUMENT_NUMBER_01〗。",
		Matches: []gateway.MatchDetail{
			{Text: "XX市人民政府办公厅", Type: gateway.EntityTypeOrganization, Source: gateway.SourceDictionary},
			{Text: "〔2024〕12号", Type: gateway.EntityTypeDocumentNumber, Source: gateway.SourceRegex},
		},
	}}
	store := &recordingAuthorityStore{}
	objects := &recordingObjectStore{}
	outbox := &recordingOutbox{}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, outbox)

	result := svc.IngestOne(context.Background(), Principal{ID: "u1"}, validIngestRequest("doc-1"))

	if result.Status != IngestStatusSuccess {
		t.Fatalf("result = %#v, want success", result)
	}
	if store.document.DocumentNumber != "〔2024〕12号" || store.document.OrganizationName != "XX市人民政府办公厅" {
		t.Fatalf("structured fields = %#v", store.document)
	}
	if len(store.chunks) != 1 || store.chunks[0].ContentText != sanitizer.result.Text {
		t.Fatalf("chunks = %#v", store.chunks)
	}
	if !objects.saved || objects.objectKey != "templates/doc-1.docx" || string(objects.content) != sanitizer.result.Text {
		t.Fatalf("object store write = %#v", objects)
	}
	if outbox.saved {
		t.Fatalf("ingest must not enqueue outbox outside authority transaction: %#v", outbox.event)
	}
	if len(store.events) != 1 || store.events[0].EventType != OutboxEventIndexChunk || store.events[0].ChunkID != store.chunks[0].ChunkID {
		t.Fatalf("transactional outbox events = %#v", store.events)
	}
}

func TestIngestOneCompensatesObjectWriteWhenAuthorityTransactionFails(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏正文"}}
	store := &recordingAuthorityStore{saveErr: errors.New("postgres down")}
	objects := &recordingObjectStore{}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, &recordingOutbox{})

	result := svc.IngestOne(context.Background(), Principal{ID: "u1"}, validIngestRequest("doc-1"))

	if result.Status != IngestStatusStoreFailed {
		t.Fatalf("result = %#v, want store failed", result)
	}
	if !objects.saved || objects.deletedKey != "templates/doc-1.docx" {
		t.Fatalf("object compensation saved=%v deleted=%q", objects.saved, objects.deletedKey)
	}
}

func TestIngestOneReturnsCompensationFailureWhenAuthorityTransactionFails(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏正文"}}
	store := &recordingAuthorityStore{saveErr: errors.New("postgres down")}
	objects := &recordingObjectStore{deleteErr: errors.New("delete failed")}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, &recordingOutbox{})

	result := svc.IngestOne(context.Background(), Principal{ID: "u1"}, validIngestRequest("doc-1"))

	if result.Status != IngestStatusStoreFailed {
		t.Fatalf("result = %#v, want store failed", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "object compensation failed") {
		t.Fatalf("error = %v, want compensation failure context", result.Err)
	}
}

func TestBatchIngestContinuesAfterSingleDocumentFailure(t *testing.T) {
	sanitizer := &recordingSanitizer{failFor: map[string]error{
		"坏正文": gateway.ErrDesensitizationIncomplete,
	}, result: SanitizedDocument{Text: "已脱敏"}}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, &recordingAuthorityStore{}, &recordingObjectStore{}, &recordingOutbox{})

	results := svc.BatchIngest(context.Background(), Principal{ID: "u1"}, []IngestRequest{
		validIngestRequest("doc-ok"),
		{
			DocumentID:     "doc-bad",
			ObjectKey:      "templates/doc-bad.docx",
			Content:        "坏正文",
			Classification: "internal",
			DocumentType:   "通知",
		},
	})

	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if results[0].Status != IngestStatusSuccess || results[1].Status != IngestStatusDesensitizationFailed {
		t.Fatalf("results = %#v", results)
	}
}

func TestBatchIngestWithCheckpointSkipsSucceededDocumentsAndRecordsFailures(t *testing.T) {
	sanitizer := &recordingSanitizer{
		failFor: map[string]error{"坏正文": gateway.ErrDesensitizationIncomplete},
		result:  SanitizedDocument{Text: "已脱敏"},
	}
	checkpoint := &recordingBatchCheckpoint{
		succeeded: map[string]struct{}{"batch-1/doc-done": {}},
	}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, &recordingAuthorityStore{}, &recordingObjectStore{}, &recordingOutbox{})

	results := svc.BatchIngestWithCheckpoint(context.Background(), Principal{ID: "u1"}, BatchIngestRequest{
		BatchID: "batch-1",
		Documents: []IngestRequest{
			validIngestRequest("doc-done"),
			validIngestRequest("doc-new"),
			{
				DocumentID:     "doc-bad",
				ObjectKey:      "templates/doc-bad.docx",
				Content:        "坏正文",
				Classification: "internal",
				DocumentType:   "通知",
			},
		},
	}, checkpoint)

	if len(results) != 3 {
		t.Fatalf("results length = %d, want 3", len(results))
	}
	if results[0].Status != IngestStatusSkipped || results[0].Reason != ReasonAlreadySucceeded {
		t.Fatalf("first result = %#v, want checkpoint skip", results[0])
	}
	if results[1].Status != IngestStatusSuccess || results[2].Status != IngestStatusDesensitizationFailed {
		t.Fatalf("results = %#v", results)
	}
	if !checkpoint.recorded["batch-1/doc-new"] || !checkpoint.recorded["batch-1/doc-bad"] {
		t.Fatalf("recorded checkpoint results = %#v", checkpoint.recorded)
	}
}

func TestIdentifierStoragePolicyKeepsAuthorityPlaintextAndHashesDerivedIdentifiers(t *testing.T) {
	authority := StoredChunk{
		ChunkID:          "doc-1:0000",
		DocumentNumber:   "〔2024〕12号",
		OrganizationName: "XX市人民政府办公厅",
	}

	projected := ProjectChunkForDerivedIndex(authority, IdentifierStoragePolicy{
		ProtectDocumentNumber:   true,
		ProtectOrganizationName: true,
		HashSalt:                "tenant-a",
	})
	projectedAgain := ProjectChunkForDerivedIndex(authority, IdentifierStoragePolicy{
		ProtectDocumentNumber:   true,
		ProtectOrganizationName: true,
		HashSalt:                "tenant-a",
	})

	if authority.DocumentNumber != "〔2024〕12号" || authority.OrganizationName != "XX市人民政府办公厅" {
		t.Fatalf("authority chunk was mutated: %#v", authority)
	}
	if projected.DocumentNumber == authority.DocumentNumber || projected.OrganizationName == authority.OrganizationName {
		t.Fatalf("derived identifiers should not keep plaintext when protected: %#v", projected)
	}
	if projected.DocumentNumber != projectedAgain.DocumentNumber || projected.OrganizationName != projectedAgain.OrganizationName {
		t.Fatalf("protected identifiers are not deterministic: %#v %#v", projected, projectedAgain)
	}
}

func TestAdoptionFeedbackDirectUseBypassesHumanRBACAndUsesSamePipeline(t *testing.T) {
	auth := &recordingAuthorizer{err: errors.New("human entry denied")}
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏采纳稿"}}
	store := &recordingAuthorityStore{}
	outbox := &recordingOutbox{}
	svc := NewIngestService(auth, sanitizer, store, &recordingObjectStore{}, outbox)

	result := svc.IngestAdoptionFeedback(context.Background(), AdoptionSignal{
		GeneratedTaskID:  "task-1",
		AdoptionActionID: "adopt-1",
		SourceObjectKey:  "drafts/task-1.docx",
		Content:          "采纳稿原文",
		Decision:         AdoptionDecisionDirectUse,
		Classification:   "internal",
		DocumentType:     "通知",
	})

	if result.Status != IngestStatusSuccess {
		t.Fatalf("result = %#v, want success", result)
	}
	if auth.lastPermission != "" {
		t.Fatalf("human RBAC authorizer was called for c08 outbox signal: %s", auth.lastPermission)
	}
	if store.document.DocumentID == "" || store.document.ObjectKey == "drafts/task-1.docx" || !strings.HasPrefix(store.document.ObjectKey, "corpus/adoptions/") {
		t.Fatalf("stored document = %#v", store.document)
	}
	if store.feedback.GeneratedTaskID != "task-1" || store.feedback.AdoptionActionID != "adopt-1" || store.feedback.IngestedDocumentID != store.document.DocumentID {
		t.Fatalf("feedback record = %#v", store.feedback)
	}
	if outbox.saved {
		t.Fatalf("adoption ingest must not enqueue outbox outside authority transaction: %#v", outbox.event)
	}
	if len(store.events) != 1 || store.events[0].EventType != OutboxEventIndexChunk {
		t.Fatalf("transactional outbox events = %#v", store.events)
	}
}

func TestAdoptionFeedbackDoesNotUseSeparatePostTransactionWrite(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏采纳稿"}}
	store := &recordingAuthorityStore{feedbackErr: errors.New("separate adoption feedback write")}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, &recordingObjectStore{}, &recordingOutbox{})

	result := svc.IngestAdoptionFeedback(context.Background(), AdoptionSignal{
		GeneratedTaskID:  "task-1",
		AdoptionActionID: "adopt-1",
		SourceObjectKey:  "drafts/task-1.docx",
		Content:          "采纳稿原文",
		Decision:         AdoptionDecisionDirectUse,
		Classification:   "internal",
		DocumentType:     "通知",
	})

	if result.Status != IngestStatusSuccess {
		t.Fatalf("result = %#v, want success without separate feedback write", result)
	}
	if store.feedback.GeneratedTaskID != "task-1" || store.feedback.IngestedDocumentID != store.document.DocumentID {
		t.Fatalf("transactional feedback record = %#v", store.feedback)
	}
}

func TestAdoptionOutboxConsumerReadsReferencedObjectWithoutPayloadContent(t *testing.T) {
	sanitizer := &recordingSanitizer{
		failFor: map[string]error{"": errors.New("empty adoption content")},
		result:  SanitizedDocument{Text: "已脱敏采纳稿"},
	}
	store := &recordingAuthorityStore{}
	objects := &recordingObjectStore{originals: map[string][]byte{
		"drafts/task-1.docx": []byte("采纳稿原文"),
	}}
	consumer := NewAdoptionOutboxConsumer(NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, &recordingOutbox{}))

	result := consumer.Consume(context.Background(), OutboxEvent{
		EventType: OutboxEventAdoptionIngest,
		Payload: map[string]string{
			"generated_task_id":  "task-1",
			"adoption_action_id": "adopt-1",
			"source_object_key":  "drafts/task-1.docx",
			"source_chunk_id":    "drafts/task-1:0000",
			"decision":           string(AdoptionDecisionMinorEdit),
			"classification":     "internal",
			"document_type":      "通知",
		},
	})

	if result.Status != IngestStatusSuccess {
		t.Fatalf("result = %#v, want success", result)
	}
	if sanitizer.lastContent != "采纳稿原文" {
		t.Fatalf("sanitizer content = %q, want referenced object content", sanitizer.lastContent)
	}
	if store.feedback.GeneratedTaskID != "task-1" || store.feedback.AdoptionActionID != "adopt-1" {
		t.Fatalf("feedback record = %#v", store.feedback)
	}
}

func TestAdoptionOutboxConsumerRejectsInlinePayloadContent(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏采纳稿"}}
	store := &recordingAuthorityStore{}
	objects := &recordingObjectStore{originals: map[string][]byte{
		"drafts/task-1.docx": []byte("采纳稿原文"),
	}}
	consumer := NewAdoptionOutboxConsumer(NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, &recordingOutbox{}))

	result := consumer.Consume(context.Background(), OutboxEvent{
		EventType: OutboxEventAdoptionIngest,
		Payload: map[string]string{
			"generated_task_id":  "task-1",
			"adoption_action_id": "adopt-1",
			"source_object_key":  "drafts/task-1.docx",
			"content":            "采纳稿原文",
			"decision":           string(AdoptionDecisionDirectUse),
			"classification":     "internal",
			"document_type":      "通知",
		},
	})

	if result.Status != IngestStatusRejected || result.Reason != "unsafe_adoption_payload" {
		t.Fatalf("result = %#v, want unsafe payload rejection", result)
	}
	if sanitizer.called || store.saved {
		t.Fatalf("unsafe payload entered ingest pipeline: sanitizer=%v store=%v", sanitizer.called, store.saved)
	}
}

func TestAdoptionOutboxConsumerRejectsMalformedPayload(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏采纳稿"}}
	store := &recordingAuthorityStore{}
	objects := &recordingObjectStore{originals: map[string][]byte{
		"drafts/task-1.docx": []byte("采纳稿原文"),
	}}
	consumer := NewAdoptionOutboxConsumer(NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, objects, &recordingOutbox{}))

	result := consumer.Consume(context.Background(), OutboxEvent{
		EventType: OutboxEventAdoptionIngest,
		Payload: map[string]string{
			"generated_task_id":  "task-1",
			"adoption_action_id": "adopt-1",
			"source_object_key":  "drafts/task-1.docx",
			"classification":     "internal",
			"document_type":      "通知",
		},
	})

	if result.Status != IngestStatusRejected || result.Reason != "invalid_adoption_payload" {
		t.Fatalf("result = %#v, want invalid payload rejection", result)
	}
	if sanitizer.called || store.saved {
		t.Fatalf("malformed payload entered ingest pipeline: sanitizer=%v store=%v", sanitizer.called, store.saved)
	}
}

func TestAdoptionFeedbackIgnoresMajorEditAndBlocksMissingClassification(t *testing.T) {
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏采纳稿"}}
	store := &recordingAuthorityStore{}
	svc := NewIngestService(allowCorpusAuthorizer{}, sanitizer, store, &recordingObjectStore{}, &recordingOutbox{})

	ignored := svc.IngestAdoptionFeedback(context.Background(), AdoptionSignal{
		GeneratedTaskID:  "task-1",
		AdoptionActionID: "adopt-1",
		Decision:         AdoptionDecisionMajorEdit,
		Classification:   "internal",
		DocumentType:     "通知",
	})
	if ignored.Status != IngestStatusIgnored || sanitizer.called || store.saved {
		t.Fatalf("ignored signal result=%#v sanitizer=%v store=%v", ignored, sanitizer.called, store.saved)
	}

	blocked := svc.IngestAdoptionFeedback(context.Background(), AdoptionSignal{
		GeneratedTaskID:  "task-2",
		AdoptionActionID: "adopt-2",
		Decision:         AdoptionDecisionMinorEdit,
		DocumentType:     "通知",
	})
	if blocked.Status != IngestStatusRejected || blocked.Reason != ReasonMissingClassification {
		t.Fatalf("blocked result = %#v, want missing classification rejection", blocked)
	}
}

func validIngestRequest(id string) IngestRequest {
	return IngestRequest{
		DocumentID:     id,
		ObjectKey:      "templates/" + id + ".docx",
		Content:        "XX市人民政府办公厅印发了〔2024〕12号。",
		Classification: "internal",
		DocumentType:   "通知",
	}
}

type recordingAuthorizer struct {
	err            error
	lastPermission Permission
}

func (a *recordingAuthorizer) Authorize(_ context.Context, _ Principal, permission Permission) error {
	a.lastPermission = permission
	return a.err
}

type allowCorpusAuthorizer struct{}

func (allowCorpusAuthorizer) Authorize(context.Context, Principal, Permission) error {
	return nil
}

type recordingSanitizer struct {
	called      bool
	lastContent string
	result      SanitizedDocument
	err         error
	failFor     map[string]error
}

func (s *recordingSanitizer) SanitizeDocument(_ context.Context, content string) (SanitizedDocument, error) {
	s.called = true
	s.lastContent = content
	if err := s.failFor[content]; err != nil {
		return SanitizedDocument{}, err
	}
	if s.err != nil {
		return SanitizedDocument{}, s.err
	}
	if s.result.Text == "" {
		s.result.Text = content
	}
	return s.result, nil
}

type recordingAuthorityStore struct {
	saved       bool
	document    StoredDocument
	chunks      []StoredChunk
	events      []OutboxEvent
	feedback    AdoptionFeedbackRecord
	feedbackErr error
	saveErr     error
}

func (s *recordingAuthorityStore) SaveDocument(_ context.Context, doc StoredDocument, chunks []StoredChunk, events []OutboxEvent, feedback *AdoptionFeedbackRecord) error {
	s.saved = true
	s.document = doc
	s.chunks = append([]StoredChunk(nil), chunks...)
	s.events = append([]OutboxEvent(nil), events...)
	if feedback != nil {
		s.feedback = *feedback
	}
	return s.saveErr
}

func (s *recordingAuthorityStore) SaveAdoptionFeedback(_ context.Context, record AdoptionFeedbackRecord) error {
	s.feedback = record
	return s.feedbackErr
}

type recordingObjectStore struct {
	saved      bool
	objectKey  string
	content    []byte
	originals  map[string][]byte
	deletedKey string
	deleteErr  error
}

func (s *recordingObjectStore) PutOriginal(_ context.Context, objectKey string, content []byte) error {
	s.saved = true
	s.objectKey = objectKey
	s.content = append([]byte(nil), content...)
	return nil
}

func (s *recordingObjectStore) GetOriginal(_ context.Context, objectKey string) ([]byte, error) {
	content, ok := s.originals[objectKey]
	if !ok {
		return nil, errors.New("original object not found")
	}
	return append([]byte(nil), content...), nil
}

func (s *recordingObjectStore) DeleteOriginal(_ context.Context, objectKey string) error {
	s.deletedKey = objectKey
	return s.deleteErr
}

type recordingOutbox struct {
	saved bool
	event OutboxEvent
}

func (s *recordingOutbox) Enqueue(_ context.Context, event OutboxEvent) error {
	s.saved = true
	s.event = event
	return nil
}

type recordingBatchCheckpoint struct {
	succeeded map[string]struct{}
	recorded  map[string]bool
}

func (c *recordingBatchCheckpoint) AlreadySucceeded(_ context.Context, batchID, documentID string) (bool, error) {
	_, ok := c.succeeded[batchID+"/"+documentID]
	return ok, nil
}

func (c *recordingBatchCheckpoint) RecordResult(_ context.Context, batchID string, result IngestResult) error {
	if c.recorded == nil {
		c.recorded = make(map[string]bool)
	}
	c.recorded[batchID+"/"+result.DocumentID] = true
	return nil
}
