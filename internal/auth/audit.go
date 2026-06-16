package auth

import "context"

type AuditLogService struct {
	reader  AuditReader
	decider *AccessDecisionService
}

func NewAuditLogService(reader AuditReader, decider *AccessDecisionService) *AuditLogService {
	return &AuditLogService{reader: reader, decider: decider}
}

func (s *AuditLogService) ListAccountSecurityAudits(ctx context.Context, principal Principal) ([]AuditEntry, error) {
	if s == nil || s.reader == nil || s.decider == nil {
		return nil, ErrUnauthorized
	}
	result := s.decider.Decide(ctx, principal, PermissionAuditRead, AccountSecurityReadContext())
	if !result.Allowed() {
		return nil, ErrUnauthorized
	}
	return s.reader.ListAudits(ctx)
}
