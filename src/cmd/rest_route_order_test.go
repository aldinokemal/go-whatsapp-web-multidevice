package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDeviceGroupIsRegisteredLast pins the ordering invariant in restServer.
//
// apiGroup.Group("", DeviceMiddleware(dm)) does not scope the middleware to
// the returned group: fiber registers it as a root "use" route that matches
// every path, and the route stack is walked in registration order. So every
// route mounted on apiGroup *after* that call silently inherits
// DeviceMiddleware — which is what broke the chatwoot config endpoints in
// multi-device setups (#837).
//
// restServer builds a live listener, so there is no runtime seam to assert
// this on. The invariant is therefore checked against the source: the device
// group must be the last thing registered on apiGroup.
func TestDeviceGroupIsRegisteredLast(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "rest.go", nil, 0)
	require.NoError(t, err)

	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "restServer" {
			body = fn.Body
			break
		}
	}
	require.NotNil(t, body, "restServer not found in rest.go")

	isAPIGroup := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "apiGroup"
	}

	var groupPos token.Pos
	type mount struct {
		pos  token.Pos
		desc string
	}
	var mounts []mount

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// apiGroup.<Method>(...)
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isAPIGroup(sel.X) {
			if sel.Sel.Name == "Group" {
				groupPos = call.Pos()
			} else {
				mounts = append(mounts, mount{call.Pos(), "apiGroup." + sel.Sel.Name + "(...)"})
			}
			return true
		}
		// f(apiGroup, ...) — InitRest*, uimcp.Register, registerUIRoute
		for _, arg := range call.Args {
			if isAPIGroup(arg) {
				mounts = append(mounts, mount{call.Pos(), callName(call) + "(apiGroup, ...)"})
				break
			}
		}
		return true
	})

	require.NotZero(t, groupPos, `apiGroup.Group("", DeviceMiddleware(dm)) not found in restServer`)
	require.NotEmpty(t, mounts, "no apiGroup registrations found — test is not checking anything")

	for _, m := range mounts {
		require.Less(t, int(m.pos), int(groupPos),
			"%s is registered at %s, after the device group at %s — it will inherit DeviceMiddleware. "+
				"Move it above the apiGroup.Group(\"\", ...) call.",
			m.desc, fset.Position(m.pos), fset.Position(groupPos))
	}
}

func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return "call"
}
