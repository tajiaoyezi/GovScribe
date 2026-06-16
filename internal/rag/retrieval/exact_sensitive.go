package retrieval

import "context"

type IdentifierAuthorityLookup interface {
	ResolveExactChunkIDs(context.Context, ExactQuery) ([]string, error)
}

type ChunkIDSearcher interface {
	SearchByChunkIDs(context.Context, []string, ExactQuery) ([]Hit, error)
}

type SensitiveIdentifierExactSearcher struct {
	authority IdentifierAuthorityLookup
	derived   ChunkIDSearcher
}

func NewSensitiveIdentifierExactSearcher(authority IdentifierAuthorityLookup, derived ChunkIDSearcher) *SensitiveIdentifierExactSearcher {
	return &SensitiveIdentifierExactSearcher{authority: authority, derived: derived}
}

func (s *SensitiveIdentifierExactSearcher) SearchExact(ctx context.Context, query ExactQuery) ([]Hit, error) {
	if s == nil || s.authority == nil || s.derived == nil {
		return nil, nil
	}
	chunkIDs, err := s.authority.ResolveExactChunkIDs(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(chunkIDs) == 0 {
		return nil, nil
	}
	return s.derived.SearchByChunkIDs(ctx, chunkIDs, query)
}
