package gateway

type EntityType string

const (
	EntityTypeOrganization            EntityType = "organization"
	EntityTypePerson                  EntityType = "person"
	EntityTypeProjectCode             EntityType = "project_code"
	EntityTypeSecretKeywordBlacklist  EntityType = "secret_keyword_blacklist"
	EntityTypeDocumentNumber          EntityType = "document_number"
	EntityTypeAmount                  EntityType = "amount"
	EntityTypeIdentityNumber          EntityType = "identity_number"
	EntityTypeUnifiedSocialCreditCode EntityType = "unified_social_credit_code"
	EntityTypeNamedEntity             EntityType = "named_entity"
)

type Source string

const (
	SourceRegex      Source = "regex"
	SourceDictionary Source = "dictionary"
	SourceNER        Source = "ner"
)

type Hit struct {
	Start  int
	End    int
	Text   string
	Type   EntityType
	Source Source
}

func (h Hit) overlaps(other Hit) bool {
	return h.Start < other.End && other.Start < h.End
}

func (h Hit) length() int {
	return h.End - h.Start
}
