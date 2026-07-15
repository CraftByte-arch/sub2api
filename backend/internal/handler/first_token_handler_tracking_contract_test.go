package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayMessagesAbandonsBothLocalInterceptResponses(t *testing.T) {
	content, err := os.ReadFile("gateway_handler.go")
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(content), "firstTokenTracker.Abandon()"))
}

func TestFirstTokenRequestTrackingHooksPrecedeAccountSelection(t *testing.T) {
	tests := []struct {
		file     string
		function string
	}{
		{file: "gateway_handler_responses.go", function: "Responses"},
		{file: "gateway_handler_chat_completions.go", function: "ChatCompletions"},
		{file: "gateway_handler.go", function: "Messages"},
		{file: "openai_gateway_handler.go", function: "Responses"},
		{file: "openai_chat_completions.go", function: "ChatCompletions"},
		{file: "openai_gateway_handler.go", function: "Messages"},
	}
	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.function, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, nil, 0)
			require.NoError(t, err)

			var function *ast.FuncDecl
			for _, decl := range file.Decls {
				candidate, ok := decl.(*ast.FuncDecl)
				if ok && candidate.Recv != nil && candidate.Name.Name == tt.function {
					function = candidate
					break
				}
			}
			require.NotNil(t, function)

			var hookPos token.Pos
			var selectionPos token.Pos
			hasDeferredFinish := false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CallExpr:
					if ident, ok := n.Fun.(*ast.Ident); ok && ident.Name == "beginFirstTokenRequestTracking" && hookPos == token.NoPos {
						hookPos = n.Pos()
					}
					if selector, ok := n.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(selector.Sel.Name, "SelectAccount") && selectionPos == token.NoPos {
						selectionPos = n.Pos()
					}
				case *ast.DeferStmt:
					if selector, ok := n.Call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Finish" {
						hasDeferredFinish = true
					}
				}
				return true
			})

			require.NotEqual(t, token.NoPos, hookPos)
			require.NotEqual(t, token.NoPos, selectionPos)
			require.Less(t, int(hookPos), int(selectionPos))
			require.True(t, hasDeferredFinish)
		})
	}
}
