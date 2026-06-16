package corpus

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/rag/vector"
)

func TestOutboxProcessorRetriesWithoutDroppingFailedIndexEvent(t *testing.T) {
	status := &recordingOutboxStatusStore{}
	index := &recordingDerivedIndex{err: errors.New("milvus down")}
	source := staticAuthorityChunkGetter{chunks: map[string]StoredChunk{
		"chunk-1": {ChunkID: "chunk-1", DocumentID: "doc-1", ContentText: "已脱敏正文", Classification: "internal", DocumentType: "通知"},
	}}
	processor := NewOutboxProcessor(status, index, source, &recordingEmbedder{values: []float64{0.1, 0.2, 0.3}}, IdentifierStoragePolicy{})

	err := processor.Process(context.Background(), OutboxEvent{EventID: 7, EventType: OutboxEventIndexChunk, ChunkID: "chunk-1"})

	if err == nil {
		t.Fatal("process error = nil, want index failure")
	}
	if status.failedEventID != 7 || status.succeededEventID != 0 {
		t.Fatalf("status store = %#v", status)
	}

	index.err = nil
	err = processor.Process(context.Background(), OutboxEvent{EventID: 7, EventType: OutboxEventIndexChunk, ChunkID: "chunk-1"})
	if err != nil {
		t.Fatalf("retry process: %v", err)
	}
	if status.succeededEventID != 7 {
		t.Fatalf("succeeded event id = %d, want 7", status.succeededEventID)
	}
	if len(index.indexedChunks) != 1 || index.indexedChunks[0].ContentText != "已脱敏正文" || index.indexedChunks[0].DocumentType != "通知" {
		t.Fatalf("indexed chunks = %#v, want complete authority chunk", index.indexedChunks)
	}
	if len(index.indexedChunks[0].DenseVector) != 3 {
		t.Fatalf("dense vector length = %d, want 3", len(index.indexedChunks[0].DenseVector))
	}
}

func TestOutboxProcessorEmbedsAuthorityChunkBeforeIndexing(t *testing.T) {
	status := &recordingOutboxStatusStore{}
	index := &recordingDerivedIndex{}
	source := staticAuthorityChunkGetter{chunks: map[string]StoredChunk{
		"chunk-1": {
			ChunkID:          "chunk-1",
			DocumentID:       "doc-1",
			ContentText:      "已脱敏正文",
			Classification:   "internal",
			DocumentType:     "通知",
			DocumentNumber:   "〔2024〕12号",
			OrganizationName: "XX市人民政府办公厅",
		},
	}}
	embedder := &recordingEmbedder{values: []float64{0.1, 0.2, 0.3}}
	processor := NewOutboxProcessor(status, index, source, embedder, IdentifierStoragePolicy{
		ProtectDocumentNumber:   true,
		ProtectOrganizationName: true,
		HashSalt:                "tenant-a",
	})

	err := processor.Process(context.Background(), OutboxEvent{EventID: 9, EventType: OutboxEventIndexChunk, ChunkID: "chunk-1"})

	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(index.indexedChunks) != 1 {
		t.Fatalf("indexed chunks = %#v, want 1", index.indexedChunks)
	}
	chunk := index.indexedChunks[0]
	if len(chunk.DenseVector) != 3 || chunk.DenseVector[0] != float32(0.1) {
		t.Fatalf("dense vector = %#v, want embedding output", chunk.DenseVector)
	}
	if chunk.DocumentNumber == "〔2024〕12号" || chunk.OrganizationName == "XX市人民政府办公厅" {
		t.Fatalf("derived identifiers were not projected: %#v", chunk)
	}
	if embedder.lastText != "已脱敏正文" {
		t.Fatalf("embedded text = %q, want chunk content", embedder.lastText)
	}
	if status.succeededEventID != 9 {
		t.Fatalf("succeeded event id = %d, want 9", status.succeededEventID)
	}
}

func TestOutboxProcessorSynchronizesSoftDelete(t *testing.T) {
	status := &recordingOutboxStatusStore{}
	index := &recordingDerivedIndex{}
	processor := NewOutboxProcessor(status, index, nil, nil, IdentifierStoragePolicy{})

	err := processor.Process(context.Background(), OutboxEvent{EventID: 8, EventType: OutboxEventSoftDelete, ChunkID: "chunk-2"})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if index.deletedChunkID != "chunk-2" || status.succeededEventID != 8 {
		t.Fatalf("index=%#v status=%#v", index, status)
	}
}

func TestReconcilerEnqueuesMissingAndSoftDeletedChunkRepairs(t *testing.T) {
	source := staticAuthorityChunks{chunks: []StoredChunk{
		{ChunkID: "chunk-present", Classification: "internal", DocumentType: "通知"},
		{ChunkID: "chunk-missing", Classification: "internal", DocumentType: "通知"},
		{ChunkID: "chunk-deleted", IsDeleted: true},
	}}
	derived := staticDerivedChunks{chunks: []DerivedChunkState{
		{ChunkID: "chunk-present", Classification: "internal", DocumentType: "通知"},
		{ChunkID: "chunk-deleted", Classification: "internal", DocumentType: "通知"},
		{ChunkID: "chunk-extra", Classification: "internal", DocumentType: "通知"},
	}}
	outbox := &recordingOutboxList{}
	reconciler := NewReconciler(source, derived, outbox)

	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.MissingInDerived != 1 || result.ShouldDeleteInDerived != 2 {
		t.Fatalf("result = %#v", result)
	}
	if len(outbox.events) != 3 {
		t.Fatalf("events = %#v, want rebuild missing + delete deleted/extra", outbox.events)
	}
}

func TestRebuilderRecreatesDerivedIndexFromAuthoritySource(t *testing.T) {
	source := staticAuthorityChunks{chunks: []StoredChunk{
		{
			ChunkID:          "chunk-1",
			ContentText:      "已脱敏正文",
			Classification:   "internal",
			DocumentType:     "通知",
			DocumentNumber:   "〔2024〕12号",
			OrganizationName: "XX市人民政府办公厅",
		},
		{ChunkID: "chunk-2", IsDeleted: true},
	}}
	index := &recordingDerivedIndex{}
	rebuilder := NewRebuilder(source, index, &recordingEmbedder{values: []float64{0.1, 0.2, 0.3}}, IdentifierStoragePolicy{
		ProtectDocumentNumber:   true,
		ProtectOrganizationName: true,
		HashSalt:                "tenant-a",
	})

	result, err := rebuilder.RebuildAll(context.Background())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if result.Reindexed != 1 || result.Deleted != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(index.indexedChunks) != 1 || index.indexedChunks[0].ChunkID != "chunk-1" || index.deletedChunkID != "chunk-2" {
		t.Fatalf("index = %#v", index)
	}
	chunk := index.indexedChunks[0]
	if len(chunk.DenseVector) != 3 {
		t.Fatalf("dense vector length = %d, want 3", len(chunk.DenseVector))
	}
	if chunk.DocumentNumber == "〔2024〕12号" || chunk.OrganizationName == "XX市人民政府办公厅" {
		t.Fatalf("rebuilder did not project derived identifiers: %#v", chunk)
	}
}

func TestFullRebuilderRegeneratesDerivedIndexFromAuthorityMetadataAndOriginalObject(t *testing.T) {
	source := staticAuthorityDocuments{documents: []StoredDocument{{
		DocumentID:       "doc-1",
		ObjectKey:        "templates/doc-1.docx",
		Classification:   "internal",
		DocumentType:     "通知",
		DocumentNumber:   "〔2024〕12号",
		OrganizationName: "XX市人民政府办公厅",
	}}}
	objects := staticOriginalObjects{objects: map[string][]byte{
		"templates/doc-1.docx": []byte("原始正文"),
	}}
	sanitizer := &recordingSanitizer{result: SanitizedDocument{Text: "已脱敏正文"}}
	embedder := &recordingEmbedder{values: []float64{0.1, 0.2, 0.3}}
	index := &recordingDerivedIndex{}
	rebuilder := NewFullRebuilder(source, objects, sanitizer, embedder, index, IdentifierStoragePolicy{})

	result, err := rebuilder.RebuildAll(context.Background())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if result.Reindexed != 1 || result.Deleted != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(index.indexedChunks) != 1 {
		t.Fatalf("indexed chunks = %#v", index.indexedChunks)
	}
	chunk := index.indexedChunks[0]
	if chunk.ChunkID != "doc-1:0000" ||
		chunk.ContentText != "已脱敏正文" ||
		chunk.Classification != "internal" ||
		chunk.DocumentType != "通知" ||
		chunk.DocumentNumber != "〔2024〕12号" ||
		chunk.OrganizationName != "XX市人民政府办公厅" ||
		len(chunk.DenseVector) != 3 {
		t.Fatalf("rebuilt chunk = %#v", chunk)
	}
}

func TestClassificationDowngradePlannerUsesDeleteThenReinsert(t *testing.T) {
	events := PlanClassificationDowngrade(StoredChunk{
		ChunkID:        "doc-1:0000",
		Classification: "secret",
		DocumentType:   "通知",
	}, "internal")

	if len(events) != 2 {
		t.Fatalf("events = %#v, want delete + reinsert", events)
	}
	if events[0].EventType != OutboxEventSoftDelete || events[0].ChunkID != "doc-1:0000" {
		t.Fatalf("first event = %#v, want soft delete", events[0])
	}
	if events[1].EventType != OutboxEventRebuildChunk || events[1].Payload["classification"] != "internal" {
		t.Fatalf("second event = %#v, want rebuild with downgraded classification", events[1])
	}
}

type recordingOutboxStatusStore struct {
	succeededEventID int64
	failedEventID    int64
}

func (s *recordingOutboxStatusStore) MarkSucceeded(_ context.Context, eventID int64) error {
	s.succeededEventID = eventID
	return nil
}

func (s *recordingOutboxStatusStore) MarkFailed(_ context.Context, eventID int64, _ error) error {
	s.failedEventID = eventID
	return nil
}

type recordingDerivedIndex struct {
	err            error
	indexedChunks  []StoredChunk
	deletedChunkID string
}

func (i *recordingDerivedIndex) IndexChunk(_ context.Context, chunk StoredChunk) error {
	if i.err != nil {
		return i.err
	}
	i.indexedChunks = append(i.indexedChunks, chunk)
	return nil
}

func (i *recordingDerivedIndex) DeleteChunk(_ context.Context, chunkID string) error {
	if i.err != nil {
		return i.err
	}
	i.deletedChunkID = chunkID
	return nil
}

type staticAuthorityChunks struct {
	chunks []StoredChunk
}

func (s staticAuthorityChunks) ListAuthorityChunks(context.Context) ([]StoredChunk, error) {
	return append([]StoredChunk(nil), s.chunks...), nil
}

type staticAuthorityChunkGetter struct {
	chunks map[string]StoredChunk
}

func (s staticAuthorityChunkGetter) GetAuthorityChunk(_ context.Context, chunkID string) (StoredChunk, error) {
	return s.chunks[chunkID], nil
}

type staticDerivedChunks struct {
	chunks []DerivedChunkState
}

func (s staticDerivedChunks) ListDerivedChunks(context.Context) ([]DerivedChunkState, error) {
	return append([]DerivedChunkState(nil), s.chunks...), nil
}

type recordingOutboxList struct {
	events []OutboxEvent
}

func (o *recordingOutboxList) Enqueue(_ context.Context, event OutboxEvent) error {
	o.events = append(o.events, event)
	return nil
}

type staticAuthorityDocuments struct {
	documents []StoredDocument
}

func (s staticAuthorityDocuments) ListAuthorityDocuments(context.Context) ([]StoredDocument, error) {
	return append([]StoredDocument(nil), s.documents...), nil
}

type staticOriginalObjects struct {
	objects map[string][]byte
}

func (s staticOriginalObjects) GetOriginal(_ context.Context, objectKey string) ([]byte, error) {
	return append([]byte(nil), s.objects[objectKey]...), nil
}

type recordingEmbedder struct {
	values   []float64
	lastText string
}

func (e *recordingEmbedder) Embed(_ context.Context, text string) (vector.Embedding, error) {
	e.lastText = text
	return vector.Embedding{Model: "bge-test", Values: append([]float64(nil), e.values...)}, nil
}

func (e *recordingEmbedder) Profile() vector.EmbeddingProfile {
	return vector.EmbeddingProfile{Model: "bge-test", Dimensions: len(e.values)}
}
