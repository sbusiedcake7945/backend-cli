package interpreter

import (
	"backend-dsl/parser"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Interpreter struct {
	port         string
	startMessage string
	variables    map[string]string
}

func NewInterpreter() *Interpreter {
	return &Interpreter{
		port:         "8080",
		startMessage: "Server starting...",
		variables:    make(map[string]string),
	}
}

func (i *Interpreter) Run(root *parser.Node) error {
	i.traverseAndRun(root)

	startMsg := i.resolveVars(i.startMessage)
	finalPort := i.resolveVars(i.port)
	fmt.Printf("%s\n", startMsg)
	fmt.Printf("Listening on http://localhost:%s\n", finalPort)
	return http.ListenAndServe(":"+finalPort, nil)
}

func (i *Interpreter) traverseAndRun(node *parser.Node) {
	// Phase 1: Config and Top-level Variables
	for _, child := range node.Children {
		switch child.Type {
		case parser.NodeConfig:
			i.handleConfig(child)
		case parser.NodeVariable:
			i.handleVariable(child)
		case parser.NodeImport:
			i.handleImport(child)
		case parser.NodeDatabase:
			i.handleDatabase(child)
		case parser.NodeBackend:
			i.traverseAndRun(child) // antisocially.
		}
	}

	// Phase 2: Routes and other logic
	for _, child := range node.Children {
		if child.Type == parser.NodeRoute {
			i.handleRoute(child)
		} else if child.Type == parser.NodeHttp {
			i.handleHttp(child)
		}
	}
}

func (i *Interpreter) handleVariable(node *parser.Node) {
	name := node.Attributes["name"]
	value := node.Attributes["value"]
	if name != "" {
		i.variables[name] = i.resolveVars(value)
	}
}

func (i *Interpreter) handleHttp(node *parser.Node) {
	method := node.Attributes["method"]
	if method == "" {
		method = "GET"
	}
	url := i.resolveVars(node.Attributes["url"])
	varName := node.Attributes["var"]

	resp, err := http.Get(url)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if varName != "" {
			i.variables[varName] = string(body)
		}
	} else {
		if varName != "" {
			i.variables[varName] = "Error: " + err.Error()
		}
	}
}

func (i *Interpreter) handleConfig(node *parser.Node) {
	for _, child := range node.Children {
		switch child.Type {
		case parser.NodePort:
			i.port = strings.TrimSpace(child.Content)
		case parser.NodeStartMessage:
			i.startMessage = strings.TrimSpace(child.Content)
		}
	}
}

func (i *Interpreter) handleRoute(node *parser.Node) {
	path := i.resolveVars(node.Attributes["path"])
	method := strings.ToUpper(node.Attributes["method"])
	if method == "" {
		method = "GET"
	}

	var handlerNode *parser.Node
	for _, child := range node.Children {
		if child.Type == parser.NodeHandler {
			handlerNode = child
			break
		}
	}

	if handlerNode == nil {
		return
	}

	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method && method != "*" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		i.executeHandler(handlerNode, w, r)
	})
}

func (i *Interpreter) executeHandler(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	// HTML tag'i var mı kontrol et ve header'ı önceden ayarla
	for _, child := range node.Children {
		if child.Type == parser.NodeHtml {
			w.Header().Set("Content-Type", "text/html")
			break
		}
	}

	for _, child := range node.Children {
		switch child.Type {
		case parser.NodeLog:
			fmt.Println(i.resolveVars(child.Content))
		case parser.NodePrint:
			fmt.Fprint(w, i.resolveVars(child.Content))
		case parser.NodeHtml:
			fmt.Fprint(w, i.resolveVars(i.renderHTML(child)))
		case parser.NodeVariable:
			i.handleVariable(child)
		case parser.NodeHttp:
			i.handleHttp(child)
		case parser.NodeInput:
			prompt := i.resolveVars(child.Attributes["prompt"])
			if prompt != "" {
				fmt.Print(prompt)
			}
			var input string
			fmt.Scanln(&input)
			varName := child.Attributes["var"]
			if varName != "" {
				i.variables[varName] = input
			}
		case parser.NodeFunction:
			i.handleFunction(child, w, r) // antisocially.
		}
	}

	if node.Content != "" {
		fmt.Fprint(w, i.resolveVars(node.Content))
	}
}

func (i *Interpreter) handleImport(node *parser.Node) {
	path := i.resolveVars(node.Attributes["path"])
	if path == "" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		fmt.Printf("Import error: %v\n", err)
		return
	}

	if info.IsDir() {
		files, _ := filepath.Glob(filepath.Join(path, "*.backend"))
		for _, f := range files {
			i.importFile(f)
		}
	} else {
		i.importFile(path)
	}
}

func (i *Interpreter) importFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("Error opening import file %s: %v\n", path, err)
		return
	}
	defer file.Close()

	p := parser.NewParser(file)
	ast, err := p.Parse()
	if err != nil {
		fmt.Printf("Parse failed for import %s: %v\n", path, err)
		return
	}
	i.traverseAndRun(ast)
}

func (i *Interpreter) handleDatabase(node *parser.Node) {
	// Simple mock/sqlite setup for now
	// User said "problematic", let's ensure it doesn't crash
	conn := i.resolveVars(node.Attributes["connection"])
	fmt.Printf("Connecting to database: %s (Feature in development)\n", conn)
}

func (i *Interpreter) handleFunction(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	funcType := node.Attributes["type"]
	content := node.Content

	switch funcType {
	case "js":
		// Execute via node if available
		cmd := exec.Command("node", "-e", content)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprint(w, "JS Error: ", err, string(out))
		} else {
			fmt.Fprint(w, string(out))
		}
	case "go":
		// Execute via go run
		tmpFile := "tmp_func.go"
		goCode := "package main\nimport \"fmt\"\nfunc main() {\n" + content + "\n}"
		os.WriteFile(tmpFile, []byte(goCode), 0644)
		cmd := exec.Command("go", "run", tmpFile)
		out, err := cmd.CombinedOutput()
		os.Remove(tmpFile)
		if err != nil {
			fmt.Fprint(w, "Go Error: ", err, string(out))
		} else {
			fmt.Fprint(w, string(out))
		}
	case "html":
		// Just render as HTML
		fmt.Fprint(w, i.resolveVars(i.renderHTML(node)))
	}
}

func (i *Interpreter) resolveVars(input string) string {
	result := input
	for name, value := range i.variables {
		placeholder := "{{" + name + "}}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

func (i *Interpreter) renderHTML(node *parser.Node) string {
	var sb strings.Builder
	sb.WriteString("<" + node.Name)
	for k, v := range node.Attributes {
		sb.WriteString(fmt.Sprintf(" %s=\"%s\"", i.resolveVars(k), i.resolveVars(v)))
	}
	sb.WriteString(">")

	if node.Content != "" {
		sb.WriteString(i.resolveVars(node.Content))
	}

	for _, child := range node.Children {
		switch child.Type {
		case parser.NodeText:
			sb.WriteString(i.resolveVars(child.Content))
		default:
			sb.WriteString(i.renderHTML(child))
		}
	}

	sb.WriteString("</" + node.Name + ">")
	return sb.String()
}
