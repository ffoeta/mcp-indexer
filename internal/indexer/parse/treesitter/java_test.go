package treesitter

import (
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/indexer/parse"
	"path/filepath"
	"testing"
)

func parseJava(t *testing.T, content string) *parse.ParseResult {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "X.java")
	writeFile(t, f, content)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return res
}

func findJavaCall(res *parse.ParseResult, callerFQN, calleeName, calleeOwner string) *parse.CallRef {
	for i := range res.Calls {
		c := &res.Calls[i]
		if c.CallerFQN == callerFQN && c.CalleeName == calleeName && c.CalleeOwner == calleeOwner {
			return c
		}
	}
	return nil
}

func findJavaVarType(res *parse.ParseResult, scope, varName, typeName string) bool {
	for _, vt := range res.VarTypes {
		if vt.ScopeFQN == scope && vt.VarName == varName && vt.TypeName == typeName {
			return true
		}
	}
	return false
}

// ───────── package + imports ─────────

func TestJava_PackageAndImports(t *testing.T) {
	res := parseJava(t, `
package com.example.svc;
import com.example.repo.OrderRepo;
import java.util.List;
import java.util.*;
import static com.x.Helpers.helper;

public class A {}
`)
	if res.Package != "com.example.svc" {
		t.Errorf("package=%q", res.Package)
	}
	wantImports := []string{
		"com.example.repo.OrderRepo",
		"java.util.List",
		"java.util",
		"com.x.Helpers.helper",
	}
	for _, w := range wantImports {
		found := false
		for _, i := range res.Imports {
			if i.Raw == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing import %q in %+v", w, res.Imports)
		}
	}
}

// ───────── objects ─────────

func TestJava_Class_FQN_UsesPackage(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class Foo {}
`)
	if len(res.Objects) != 1 || res.Objects[0].FQN != "com.x.Foo" {
		t.Errorf("expected com.x.Foo, got %+v", res.Objects)
	}
}

func TestJava_Object_Subkinds(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class C {}
public interface I {}
public enum E { ONE }
`)
	got := map[string]string{}
	for _, o := range res.Objects {
		got[o.Name] = o.Subkind
	}
	if got["C"] != store.SubClass || got["I"] != store.SubInterface || got["E"] != store.SubEnum {
		t.Errorf("subkinds wrong: %+v", got)
	}
}

func TestJava_Object_ExtendsAndImplements(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class Cat extends Animal implements Pettable, Cuddly {}
`)
	if len(res.Objects) != 1 {
		t.Fatalf("objects=%+v", res.Objects)
	}
	bases := res.Objects[0].Bases
	rels := map[string]string{}
	for _, b := range bases {
		rels[b.Name] = b.Relation
	}
	if rels["Animal"] != store.RelExtends {
		t.Errorf("Animal should be extends: %+v", bases)
	}
	if rels["Pettable"] != store.RelImplements || rels["Cuddly"] != store.RelImplements {
		t.Errorf("Pettable/Cuddly should be implements: %+v", bases)
	}
}

func TestJava_NestedClass_FQN(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class Outer {
    public static class Inner {
        public void f() {}
    }
}
`)
	hasInner := false
	hasInnerF := false
	for _, o := range res.Objects {
		if o.FQN == "com.x.Outer.Inner" {
			hasInner = true
		}
	}
	for _, m := range res.Methods {
		if m.FQN == "com.x.Outer.Inner.f" && m.OwnerFQN == "com.x.Outer.Inner" {
			hasInnerF = true
		}
	}
	if !hasInner || !hasInnerF {
		t.Errorf("nested missing: objects=%+v methods=%+v", res.Objects, res.Methods)
	}
}

func TestJava_Object_Javadoc(t *testing.T) {
	res := parseJava(t, `
package com.x;

/**
 * Service for processing orders.
 * Multi-line.
 */
public class OrderService {}
`)
	if len(res.Objects) != 1 {
		t.Fatalf("objects=%+v", res.Objects)
	}
	if res.Objects[0].Doc != "Service for processing orders." {
		t.Errorf("doc=%q", res.Objects[0].Doc)
	}
}

// ───────── methods ─────────

func TestJava_Method_OwnerAndScope(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    public A() {}
    public void run() {}
}
`)
	var ctor, run *parse.MethodDef
	for i := range res.Methods {
		if res.Methods[i].Name == "A" {
			ctor = &res.Methods[i]
		}
		if res.Methods[i].Name == "run" {
			run = &res.Methods[i]
		}
	}
	if ctor == nil || ctor.OwnerFQN != "com.x.A" || ctor.Subkind != store.SubCtor {
		t.Errorf("ctor wrong: %+v", ctor)
	}
	if run == nil || run.OwnerFQN != "com.x.A" || run.Subkind != store.SubMethod || run.Scope != store.ScopeMember {
		t.Errorf("run wrong: %+v", run)
	}
}

// ───────── calls ─────────

func TestJava_Calls_BareIntraClass(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        helper();
    }
    void helper() {}
}
`)
	if c := findJavaCall(res, "com.x.A.run", "helper", ""); c == nil {
		t.Errorf("bare intra-class call missing: %+v", res.Calls)
	}
}

func TestJava_Calls_ThisReceiver(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        this.helper();
    }
    void helper() {}
}
`)
	if c := findJavaCall(res, "com.x.A.run", "helper", ""); c == nil {
		t.Errorf("this.helper() call missing: %+v", res.Calls)
	}
}

func TestJava_Calls_ImportedReceiver(t *testing.T) {
	res := parseJava(t, `
package com.x;
import com.y.OrderRepo;
public class A {
    void run() {
        OrderRepo r = new OrderRepo();
        r.save(null);
    }
}
`)
	// Через VarType: r → OrderRepo. Сам call записан с CalleeOwner="r", резолвится в Pass 2 engine.
	if c := findJavaCall(res, "com.x.A.run", "save", "r"); c == nil {
		t.Errorf("call r.save missing: %+v", res.Calls)
	}
	if !findJavaVarType(res, "com.x.A.run", "r", "OrderRepo") {
		t.Errorf("var-type r:OrderRepo missing: %+v", res.VarTypes)
	}
}

func TestJava_Calls_StaticClassReceiver(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        Helper.staticOne();
    }
}
`)
	if c := findJavaCall(res, "com.x.A.run", "staticOne", "Helper"); c == nil {
		t.Errorf("Helper.staticOne() missing: %+v", res.Calls)
	}
}

func TestJava_Calls_ObjectCreation(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        new OrderRepo();
    }
}
`)
	if c := findJavaCall(res, "com.x.A.run", "OrderRepo", ""); c == nil {
		t.Errorf("new OrderRepo() missing: %+v", res.Calls)
	}
}

func TestJava_Calls_ObjectCreation_Scoped(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        new com.y.OrderRepo();
    }
}
`)
	if c := findJavaCall(res, "com.x.A.run", "OrderRepo", "com.y"); c == nil {
		t.Errorf("new com.y.OrderRepo() missing: %+v", res.Calls)
	}
}

func TestJava_Calls_ChainedNew(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        new Logger().info("hi");
    }
}
`)
	// owner для info — тип создаваемого объекта.
	if c := findJavaCall(res, "com.x.A.run", "info", "Logger"); c == nil {
		t.Errorf("new Logger().info missing: %+v", res.Calls)
	}
}

// ───────── var-types ─────────

func TestJava_VarType_Field(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    private OrderRepo repo;
}
`)
	if !findJavaVarType(res, "com.x.A", "repo", "OrderRepo") {
		t.Errorf("field var-type repo:OrderRepo missing: %+v", res.VarTypes)
	}
}

func TestJava_VarType_FormalParam(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void handle(Request req, int count) {}
}
`)
	if !findJavaVarType(res, "com.x.A.handle", "req", "Request") {
		t.Errorf("param req:Request missing: %+v", res.VarTypes)
	}
	if !findJavaVarType(res, "com.x.A.handle", "count", "int") {
		t.Errorf("param count:int missing: %+v", res.VarTypes)
	}
}

func TestJava_VarType_LocalDecl(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        OrderRepo r = new OrderRepo();
        int n = 0;
    }
}
`)
	if !findJavaVarType(res, "com.x.A.run", "r", "OrderRepo") {
		t.Errorf("local r:OrderRepo missing: %+v", res.VarTypes)
	}
	if !findJavaVarType(res, "com.x.A.run", "n", "int") {
		t.Errorf("local n:int missing: %+v", res.VarTypes)
	}
}

func TestJava_VarType_Generics_StripsTypeArgs(t *testing.T) {
	res := parseJava(t, `
package com.x;
import java.util.List;
public class A {
    void run() {
        List<String> xs;
    }
}
`)
	if !findJavaVarType(res, "com.x.A.run", "xs", "List") {
		t.Errorf("generic List<String> should reduce to List: %+v", res.VarTypes)
	}
}

// ───────── error paths ─────────

func TestJava_EmptyFile(t *testing.T) {
	res := parseJava(t, "")
	if len(res.Objects) != 0 || len(res.Methods) != 0 {
		t.Errorf("empty file: %+v", res)
	}
}

// ───────── synthetic <init> ─────────

func TestJava_SyntheticInit_FieldInitializer(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    private Logger log = new Logger();
}
`)
	// synthetic method присутствует
	var found *parse.MethodDef
	for i := range res.Methods {
		if res.Methods[i].FQN == "com.x.A."+parse.SyntheticInitName {
			found = &res.Methods[i]
		}
	}
	if found == nil {
		t.Fatalf("synthetic <init> missing: %+v", res.Methods)
	}
	if found.OwnerFQN != "com.x.A" || found.Subkind != store.SubInit {
		t.Errorf("init metadata wrong: %+v", found)
	}
	// new Logger() атрибутирован к <init>
	if c := findJavaCall(res, "com.x.A."+parse.SyntheticInitName, "Logger", ""); c == nil {
		t.Errorf("new Logger() not attributed to <init>: %+v", res.Calls)
	}
}

func TestJava_SyntheticInit_StaticInitializer(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    static {
        Setup.run();
    }
}
`)
	if c := findJavaCall(res, "com.x.A."+parse.SyntheticInitName, "run", "Setup"); c == nil {
		t.Errorf("Setup.run() in static{} not attributed to <init>: %+v", res.Calls)
	}
}

func TestJava_SyntheticInit_InstanceBlock(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void helper() {}
    {
        helper();
    }
}
`)
	if c := findJavaCall(res, "com.x.A."+parse.SyntheticInitName, "helper", ""); c == nil {
		t.Errorf("instance-block call not attributed to <init>: %+v", res.Calls)
	}
}

func TestJava_SyntheticInit_NotCreatedWhenNoInitializers(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    private int x;
    void foo() {}
}
`)
	for _, m := range res.Methods {
		if m.Name == parse.SyntheticInitName {
			t.Errorf("synthetic <init> should not exist when no initializer-calls: %+v", res.Methods)
		}
	}
}

func TestJava_Calls_Deduplicated(t *testing.T) {
	res := parseJava(t, `
package com.x;
public class A {
    void run() {
        helper();
        helper();
        helper();
    }
    void helper() {}
}
`)
	count := 0
	for _, c := range res.Calls {
		if c.CallerFQN == "com.x.A.run" && c.CalleeName == "helper" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dedup failed: %d entries: %+v", count, res.Calls)
	}
}
