package v1

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/acorn-io/baaah/pkg/typed"
	"github.com/sirupsen/logrus"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

type GenericMap struct {
	// +optional
	Data map[string]any `json:"-"`
}

func (_ GenericMap) OpenAPIDefinition() common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			VendorExtensible: spec.VendorExtensible{
				Extensions: spec.Extensions{
					"x-kubernetes-preserve-unknown-fields": "true",
				},
			},
			SchemaProps: spec.SchemaProps{
				Type: []string{"object"},
			},
		},
	}
}

func (g *GenericMap) UnmarshalJSON(data []byte) error {
	if g == nil {
		return fmt.Errorf("%T: UnmarshalJSON on nil pointer", g)
	}

	dec := json.NewDecoder(bytes.NewBuffer(data))
	dec.UseNumber()

	d := map[string]any{}
	if err := dec.Decode(&d); err != nil {
		return fmt.Errorf("%T: Failed to decode data: %w", g, err)
	}

	if _, err := translateObject(d); err != nil {
		return fmt.Errorf("%T: Failed to translate object: %w", g, err)
	}

	if len(d) > 0 {
		// Consumers expect empty generic maps to have a nil Data field
		g.Data = d
	}

	return nil
}

// Merge merges the given map into this map, returning a new map, leaving the original unchanged.
func (g *GenericMap) Merge(from *GenericMap) *GenericMap {
	if from == nil || from.Data == nil {
		return g
	}

	if g == nil {
		g = &GenericMap{}
	}

	return &GenericMap{
		Data: typed.Concat(g.Data, from.Data),
	}
}

// MarshalJSON may get called on pointers or values, so implement MarshalJSON on value.
// http://stackoverflow.com/questions/21390979/custom-marshaljson-never-gets-called-in-go
func (g GenericMap) MarshalJSON() ([]byte, error) {
	//if g.Data == nil {
	//	return nil, nil
	//}

	// Encode unset/nil objects as JSON's "null"
	// if g.Data == nil {
	// 	return []byte("null"), nil
	// }

	return json.Marshal(g.Data)
}

func translateObject(data interface{}) (ret interface{}, err error) {
	switch t := data.(type) {
	case map[string]any:
		for k, v := range t {
			if t[k], err = translateObject(v); err != nil {
				return nil, err
			}
		}
	case []any:
		for i, v := range t {
			if t[i], err = translateObject(v); err != nil {
				return nil, err
			}
		}
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return i, nil
		}
		return t.Float64()
	}
	return data, nil
}

func (in *GenericMap) DeepCopyInto(out *GenericMap) {
	var err error
	if *out, err = Mapify(in.Data); err != nil {
		logrus.WithError(err).Errorf("failed to deep copy into [%T]", out)
	}
}

func Mapify(data any) (GenericMap, error) {
	marshaled, err := json.Marshal(data)
	if err != nil {
		return GenericMap{}, err
	}

	gm := &GenericMap{}
	if err := gm.UnmarshalJSON(marshaled); err != nil {
		return GenericMap{}, err
	}

	return *gm, nil
}
