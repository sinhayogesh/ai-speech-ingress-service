package schema

import "log"

type Validator struct{}

func New() *Validator {
	return &Validator{}
}

func (v *Validator) Validate(event any) error {
	// Phase 1: stubbed
	// Phase 2: plug JSON Schema validator here
	log.Printf("schema validated: %+v", event)
	return nil
}
