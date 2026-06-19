package draft

import (
	"context"
	"errors"
	"strings"
)

type CompleteStructureContract struct {
	StructureContract
	Template PromptTemplateObject
}

type StructureContractReader struct {
	contracts StructureContractStore
	templates PromptTemplateObjectReader
}

func NewStructureContractReader(contracts StructureContractStore, templates PromptTemplateObjectReader) *StructureContractReader {
	return &StructureContractReader{contracts: contracts, templates: templates}
}

func (r *StructureContractReader) Get(ctx context.Context, doctypeName string) (CompleteStructureContract, error) {
	if r == nil || r.contracts == nil {
		return CompleteStructureContract{}, errors.New("structure contract store is required")
	}
	if r.templates == nil {
		return CompleteStructureContract{}, errors.New("prompt template object reader is required")
	}
	contract, err := r.contracts.Get(ctx, doctypeName)
	if err != nil {
		return CompleteStructureContract{}, err
	}
	if strings.TrimSpace(contract.TemplateObjectKey) == "" || strings.TrimSpace(contract.TemplateVersion) == "" {
		return CompleteStructureContract{}, ErrPromptTemplateNotFound
	}
	content, err := r.templates.GetTemplate(ctx, contract.TemplateObjectKey)
	if errors.Is(err, ErrPromptTemplateNotFound) {
		return CompleteStructureContract{}, ErrPromptTemplateNotFound
	}
	if err != nil {
		return CompleteStructureContract{}, err
	}
	return CompleteStructureContract{
		StructureContract: copyStructureContract(contract),
		Template: PromptTemplateObject{
			Doctype:   contract.Doctype,
			Version:   contract.TemplateVersion,
			ObjectKey: contract.TemplateObjectKey,
			Content:   string(content),
		},
	}, nil
}
