package registry

import (
	"encoding"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Node describes the JSON of one Go type.
type Node struct {
	// Kind is object, array, map, string, number, integer, boolean, null,
	// ref (a schema of api/openapi.json) or any. Enum lists the values a
	// string, or a boolean that is always the same, can have.
	Kind     string   `json:"kind"`
	Format   string   `json:"format,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	Nullable bool     `json:"nullable,omitempty"`
	Ref      string   `json:"ref,omitempty"`
	Items    *Node    `json:"items,omitempty"`
	Fields   []Field  `json:"fields,omitempty"`
}

// Field is one key of an object, in the order it is written.
type Field struct {
	Name     string `json:"name"`
	Optional bool   `json:"optional,omitempty"`
	Type     Node   `json:"type"`
}

// Shape is one Go type that an event name is published as.
type Shape struct {
	GoType string `json:"go"`
	Type   Node   `json:"type"`
}

// Document is api/events.json: the shapes of every server event by name.
type Document struct {
	Comment string             `json:"comment"`
	Events  map[string][]Shape `json:"events"`
}

var (
	uuidType      = reflect.TypeFor[uuid.UUID]()
	timeType      = reflect.TypeFor[time.Time]()
	rawType       = reflect.TypeFor[json.RawMessage]()
	marshalerType = reflect.TypeFor[json.Marshaler]()
	textType      = reflect.TypeFor[encoding.TextMarshaler]()
)

// Schema describes All. known reports whether api/openapi.json has a schema
// of that name: such a type is referred to instead of described again.
func Schema(known func(name string) bool) Document {
	doc := Document{
		Comment: "Generated from the Go event types by internal/events/registry; do not edit. " +
			"Regenerate: UPDATE_EVENT_SCHEMA=1 go test ./internal/events/registry",
		Events: map[string][]Shape{},
	}
	d := describer{known: known}
	for _, e := range All() {
		name := e.Event.EventName()
		doc.Events[name] = append(doc.Events[name], Shape{GoType: goName(reflect.TypeOf(e.Event)), Type: d.node(e.shape(), "")})
	}
	for _, shapes := range doc.Events {
		sort.Slice(shapes, func(i, j int) bool { return shapes[i].GoType < shapes[j].GoType })
	}
	return doc
}

// goName is "pkg.Type" of an event type.
func goName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.String()
}

type describer struct {
	known func(string) bool
}

// restName is the name api/openapi.json gives a named struct: its package and
// name, with every package below internal/chat called "chat".
func restName(t reflect.Type) string {
	pkg := t.PkgPath()
	if strings.Contains(pkg, "/internal/chat/") {
		return "chat." + t.Name()
	}
	return pkg[strings.LastIndex(pkg, "/")+1:] + "." + t.Name()
}

func (d describer) node(t reflect.Type, tag reflect.StructTag) Node {
	switch {
	case t == uuidType:
		return Node{Kind: "string", Format: "uuid"}
	case t == timeType:
		return Node{Kind: "string", Format: "date-time"}
	case t == rawType:
		return Node{Kind: "any"}
	case t.Kind() == reflect.Pointer:
		n := d.node(t.Elem(), tag)
		n.Nullable = true
		return n
	case t.Implements(marshalerType) || t.Implements(textType):
		return marshaled(t)
	}
	return d.byKind(t, tag)
}

// marshaled describes a type that writes its own JSON by what its zero value
// becomes: null for a signal, text for an enumeration.
func marshaled(t reflect.Type) Node {
	data, err := json.Marshal(reflect.Zero(t).Interface())
	switch {
	case err == nil && string(data) == "null":
		return Node{Kind: "null"}
	case err == nil && strings.HasPrefix(string(data), `"`):
		return Node{Kind: "string"}
	}
	return Node{Kind: "any"}
}

func (d describer) byKind(t reflect.Type, tag reflect.StructTag) Node {
	switch t.Kind() {
	case reflect.String:
		n := Node{Kind: "string"}
		if enum := tag.Get("enums"); enum != "" {
			n.Enum = strings.Split(enum, ",")
		}
		return n
	case reflect.Bool:
		n := Node{Kind: "boolean"}
		if only := tag.Get("enums"); only != "" {
			n.Enum = []string{only} // a flag that is always true, or always false
		}
		return n
	case reflect.Float32, reflect.Float64:
		return Node{Kind: "number"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Node{Kind: "integer"}
	case reflect.Slice, reflect.Array:
		items := d.node(t.Elem(), "")
		return Node{Kind: "array", Items: &items, Nullable: strings.Contains(tag.Get("extensions"), "x-nullable")}
	case reflect.Map:
		items := d.node(t.Elem(), "")
		return Node{Kind: "map", Items: &items}
	case reflect.Struct:
		if name := restName(t); t.Name() != "" && d.known(name) {
			return Node{Kind: "ref", Ref: name}
		}
		return Node{Kind: "object", Fields: d.fields(t)}
	}
	return Node{Kind: "any"}
}

// fields lists the keys of a struct as encoding/json writes them: in
// declaration order, embedded structs flattened, unexported and "-" skipped.
func (d describer) fields(t reflect.Type) []Field {
	var out []Field
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case flattened(f, name):
			out = append(out, d.fields(f.Type)...)
		case f.IsExported() && name != "-":
			out = append(out, d.field(f, name, strings.Contains(opts, "omitempty")))
		}
	}
	return out
}

// flattened reports whether f is an embedded struct whose keys appear in the
// object that embeds it.
func flattened(f reflect.StructField, jsonName string) bool {
	return f.Anonymous && jsonName == "" && f.Type.Kind() == reflect.Struct && !f.Type.Implements(marshalerType)
}

func (d describer) field(f reflect.StructField, name string, optional bool) Field {
	if name == "" {
		name = f.Name
	}
	node := d.node(f.Type, f.Tag)
	if optional {
		node.Nullable = false // an omitted pointer is never written as null
	}
	return Field{Name: name, Optional: optional, Type: node}
}
