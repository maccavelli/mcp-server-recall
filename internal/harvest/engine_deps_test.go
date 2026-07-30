package harvest

import (
	"context"
	"go/types"
	"testing"
)

func TestResolveGoBin(t *testing.T) {
	// Simple test to ensure it runs without panicking
	t.Setenv("RECALL_GO_BIN", "/non/existent/go")
	_ = resolveGoBin()

	t.Setenv("RECALL_GO_BIN", "")
	_ = resolveGoBin()

	// Test goEnv
	t.Setenv("GOROOT", "/non/existent/goroot")
	_ = goEnv()
}

func TestExtractStructDeps(t *testing.T) {
	e := NewEngine()
	// create a dummy struct
	var fields []*types.Var
	// field of type string
	fields = append(fields, types.NewField(0, nil, "StringField", types.Typ[types.String], false))

	s := types.NewStruct(fields, nil)
	deps := e.extractStructDeps(s)

	if len(deps) != 0 {
		// basic types don't have deps
		t.Errorf("expected 0 deps for string field, got %d", len(deps))
	}
}

func TestSetupRemoteHarvestDir(t *testing.T) {
	e := NewEngine()
	// test with invalid package
	_, cleanup, ok := e.setupRemoteHarvestDir(context.Background(), "./local")
	if ok {
		t.Errorf("expected setupRemoteHarvestDir to return false for local package")
	}
	if cleanup != nil {
		cleanup()
	}
}

func TestExtractDeps(t *testing.T) {
	e := NewEngine()
	deps := make(map[string]bool)

	pkg := types.NewPackage("example.com/test", "test")
	obj := types.NewTypeName(0, pkg, "MyType", nil)
	named := types.NewNamed(obj, types.Typ[types.String], nil)

	// Test Named
	e.extractDeps(named, deps)

	// Test Pointer
	ptr := types.NewPointer(named)
	e.extractDeps(ptr, deps)

	// Test Slice
	slice := types.NewSlice(named)
	e.extractDeps(slice, deps)

	// Test Array
	arr := types.NewArray(named, 5)
	e.extractDeps(arr, deps)

	// Test Map
	mapType := types.NewMap(types.Typ[types.String], named)
	e.extractDeps(mapType, deps)

	// Test Chan
	chanType := types.NewChan(types.SendRecv, named)
	e.extractDeps(chanType, deps)

	// Test Signature
	params := types.NewTuple(types.NewVar(0, nil, "p1", named))
	results := types.NewTuple(types.NewVar(0, nil, "r1", named))
	sig := types.NewSignatureType(nil, nil, nil, params, results, false)
	e.extractDeps(sig, deps)

	if len(deps) == 0 {
		t.Errorf("expected deps to be extracted, got 0")
	}
}
