package dictionary

import (
	"context"
	"errors"
	"time"
)

type EntryType string

const (
	EntryTypeOrganization                EntryType  = "organization"
	EntryTypePerson                      EntryType  = "person"
	EntryTypeProjectCode                 EntryType  = "project_code"
	EntryTypeSecretKeywordBlacklist      EntryType  = "secret_keyword_blacklist"
	PermissionDictManage                 Permission = "dict.manage"
	defaultDictionaryImportBatchCapacity            = 16
)

type Permission string

type Principal struct {
	ID string
}

type Authorizer interface {
	Authorize(context.Context, Principal, Permission) error
}

type EntryReloader interface {
	ReloadDictionary(context.Context, []Entry) error
}

type Entry struct {
	ID        string
	Text      string
	Type      EntryType
	Deleted   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateEntryRequest struct {
	Text string
	Type EntryType
}

type UpdateEntryRequest struct {
	Text string
	Type EntryType
}

type ImportEntry struct {
	Text string
	Type EntryType
}

var (
	ErrUnauthorized     = errors.New("unauthorized dictionary access")
	ErrEntryNotFound    = errors.New("dictionary entry not found")
	ErrInvalidEntryText = errors.New("dictionary entry text is required")
	ErrInvalidEntryType = errors.New("invalid dictionary entry type")
)

func (t EntryType) Valid() bool {
	switch t {
	case EntryTypeOrganization, EntryTypePerson, EntryTypeProjectCode, EntryTypeSecretKeywordBlacklist:
		return true
	default:
		return false
	}
}
