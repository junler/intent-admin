package capability

import (
	"github.com/google/jsonschema-go/jsonschema"
)

// schemaFor infers a JSON Schema from a Go type.
//
// This is isolated in one function because the same schema feeds three
// consumers — the OpenAPI document, the AI tool definition and the MCP tool
// definition — and they must never drift apart. Field descriptions come from
// the `jsonschema` struct tag.
//
// IgnoreInvalidTypes is on so that an output type containing something not
// expressible in JSON Schema degrades to a partial schema instead of refusing
// to register the capability. A nil schema simply means "undescribed".
func schemaFor[T any]() (*jsonschema.Schema, error) {
	s, err := jsonschema.For[T](&jsonschema.ForOptions{IgnoreInvalidTypes: true})
	if err != nil {
		// `any` outputs and similar unconstrained types land here. Treat them
		// as undescribed rather than fatal.
		return nil, nil //nolint:nilerr // intentional: undescribed, not failed
	}
	return s, nil
}

// Empty is the output type for capabilities that return nothing meaningful.
// Using a named type rather than `any` keeps the generated schema clean.
type Empty struct{}
