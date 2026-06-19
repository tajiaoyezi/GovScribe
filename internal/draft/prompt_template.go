package draft

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const DefaultPromptTemplateVersion = "v1"

var ErrPromptTemplateNotFound = errors.New("prompt template object not found")

type PromptTemplateObject struct {
	Doctype   string
	Version   string
	ObjectKey string
	Content   string
}

type PromptTemplateObjectWriter interface {
	PutTemplate(context.Context, string, []byte) error
}

type PromptTemplateObjectReader interface {
	GetTemplate(context.Context, string) ([]byte, error)
}

func DefaultPromptTemplateObjects() []PromptTemplateObject {
	contracts := DefaultStructureContracts()
	templates := make([]PromptTemplateObject, 0, len(contracts))
	for _, contract := range contracts {
		templates = append(templates, PromptTemplateObject{
			Doctype:   contract.Doctype,
			Version:   DefaultPromptTemplateVersion,
			ObjectKey: mustPromptTemplateObjectKey(contract.Doctype, DefaultPromptTemplateVersion),
			Content:   BuildPromptTemplateContent(contract),
		})
	}
	return templates
}

func PromptTemplateObjectKey(doctypeName, version string) (string, error) {
	slug, ok := promptTemplateSlugs()[strings.TrimSpace(doctypeName)]
	if !ok || strings.TrimSpace(version) == "" {
		return "", ErrPromptTemplateNotFound
	}
	return "draft/templates/high-frequency/" + slug + "/" + strings.TrimSpace(version) + ".md", nil
}

func SeedPromptTemplateObjects(ctx context.Context, store PromptTemplateObjectWriter, templates []PromptTemplateObject) error {
	if store == nil {
		return errors.New("prompt template object store is required")
	}
	for _, tpl := range templates {
		if strings.TrimSpace(tpl.ObjectKey) == "" {
			return fmt.Errorf("%w: empty object key for %s", ErrPromptTemplateNotFound, tpl.Doctype)
		}
		if err := store.PutTemplate(ctx, tpl.ObjectKey, []byte(tpl.Content)); err != nil {
			return err
		}
	}
	return nil
}

func GetPromptTemplateObject(ctx context.Context, store PromptTemplateObjectReader, doctypeName, version string) (PromptTemplateObject, error) {
	if store == nil {
		return PromptTemplateObject{}, errors.New("prompt template object reader is required")
	}
	key, err := PromptTemplateObjectKey(doctypeName, version)
	if err != nil {
		return PromptTemplateObject{}, err
	}
	content, err := store.GetTemplate(ctx, key)
	if errors.Is(err, ErrPromptTemplateNotFound) {
		return PromptTemplateObject{}, ErrPromptTemplateNotFound
	}
	if err != nil {
		return PromptTemplateObject{}, err
	}
	return PromptTemplateObject{
		Doctype:   strings.TrimSpace(doctypeName),
		Version:   strings.TrimSpace(version),
		ObjectKey: key,
		Content:   string(content),
	}, nil
}

func BuildPromptTemplateContent(contract StructureContract) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(contract.Doctype)
	b.WriteString("提示模板\n\n")
	b.WriteString("## 标题构成\n")
	b.WriteString(contract.TitleRule)
	b.WriteString("\n\n")
	b.WriteString("## 称谓\n")
	b.WriteString(contract.SalutationRule)
	b.WriteString("\n\n")
	b.WriteString("## 主送机关\n")
	b.WriteString(contract.RecipientRule)
	b.WriteString("\n\n")
	b.WriteString("## 正文段落结构与必备要素\n")
	writeBulletList(&b, contract.BodyStructure)
	b.WriteString("\n必备要素：")
	b.WriteString(strings.Join(requiredSlotsToStrings(contract.RequiredSlots), "、"))
	b.WriteString("\n\n")
	b.WriteString("## 落款\n")
	b.WriteString(contract.SignatureRule)
	b.WriteString("\n\n")
	b.WriteString("## 口吻指令\n")
	writeBulletList(&b, contract.ToneRules)
	if strings.TrimSpace(contract.ClosingRule) != "" {
		b.WriteString("\n结束语约束：")
		b.WriteString(contract.ClosingRule)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString("## Few-shot 样例编排\n")
	b.WriteString("- 仅使用 c03 检索接口返回的同文种脱敏范文样例；存在子类元数据时优先使用同子类样例。\n")
	b.WriteString("- 样例与待写场景要素分区呈现，样例不得被臆造、还原或改写。\n")
	b.WriteString("- 注入条数不得超过调用方按契约设定的 TopK 上限。\n\n")
	b.WriteString("## 机关口径红线\n")
	writeBulletList(&b, contract.RedlineRules)
	return b.String()
}

func writeBulletList(b *strings.Builder, items []string) {
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}
}

func mustPromptTemplateObjectKey(doctypeName, version string) string {
	key, err := PromptTemplateObjectKey(doctypeName, version)
	if err != nil {
		panic(err)
	}
	return key
}

func promptTemplateSlugs() map[string]string {
	return map[string]string{
		"通知":   "tongzhi",
		"请示":   "qingshi",
		"报告":   "baogao",
		"函":    "han",
		"会议纪要": "huiyi-jiyao",
		"通报":   "tongbao",
		"批复":   "pifu",
		"讲话稿":  "jianghua-gao",
		"方案":   "fangan",
	}
}
