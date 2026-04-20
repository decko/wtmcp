package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const maxSchemaSize = 64 * 1024 // 64KB

var errPrinter = message.NewPrinter(language.English)

// CompiledSchema wraps a compiled JSON Schema for parameter validation.
type CompiledSchema struct {
	schema *jsonschema.Schema
}

// CompileParamsSchema compiles a tool's parameter schema for validation.
// Returns nil (no validation) if the schema is empty or cannot be compiled.
// Returns an error only if the schema is oversized.
func CompileParamsSchema(toolName string, toolDef ToolDef) (*CompiledSchema, error) {
	schemaMap := toolDef.ParamsSchema()
	schemaJSON, err := json.Marshal(schemaMap)
	if err != nil {
		log.Printf("[%s] schema marshal failed, skipping validation: %v", toolName, err)
		return nil, nil //nolint:nilerr // unparseable schema → skip validation
	}

	if len(schemaJSON) > maxSchemaSize {
		return nil, fmt.Errorf("tool %s: schema exceeds %d bytes", toolName, maxSchemaSize)
	}

	props, ok := schemaMap["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return nil, nil // no properties → nothing to validate
	}

	var schemaDoc any
	if err := json.Unmarshal(schemaJSON, &schemaDoc); err != nil {
		log.Printf("[%s] schema parse failed, skipping validation: %v", toolName, err)
		return nil, nil //nolint:nilerr // bad schema → skip validation
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(nil)
	if err := compiler.AddResource("schema.json", schemaDoc); err != nil {
		log.Printf("[%s] schema load failed, skipping validation: %v", toolName, err)
		return nil, nil //nolint:nilerr // bad schema → skip validation
	}

	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		log.Printf("[%s] schema compilation failed, skipping validation: %v", toolName, err)
		return nil, nil //nolint:nilerr // compilation error → skip validation
	}

	return &CompiledSchema{schema: compiled}, nil
}

// Validate checks params against the compiled schema. Returns nil if valid.
func (cs *CompiledSchema) Validate(params json.RawMessage) error {
	if cs == nil || cs.schema == nil {
		return nil
	}

	var v any
	if err := json.Unmarshal(params, &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	err := cs.schema.Validate(v)
	if err == nil {
		return nil
	}

	valErr := &jsonschema.ValidationError{}
	ok := errors.As(err, &valErr)
	if !ok {
		return fmt.Errorf("validation failed: %w", err)
	}

	return fmt.Errorf("invalid parameters: %s", formatValidationError(valErr))
}

func formatValidationError(err *jsonschema.ValidationError) string {
	var msgs []string
	collectErrors(err, &msgs)
	if len(msgs) == 0 {
		return err.Error()
	}
	return strings.Join(msgs, "; ")
}

func collectErrors(err *jsonschema.ValidationError, msgs *[]string) {
	if len(err.Causes) == 0 {
		loc := "/" + strings.Join(err.InstanceLocation, "/")
		*msgs = append(*msgs, fmt.Sprintf("%s: %s", loc, err.ErrorKind.LocalizedString(errPrinter)))
	}
	for _, cause := range err.Causes {
		collectErrors(cause, msgs)
	}
}
