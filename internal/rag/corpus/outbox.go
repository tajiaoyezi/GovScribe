package corpus

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/gateway"
	"github.com/tajiaoyezi/GovScribe/internal/rag/vector"
)

var ErrUnsupportedOutboxEvent = errors.New("unsupported corpus outbox event")

type OutboxStatusStore interface {
	MarkSucceeded(context.Context, int64) error
	MarkFailed(context.Context, int64, error) error
}

type DerivedIndex interface {
	IndexChunk(context.Context, StoredChunk) error
	DeleteChunk(context.Context, string) error
}

type OutboxProcessor struct {
	status           OutboxStatusStore
	index            DerivedIndex
	source           AuthorityChunkGetter
	embedder         vector.Embedder
	identifierPolicy IdentifierStoragePolicy
}

type AuthorityChunkGetter interface {
	GetAuthorityChunk(context.Context, string) (StoredChunk, error)
}

func NewOutboxProcessor(status OutboxStatusStore, index DerivedIndex, source AuthorityChunkGetter, embedder vector.Embedder, policy IdentifierStoragePolicy) *OutboxProcessor {
	return &OutboxProcessor{
		status:           status,
		index:            index,
		source:           source,
		embedder:         embedder,
		identifierPolicy: policy,
	}
}

func (p *OutboxProcessor) Process(ctx context.Context, event OutboxEvent) error {
	if p.status == nil || p.index == nil {
		return ErrUnsupportedOutboxEvent
	}
	var err error
	switch event.EventType {
	case OutboxEventIndexChunk, OutboxEventRebuildChunk:
		if p.source == nil || p.embedder == nil {
			err = ErrUnsupportedOutboxEvent
			break
		}
		var chunk StoredChunk
		chunk, err = p.source.GetAuthorityChunk(ctx, event.ChunkID)
		if err == nil {
			if chunk.IsDeleted {
				err = p.index.DeleteChunk(ctx, event.ChunkID)
			} else {
				chunk, err = embedStoredChunk(ctx, p.embedder, chunk)
				if err == nil {
					err = p.index.IndexChunk(ctx, ProjectChunkForDerivedIndex(chunk, p.identifierPolicy))
				}
			}
		}
	case OutboxEventSoftDelete:
		err = p.index.DeleteChunk(ctx, event.ChunkID)
	default:
		err = ErrUnsupportedOutboxEvent
	}
	if err != nil {
		_ = p.status.MarkFailed(ctx, event.EventID, err)
		return err
	}
	return p.status.MarkSucceeded(ctx, event.EventID)
}

func embedStoredChunk(ctx context.Context, embedder vector.Embedder, chunk StoredChunk) (StoredChunk, error) {
	if embedder == nil {
		return StoredChunk{}, ErrUnsupportedOutboxEvent
	}
	embedding, err := embedder.Embed(ctx, chunk.ContentText)
	if err != nil {
		return StoredChunk{}, err
	}
	profile := embedder.Profile()
	if profile.Dimensions <= 0 || len(embedding.Values) != profile.Dimensions {
		return StoredChunk{}, fmt.Errorf("%w: got %d want %d", vector.ErrEmbeddingDimensionMismatch, len(embedding.Values), profile.Dimensions)
	}
	if strings.TrimSpace(profile.Model) == "" || strings.TrimSpace(embedding.Model) == "" || embedding.Model != profile.Model {
		return StoredChunk{}, vector.ErrEmbeddingProfileMismatch
	}
	chunk.DenseVector = float32Vector(embedding.Values)
	return chunk, nil
}

type AuthorityChunkSource interface {
	ListAuthorityChunks(context.Context) ([]StoredChunk, error)
}

type DerivedChunkState struct {
	ChunkID        string
	Classification string
	DocumentType   string
	IsDeleted      bool
}

type DerivedChunkLister interface {
	ListDerivedChunks(context.Context) ([]DerivedChunkState, error)
}

type ReconciliationResult struct {
	MissingInDerived      int
	InconsistentInDerived int
	ShouldDeleteInDerived int
}

type Reconciler struct {
	authority AuthorityChunkSource
	derived   DerivedChunkLister
	outbox    Outbox
}

func NewReconciler(authority AuthorityChunkSource, derived DerivedChunkLister, outbox Outbox) *Reconciler {
	return &Reconciler{authority: authority, derived: derived, outbox: outbox}
}

func (r *Reconciler) Reconcile(ctx context.Context) (ReconciliationResult, error) {
	authorityChunks, err := r.authority.ListAuthorityChunks(ctx)
	if err != nil {
		return ReconciliationResult{}, err
	}
	derivedChunks, err := r.derived.ListDerivedChunks(ctx)
	if err != nil {
		return ReconciliationResult{}, err
	}
	authorityByID := make(map[string]StoredChunk, len(authorityChunks))
	for _, chunk := range authorityChunks {
		authorityByID[chunk.ChunkID] = chunk
	}
	derivedByID := make(map[string]DerivedChunkState, len(derivedChunks))
	for _, chunk := range derivedChunks {
		derivedByID[chunk.ChunkID] = chunk
	}

	var result ReconciliationResult
	for _, chunk := range authorityChunks {
		derived, ok := derivedByID[chunk.ChunkID]
		if chunk.IsDeleted {
			if ok {
				result.ShouldDeleteInDerived++
				if err := r.outbox.Enqueue(ctx, OutboxEvent{EventType: OutboxEventSoftDelete, ChunkID: chunk.ChunkID}); err != nil {
					return result, err
				}
			}
			continue
		}
		if !ok {
			result.MissingInDerived++
			if err := r.outbox.Enqueue(ctx, OutboxEvent{EventType: OutboxEventRebuildChunk, ChunkID: chunk.ChunkID}); err != nil {
				return result, err
			}
			continue
		}
		if derived.Classification != chunk.Classification || derived.DocumentType != chunk.DocumentType || derived.IsDeleted != chunk.IsDeleted {
			result.InconsistentInDerived++
			if err := r.outbox.Enqueue(ctx, OutboxEvent{EventType: OutboxEventRebuildChunk, ChunkID: chunk.ChunkID}); err != nil {
				return result, err
			}
		}
	}
	for _, chunk := range derivedChunks {
		if _, ok := authorityByID[chunk.ChunkID]; !ok {
			result.ShouldDeleteInDerived++
			if err := r.outbox.Enqueue(ctx, OutboxEvent{EventType: OutboxEventSoftDelete, ChunkID: chunk.ChunkID}); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

type RebuildResult struct {
	Reindexed int
	Deleted   int
}

type Rebuilder struct {
	source           AuthorityChunkSource
	index            DerivedIndex
	embedder         vector.Embedder
	identifierPolicy IdentifierStoragePolicy
}

func NewRebuilder(source AuthorityChunkSource, index DerivedIndex, embedder vector.Embedder, policy IdentifierStoragePolicy) *Rebuilder {
	return &Rebuilder{source: source, index: index, embedder: embedder, identifierPolicy: policy}
}

func (r *Rebuilder) RebuildAll(ctx context.Context) (RebuildResult, error) {
	if r.source == nil || r.index == nil || r.embedder == nil {
		return RebuildResult{}, ErrUnsupportedOutboxEvent
	}
	chunks, err := r.source.ListAuthorityChunks(ctx)
	if err != nil {
		return RebuildResult{}, err
	}
	var result RebuildResult
	for _, chunk := range chunks {
		if chunk.IsDeleted {
			if err := r.index.DeleteChunk(ctx, chunk.ChunkID); err != nil {
				return result, err
			}
			result.Deleted++
			continue
		}
		chunk, err = embedStoredChunk(ctx, r.embedder, chunk)
		if err != nil {
			return result, err
		}
		if err := r.index.IndexChunk(ctx, ProjectChunkForDerivedIndex(chunk, r.identifierPolicy)); err != nil {
			return result, err
		}
		result.Reindexed++
	}
	return result, nil
}

type AuthorityDocumentSource interface {
	ListAuthorityDocuments(context.Context) ([]StoredDocument, error)
}

type OriginalReader interface {
	GetOriginal(context.Context, string) ([]byte, error)
}

type FullRebuilder struct {
	source           AuthorityDocumentSource
	objects          OriginalReader
	sanitizer        DocumentSanitizer
	embedder         vector.Embedder
	index            DerivedIndex
	identifierPolicy IdentifierStoragePolicy
}

func NewFullRebuilder(source AuthorityDocumentSource, objects OriginalReader, sanitizer DocumentSanitizer, embedder vector.Embedder, index DerivedIndex, policy IdentifierStoragePolicy) *FullRebuilder {
	return &FullRebuilder{
		source:           source,
		objects:          objects,
		sanitizer:        sanitizer,
		embedder:         embedder,
		index:            index,
		identifierPolicy: policy,
	}
}

func (r *FullRebuilder) RebuildAll(ctx context.Context) (RebuildResult, error) {
	if r.source == nil || r.objects == nil || r.sanitizer == nil || r.embedder == nil || r.index == nil {
		return RebuildResult{}, ErrUnsupportedOutboxEvent
	}
	docs, err := r.source.ListAuthorityDocuments(ctx)
	if err != nil {
		return RebuildResult{}, err
	}
	var result RebuildResult
	for _, doc := range docs {
		if doc.IsDeleted {
			if err := r.index.DeleteChunk(ctx, chunkID(doc.DocumentID, 0)); err != nil {
				return result, err
			}
			result.Deleted++
			continue
		}
		original, err := r.objects.GetOriginal(ctx, doc.ObjectKey)
		if err != nil {
			return result, err
		}
		sanitized, err := r.sanitizer.SanitizeDocument(ctx, string(original))
		if err != nil {
			return result, err
		}
		chunk := StoredChunk{
			ChunkID:          chunkID(doc.DocumentID, 0),
			DocumentID:       doc.DocumentID,
			ChunkIndex:       0,
			ContentText:      sanitized.Text,
			Classification:   doc.Classification,
			DocumentType:     doc.DocumentType,
			DocumentNumber:   firstNonEmpty(doc.DocumentNumber, firstMatch(sanitized.Matches, gateway.EntityTypeDocumentNumber)),
			OrganizationName: firstNonEmpty(doc.OrganizationName, firstMatch(sanitized.Matches, gateway.EntityTypeOrganization)),
			IsDeleted:        false,
		}
		chunk, err = embedStoredChunk(ctx, r.embedder, chunk)
		if err != nil {
			return result, err
		}
		if err := r.index.IndexChunk(ctx, ProjectChunkForDerivedIndex(chunk, r.identifierPolicy)); err != nil {
			return result, err
		}
		result.Reindexed++
	}
	return result, nil
}

func PlanClassificationDowngrade(chunk StoredChunk, newClassification string) []OutboxEvent {
	return []OutboxEvent{
		{
			EventType: OutboxEventSoftDelete,
			ChunkID:   chunk.ChunkID,
			Payload: map[string]string{
				"classification": strings.TrimSpace(chunk.Classification),
			},
		},
		{
			EventType: OutboxEventRebuildChunk,
			ChunkID:   chunk.ChunkID,
			Payload: map[string]string{
				"document_id":             chunk.DocumentID,
				"previous_classification": strings.TrimSpace(chunk.Classification),
				"classification":          strings.TrimSpace(newClassification),
			},
		},
	}
}

func float32Vector(values []float64) []float32 {
	out := make([]float32, len(values))
	for i, value := range values {
		out[i] = float32(value)
	}
	return out
}
