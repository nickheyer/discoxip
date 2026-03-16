package xap

import (
	"fmt"
	"io"
	"strings"
)

// PrettyPrint writes an indented representation of the scene.
func PrettyPrint(w io.Writer, scene *Scene) {
	for _, item := range scene.Items {
		switch item.Kind {
		case SNode:
			printNode(w, item.Node, 0)
		case SScript:
			fmt.Fprintf(w, "%s\n\n", item.Script)
		}
	}
}

func printNode(w io.Writer, n *Node, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)

	// USE reference placeholder
	if n.TypeName == "USE" {
		fmt.Fprintf(w, "%sUSE %s\n", indent, n.DefName)
		return
	}

	// Node header
	if n.DefName != "" {
		fmt.Fprintf(w, "%sDEF %s %s", indent, n.DefName, n.TypeName)
	} else {
		fmt.Fprintf(w, "%s%s", indent, n.TypeName)
	}

	// Check if node has any content
	hasContent := len(n.Fields) > 0 || len(n.Children) > 0 || len(n.Scripts) > 0
	if !hasContent {
		// Bare node (no braces in original) — just the type name
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintln(w, " {")

	// Fields in order
	for _, f := range n.Fields {
		printField(w, &f, depth+1)
	}

	// Scripts verbatim
	for _, s := range n.Scripts {
		fmt.Fprintf(w, "%s  %s\n", indent, s)
	}

	// Children
	if len(n.Children) > 0 {
		fmt.Fprintf(w, "%s  children [\n", indent)
		for _, child := range n.Children {
			printNode(w, child, depth+2)
		}
		fmt.Fprintf(w, "%s  ]\n", indent)
	}

	fmt.Fprintf(w, "%s}\n", indent)
}

func printField(w io.Writer, f *Field, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(w, "%s%s ", indent, f.Key)

	for i, v := range f.Values {
		if i > 0 {
			fmt.Fprint(w, " ")
		}
		printValue(w, &v, depth)
	}
	fmt.Fprintln(w)
}

func printValue(w io.Writer, v *Value, depth int) {
	switch v.Kind {
	case VNumber:
		fmt.Fprintf(w, "%g", v.Num)
	case VString:
		fmt.Fprintf(w, "%q", v.Str)
	case VBool:
		if v.Bool {
			fmt.Fprint(w, "TRUE")
		} else {
			fmt.Fprint(w, "FALSE")
		}
	case VIdent:
		fmt.Fprint(w, v.Str)
	case VScript:
		fmt.Fprintf(w, "{ %s }", v.Str)
	case VNode:
		if v.Node != nil {
			printInlineNode(w, v.Node, depth)
		}
	case VArray:
		fmt.Fprint(w, "[ ")
		for i, av := range v.Array {
			if i > 0 {
				fmt.Fprint(w, ", ")
			}
			printValue(w, &av, depth)
		}
		fmt.Fprint(w, " ]")
	}
}

func printInlineNode(w io.Writer, n *Node, depth int) {
	if n.DefName != "" {
		fmt.Fprintf(w, "DEF %s %s", n.DefName, n.TypeName)
	} else {
		fmt.Fprint(w, n.TypeName)
	}

	hasContent := len(n.Fields) > 0 || len(n.Children) > 0 || len(n.Scripts) > 0
	if !hasContent {
		return
	}

	indent := strings.Repeat("  ", depth)
	fmt.Fprintln(w, " {")
	for _, f := range n.Fields {
		printField(w, &f, depth+1)
	}
	for _, s := range n.Scripts {
		fmt.Fprintf(w, "%s  %s\n", indent, s)
	}
	if len(n.Children) > 0 {
		fmt.Fprintf(w, "%s  children [\n", indent)
		for _, child := range n.Children {
			printNode(w, child, depth+2)
		}
		fmt.Fprintf(w, "%s  ]\n", indent)
	}
	fmt.Fprintf(w, "%s}", indent)
}
