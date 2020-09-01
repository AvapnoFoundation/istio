// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	bytes "bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/ghodss/yaml"
	gogojsonpb "github.com/gogo/protobuf/jsonpb"
	gogoproto "github.com/gogo/protobuf/proto"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/protobuf/reflect/protoreflect"

	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/pkg/util/protomarshal"
)

// Meta is metadata attached to each configuration unit.
// The revision is optional, and if provided, identifies the
// last update operation on the object.
type Meta struct {
	// GroupVersionKind is a short configuration name that matches the content message type
	// (e.g. "route-rule")
	GroupVersionKind GroupVersionKind `json:"type,omitempty"`

	// Name is a unique immutable identifier in a namespace
	Name string `json:"name,omitempty"`

	// Namespace defines the space for names (optional for some types),
	// applications may choose to use namespaces for a variety of purposes
	// (security domains, fault domains, organizational domains)
	Namespace string `json:"namespace,omitempty"`

	// Domain defines the suffix of the fully qualified name past the namespace.
	// Domain is not a part of the unique key unlike name and namespace.
	Domain string `json:"domain,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// ResourceVersion is an opaque identifier for tracking updates to the config registry.
	// The implementation may use a change index or a commit log for the revision.
	// The config client should not make any assumptions about revisions and rely only on
	// exact equality to implement optimistic concurrency of read-write operations.
	//
	// The lifetime of an object of a particular revision depends on the underlying data store.
	// The data store may compactify old revisions in the interest of storage optimization.
	//
	// An empty revision carries a special meaning that the associated object has
	// not been stored and assigned a revision.
	ResourceVersion string `json:"resourceVersion,omitempty"`

	// CreationTimestamp records the creation time
	CreationTimestamp time.Time `json:"creationTimestamp,omitempty"`
}

// Config is a configuration unit consisting of the type of configuration, the
// key identifier that is unique per type, and the content represented as a
// protobuf message.
type Config struct {
	Meta

	// Spec holds the configuration object as a gogo protobuf message
	Spec Spec
}

// Spec defines the spec for the config. In order to use below helper methods,
// this must be one of:
// * golang/protobuf Message
// * gogo/protobuf Message
// * Able to marshal/unmarshal using json
type Spec interface{}

func ToProtoGogo(s Spec) (*gogotypes.Any, error) {
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			golangany, err := ptypes.MarshalAny(pb)
			if err != nil {
				return nil, err
			}
			return &gogotypes.Any{
				TypeUrl: golangany.TypeUrl,
				Value:   golangany.Value,
			}, nil
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		return gogotypes.MarshalAny(pb)
	}

	js, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	pbs := &gogotypes.Struct{}
	if err := gogojsonpb.Unmarshal(bytes.NewReader(js), pbs); err != nil {
		return nil, err
	}
	return gogotypes.MarshalAny(pbs)
}

func ToMap(s Spec) (map[string]interface{}, error) {
	js, err := ToJSON(s)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	var data map[string]interface{}
	err = json.Unmarshal(js, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func ToJSON(s Spec) ([]byte, error) {
	b := &bytes.Buffer{}
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			err := (&jsonpb.Marshaler{}).Marshal(b, pb)
			return b.Bytes(), err
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		err := (&gogojsonpb.Marshaler{}).Marshal(b, pb)
		return b.Bytes(), err
	}

	return json.Marshal(s)
}

type deepCopier interface {
	DeepCopyInterface() interface{}
}

func ApplyYAML(s Spec, yml string) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSON(s, string(js))
}

func ApplyJSONStrict(s Spec, js string) error {
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			err := protomarshal.ApplyJSONStrict(js, pb)
			return err
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		err := gogoprotomarshal.ApplyJSONStrict(js, pb)
		return err
	}

	d := json.NewDecoder(bytes.NewReader([]byte(js)))
	d.DisallowUnknownFields()
	return d.Decode(&s)
}

func ApplyJSON(s Spec, js string) error {
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			err := protomarshal.ApplyJSON(js, pb)
			return err
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		err := gogoprotomarshal.ApplyJSON(js, pb)
		return err
	}

	return json.Unmarshal([]byte(js), &s)
}

func DeepCopy(s Spec) Spec {
	// If deep copy is defined, use that
	if dc, ok := s.(deepCopier); ok {
		return dc.DeepCopyInterface()
	}

	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			return proto.Clone(pb)
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		return gogoproto.Clone(pb)
	}

	// If we don't have a deep copy method, we will have to do some reflection magic. Its not ideal,
	// but all Istio types have an efficient deep copy.
	js, err := json.Marshal(s)
	if err != nil {
		return nil
	}

	data := reflect.New(reflect.TypeOf(s).Elem()).Interface()
	err = json.Unmarshal(js, &data)
	if err != nil {
		return nil
	}

	return data
}

// Key function for the configuration objects
func Key(typ, name, namespace string) string {
	return fmt.Sprintf("%s/%s/%s", typ, namespace, name)
}

// Key is the unique identifier for a configuration object
// TODO: this is *not* unique - needs the version and group
func (meta *Meta) Key() string {
	return Key(meta.GroupVersionKind.Kind, meta.Name, meta.Namespace)
}

func (c Config) DeepCopy() Config {
	var clone Config
	clone.Meta = c.Meta
	if c.Labels != nil {
		clone.Labels = make(map[string]string)
		for k, v := range c.Labels {
			clone.Labels[k] = v
		}
	}
	if c.Annotations != nil {
		clone.Annotations = make(map[string]string)
		for k, v := range c.Annotations {
			clone.Annotations[k] = v
		}
	}
	clone.Spec = DeepCopy(c.Spec)
	return clone
}

var _ fmt.Stringer = GroupVersionKind{}

type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func (g GroupVersionKind) String() string {
	if g.Group == "" {
		return "core/" + g.Version + "/" + g.Kind
	}
	return g.Group + "/" + g.Version + "/" + g.Kind
}
