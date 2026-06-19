package draft

import (
	"errors"
	"fmt"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

var ErrScenarioNotForC05 = errors.New("scenario context target capability is not c05")

type C05ScenarioContext struct {
	TargetCapability     doctype.TargetCapability
	Doctype              string
	Subtype              string
	Direction            doctype.WritingDirection
	Confidence           float64
	SceneDescription     string
	FilledSlots          map[doctype.RequiredSlot]string
	MissingSlots         []doctype.RequiredSlot
	ContentSecurityLevel llm.ContentSecurityLevel
}

func ConsumeC06ScenarioContext(scenario doctype.ScenarioContext) (C05ScenarioContext, error) {
	if scenario.TargetCapability != doctype.CapabilityC05 {
		return C05ScenarioContext{}, fmt.Errorf("%w: %s", ErrScenarioNotForC05, scenario.TargetCapability)
	}
	filled := make(map[doctype.RequiredSlot]string, len(scenario.FilledSlots))
	for slot, value := range scenario.FilledSlots {
		filled[slot] = value
	}
	missing := make([]doctype.RequiredSlot, len(scenario.MissingSlots))
	copy(missing, scenario.MissingSlots)

	return C05ScenarioContext{
		TargetCapability:     scenario.TargetCapability,
		Doctype:              scenario.Doctype,
		Subtype:              scenario.Subtype,
		Direction:            scenario.Direction,
		Confidence:           scenario.Confidence,
		SceneDescription:     scenario.SceneDescription,
		FilledSlots:          filled,
		MissingSlots:         missing,
		ContentSecurityLevel: scenario.ContentSecurityLevel,
	}, nil
}
