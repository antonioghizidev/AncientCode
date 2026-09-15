package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type dumbTool struct {
	name string
}

func (d dumbTool) Name() string        { return d.name }
func (d dumbTool) Description() string { return "dumb" }
func (d dumbTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (dumbTool) Execute(context.Context, json.RawMessage) (Result, error) {
	return Result{}, nil
}

func TestValidateName(t *testing.T) {
	valid := []string{"read_file", "list_dir", "docker", "a", "a1_b2"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("name %q should be valid: %v", n, err)
		}
	}
	invalid := []string{"", "ReadFile", "1abc", "a-b", "a b", "a.b", "docker ps"}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("name %q should be invalid", n)
		}
	}
}

func TestRegistryRegisterLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dumbTool{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(dumbTool{name: "list_dir"}); err != nil {
		t.Fatal(err)
	}
	if !r.Has("read_file") || !r.Has("list_dir") || r.Has("nope") {
		t.Fatalf("Has() wrong; names=%v", r.Names())
	}
	if _, ok := r.Lookup("nope"); ok {
		t.Fatal("Lookup found unregistered tool")
	}
}

func TestRegistryRejectsInvalidAndDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dumbTool{name: "bad name"}); err == nil {
		t.Fatal("invalid name accepted")
	}
	if err := r.Register(dumbTool{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(dumbTool{name: "read_file"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := r.Register(nil); err == nil {
		t.Fatal("nil tool accepted")
	}
}

func TestRegistryDefs(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dumbTool{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	defs := r.Defs()
	if len(defs) != 1 || defs[0].Function.Name != "read_file" {
		t.Fatalf("defs = %+v", defs)
	}
}

func TestUnmarshalStrict(t *testing.T) {
	var v struct {
		Path string `json:"path"`
	}
	if err := UnmarshalStrict([]byte(`{"path":"a.txt"}`), &v); err != nil {
		t.Fatal(err)
	}
	// unknown field -> strict failure
	if err := UnmarshalStrict([]byte(`{"path":"a.txt","oops":1}`), &v); err == nil {
		t.Fatal("unknown field accepted")
	}
	// trailing data
	if err := UnmarshalStrict([]byte(`{"path":"a.txt"} extra`), &v); err == nil {
		t.Fatal("trailing data accepted")
	}
	// wrong type
	if err := UnmarshalStrict([]byte(`{"path":123}`), &v); err == nil {
		t.Fatal("wrong type accepted")
	}
}

func TestUnmarshalStrictPreservesLargeNumbers(t *testing.T) {
	// UseNumber guarantees precision for big ints instead of float mangling.
	var v struct {
		Val json.Number `json:"val"`
	}
	if err := UnmarshalStrict([]byte(`{"val":9007199254740993}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Val.String() != "9007199254740993" {
		t.Fatalf("number mangled: %s", v.Val.String())
	}
}
