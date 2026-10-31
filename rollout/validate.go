package rollout

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// A Validator checks a candidate file before any instance sees it.
type Validator interface {
	Name() string
	Validate(consumer string, data []byte) error
}

// formats maps each consumer to the file format it reads.
var formats = map[string]string{
	"bot-proxy": "bot-features/1",
	"go-policy": "quota-policy/1",
}

// FormatCheck is the minimum every strategy runs: the file parses as JSON
// and declares the format its consumer reads. It is B0's only check.
type FormatCheck struct{}

func (FormatCheck) Name() string { return "format" }

func (FormatCheck) Validate(consumer string, data []byte) error {
	want, ok := formats[consumer]
	if !ok {
		return fmt.Errorf("unknown consumer %q", consumer)
	}
	var head struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("not valid JSON: %v", err)
	}
	if head.Schema != want {
		return fmt.Errorf("declares format %q, %s reads %q", head.Schema, consumer, want)
	}
	return nil
}

//go:embed schemas/*.json
var schemaFS embed.FS

var schemaFiles = map[string]string{
	"bot-features/1": "schemas/bot-features.schema.json",
	"quota-policy/1": "schemas/quota-policy.schema.json",
}

var (
	compileOnce sync.Once
	compiled    map[string]*jsonschema.Schema
	compileErr  error
)

func schemas() (map[string]*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiled = map[string]*jsonschema.Schema{}
		c := jsonschema.NewCompiler()
		for format, path := range schemaFiles {
			raw, err := schemaFS.ReadFile(path)
			if err != nil {
				compileErr = err
				return
			}
			if err := c.AddResource(path, bytes.NewReader(raw)); err != nil {
				compileErr = err
				return
			}
			s, err := c.Compile(path)
			if err != nil {
				compileErr = fmt.Errorf("%s: %w", path, err)
				return
			}
			compiled[format] = s
		}
	})
	return compiled, compileErr
}

// SchemaCheck validates the file against the JSON Schema for its format
// (rollout/schemas). The schemas describe what the format promises, the way
// a typical team writes one. They do not encode any consumer release's
// limits. This is B1's extra check.
type SchemaCheck struct{}

func (SchemaCheck) Name() string { return "schema" }

func (SchemaCheck) Validate(consumer string, data []byte) error {
	all, err := schemas()
	if err != nil {
		return err
	}
	s := all[formats[consumer]]
	if s == nil {
		return fmt.Errorf("no schema for consumer %q", consumer)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("not valid JSON: %v", err)
	}
	if err := s.Validate(doc); err != nil {
		return fmt.Errorf("schema: %v", err)
	}
	return nil
}
