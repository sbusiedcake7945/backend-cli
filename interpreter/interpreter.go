package interpreter

import (
	"backend-cli/parser"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/robertkrimen/otto"
	_ "modernc.org/sqlite"
)

type Interpreter struct {
	port           string
	startMessage   string
	variables      map[string]string
	db             *sql.DB
	namedFunctions map[string]*parser.Node
	jsEngine       *otto.Otto
	includeStack   []*parser.Node
	errorPages     map[int]string
	routes         []*Route
}

type Route struct {
	Path    string
	Pattern *regexp.Regexp
	Method  string
	Handler *parser.Node
}

func NewInterpreter() *Interpreter {
	i := &Interpreter{
		port:           "8080",
		startMessage:   "Server starting...",
		variables:      make(map[string]string),
		namedFunctions: make(map[string]*parser.Node),
		jsEngine:       otto.New(),
		errorPages:     make(map[int]string),
		routes:         make([]*Route, 0),
	}

	i.jsEngine.Set("render", func(call otto.FunctionCall) otto.Value {
		// This is a bit tricky since we need 'r'.
		// However, for variable resolution, we might not have 'r' easily.
		// For now, this is a placeholder to show the intent.
		return otto.Value{}
	})

	return i
}

func (i *Interpreter) Run(root *parser.Node) error {
	i.traverseAndRun(root, nil, nil)
	http.HandleFunc("/", i.dispatch)

	startMsg := i.resolveVars(i.startMessage)
	finalPort := i.resolveVars(i.port)
	fmt.Printf("%s\n", startMsg)
	fmt.Printf("Listening on http://localhost:%s\n", finalPort)
	return http.ListenAndServe(":"+finalPort, nil)
}

func (i *Interpreter) traverseAndRun(node *parser.Node, w http.ResponseWriter, r *http.Request) {
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
		case parser.NodeQuery:
			// Top-level queries for DB setup
			i.handleQuery(child, nil, nil)
		case parser.NodeFunction:
			// If it has a name, it's a global definition
			name := child.Attributes["name"]
			if name != "" {
				i.namedFunctions[name] = child
			}
		case parser.NodeBackend:
			i.traverseAndRun(child, w, r)
		case parser.NodeOpen:
			i.handleOpen(child)
		case parser.NodeInclude:
			i.handleInclude(child, nil, nil)
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
		case parser.NodeElement:
			if child.Name == "error_page" {
				code, _ := strconv.Atoi(child.Attributes["code"])
				file := child.Attributes["file"]
				if code != 0 && file != "" {
					i.errorPages[code] = file
				}
			}
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

	// Transform :param into regex ([^/]+)
	parts := strings.Split(path, "/")
	var regParts []string
	for _, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			name := part[1:]
			regParts = append(regParts, "(?P<"+name+">[^/]+)")
		} else {
			regParts = append(regParts, regexp.QuoteMeta(part))
		}
	}
	pattern := regexp.MustCompile("^" + strings.Join(regParts, "/") + "$")
	i.routes = append(i.routes, &Route{
		Path:    path,
		Pattern: pattern,
		Method:  method,
		Handler: handlerNode,
	})
}

func (i *Interpreter) dispatch(w http.ResponseWriter, r *http.Request) {
	for _, route := range i.routes {
		if (route.Method == "*" || route.Method == r.Method) && route.Pattern.MatchString(r.URL.Path) {
			// Extract parameters
			matches := route.Pattern.FindStringSubmatch(r.URL.Path)
			if len(matches) > 0 {
				for index, name := range route.Pattern.SubexpNames() {
					if index != 0 && name != "" {
						i.variables[name] = matches[index]
					}
				}
			}
			i.executeHandler(route.Handler, w, r)
			return
		}
	}

	// 404 Handler
	if errorFile, exists := i.errorPages[404]; exists {
		content, err := os.ReadFile(errorFile)
		if err == nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)

			// Parse the error page content as a node to enable full tag processing
			p := parser.NewParser(strings.NewReader(string(content)), errorFile)
			ast, err := p.Parse()
			if err == nil {
				// Process variables and tags recursively
				fmt.Fprint(w, i.renderHTML(ast, r))
				return
			}

			// Fallback if parsing fails
			fmt.Fprint(w, i.resolveVars(string(content)))
			return
		}
	}
	http.NotFound(w, r)
}

func (i *Interpreter) executeHandler(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	// HTML tag'i var mı kontrol et ve header'ı önceden ayarla
	for _, child := range node.Children {
		if child.Type == parser.NodeHtml || (child.Type == parser.NodeElement && child.Name != "" && child.Name != "error_page") {
			w.Header().Set("Content-Type", "text/html")
			break
		}
	}

	for _, child := range node.Children {
		// If it's a backend special tag, process it directly
		if i.isSpecialTag(child.Type) && child.Type != parser.NodeBackend {
			i.executeNode(child, w, r)
		} else {
			// Otherwise process via executeNode which handles HTML and Backend nodes
			i.executeNode(child, w, r)
		}
	}
}

func (i *Interpreter) executeNode(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	switch node.Type {
	case parser.NodePrint:
		fmt.Fprint(w, i.resolveVars(node.Content))
	case parser.NodeGet:
		i.handleGet(node, w, r)
	case parser.NodeText:
		fmt.Fprint(w, i.resolveVars(node.Content))
	case parser.NodeHtml:
		fmt.Fprint(w, i.resolveVars(i.renderHTML(node, r)))
	case parser.NodeVariable:
		i.handleVariable(node)
	case parser.NodeHttp:
		i.handleHttp(node)
	case parser.NodeInput:
		prompt := i.resolveVars(node.Attributes["prompt"])
		if prompt != "" {
			fmt.Print(prompt)
		}
		var input string
		fmt.Scanln(&input)
		varName := node.Attributes["var"]
		if varName != "" {
			i.variables[varName] = input
		}
	case parser.NodeFunction:
		i.handleFunction(node, w, r)
	case parser.NodeQuery:
		i.handleQuery(node, w, r)
	case parser.NodeCall:
		i.handleCall(node, w, r)
	case parser.NodeOpen:
		i.handleOpen(node)
	case parser.NodeInclude:
		i.handleInclude(node, w, r)
	case parser.NodeIf:
		i.handleIf(node, w, r)
	case parser.NodeElement:
		// Check if it's a custom tag (named function)
		if _, exists := i.namedFunctions[node.Name]; exists {
			i.handleCall(node, w, r)
		} else {
			// If not a named function, we treat it as HTML
			fmt.Fprint(w, i.resolveVars(i.renderHTML(node, r)))
		}
	case parser.NodeBackend:
		i.executeHandler(node, w, r)
	default:
		// If it's a generic tag (like in HTML), render it
		if node.Type == parser.NodeElement || node.Type == parser.NodeBackend {
			fmt.Fprint(w, i.resolveVars(i.renderHTML(node, r)))
		}
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

	p := parser.NewParser(file, path)
	ast, err := p.Parse()
	if err != nil {
		fmt.Printf("Parse failed for import %s: %v\n", path, err)
		return
	}
	i.traverseAndRun(ast, nil, nil)
}

func (i *Interpreter) handleQuery(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	// Security Check: Block if <get> tag is inside query content or attributes
	if i.containsSecureTag(node) {
		i.reportError(node, "Security Violation: <get> tag or sensitive data access is forbidden within database queries.")
		return
	}

	query := i.resolveVars(node.Content)
	varName := node.Attributes["var"]

	if i.db == nil {
		if w != nil {
			fmt.Fprint(w, "Error: Database not connected")
		} else {
			fmt.Printf("Database Error: Not connected while running query: %s\n", query)
		}
		return
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT") {
		rows, err := i.db.Query(query)
		if err != nil {
			fmt.Fprint(w, "Query Error: ", err)
			return
		}
		defer rows.Close()

		cols, _ := rows.Columns()
		var results []string
		for rows.Next() {
			columns := make([]interface{}, len(cols))
			columnPointers := make([]interface{}, len(cols))
			for j := range columns {
				columnPointers[j] = &columns[j]
			}

			if err := rows.Scan(columnPointers...); err != nil {
				continue
			}

			var rowVals []string
			for _, col := range columns {
				rowVals = append(rowVals, fmt.Sprintf("%v", col))
			}
			results = append(results, strings.Join(rowVals, ", "))
		}

		output := strings.Join(results, "\n")
		if varName != "" {
			i.variables[varName] = output
		} else {
			if w != nil {
				fmt.Fprint(w, output)
			} else {
				fmt.Println(output)
			}
		}
	} else {
		_, err := i.db.Exec(query)
		if err != nil {
			if w != nil {
				fmt.Fprint(w, "Exec Error: ", err)
			} else {
				fmt.Printf("SQL Exec Error: %v\n", err)
			}
		}
	}
}

func (i *Interpreter) handleCall(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	name := node.Name
	if node.Type == parser.NodeCall {
		name = node.Attributes["name"]
	}

	if funcNode, exists := i.namedFunctions[name]; exists {
		// Store current variables to restore them later (isolation)
		oldVars := make(map[string]string)
		for k, v := range i.variables {
			oldVars[k] = v
		}

		// Map attributes as parameters
		for k, v := range node.Attributes {
			if k != "name" {
				i.variables[k] = i.resolveVars(v)
			}
		}

		if funcNode.Attributes["type"] != "" && funcNode.Attributes["type"] != "html" {
			i.handleFunction(funcNode, w, r)
		} else {
			i.executeHandler(funcNode, w, r)
		}

		i.variables = oldVars
	} else {
		i.reportError(node, "cannot find tag <%s>", name)
	}
}

func (i *Interpreter) handleDatabase(node *parser.Node) {
	dbType := node.Attributes["type"]
	if dbType == "" {
		dbType = "sqlite"
	}

	if dbType == "json" {
		path := i.resolveVars(node.Attributes["connection"])
		if path == "" {
			path = i.resolveVars(node.Attributes["path"])
		}
		varName := node.Attributes["var"]

		if path == "" || varName == "" {
			return
		}

		if strings.HasPrefix(path, "./") {
			cwd, _ := os.Getwd()
			path = filepath.Join(cwd, path)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("JSON DB Error: %v\n", err)
			return
		}
		i.variables[varName] = string(data)
		return
	}

	conn := i.resolveVars(node.Attributes["connection"])
	if conn == "" {
		return
	}

	// Dynamic Relative Path Support
	if strings.HasPrefix(conn, "./") {
		cwd, _ := os.Getwd()
		conn = filepath.Join(cwd, conn)
	}

	db, err := sql.Open("sqlite", conn)
	if err != nil {
		fmt.Printf("Database connection error: %v\n", err)
		return
	}
	i.db = db
}

func (i *Interpreter) handleFunction(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	name := node.Attributes["name"]
	if name == "" {
		// Warning removed
	} else {
		i.namedFunctions[name] = node
		// If it has a name but no type, it's just a definition
		if node.Attributes["type"] == "" {
			return
		}
	}

	funcType := node.Attributes["type"]
	content := i.renderInnerContent(node, r)
	switch funcType {
	case "js":
		vm := otto.New()

		vm.Set("print", func(call otto.FunctionCall) otto.Value {
			for _, arg := range call.ArgumentList {
				if w != nil {
					fmt.Fprint(w, arg.String())
				} else {
					fmt.Print(arg.String())
				}
			}
			return otto.Value{}
		})

		console, _ := vm.Object("console = {}")
		console.Set("log", func(call otto.FunctionCall) otto.Value {
			return otto.Value{}
		})

		_, err := vm.Run(content)
		if err != nil {
			if w != nil {
				fmt.Fprint(w, "JS Error: ", err)
			} else {
				fmt.Printf("JS Runtime Error: %v\n", err)
			}
		}
	case "go":
		tmpFile := "tmp_func.go"
		goCode := content
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
		fmt.Fprint(w, i.resolveVars(i.renderHTML(node, r)))
	}
}

func (i *Interpreter) resolveVars(input string) string {
	result := input
	result = strings.ReplaceAll(result, "{{port}}", i.port)
	for name, value := range i.variables {
		placeholder := "{{" + name + "}}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

func (i *Interpreter) renderHTML(node *parser.Node, r *http.Request) string {
	// If it's a function node, we might need to render its output
	if node.Type == parser.NodeFunction {
		var sb strings.Builder
		recorder := &bufferedResponseWriter{sb: &sb}
		i.handleCall(node, recorder, r)
		return sb.String()
	}
	if i.isSpecialTag(node.Type) && node.Type != parser.NodeBackend {
		return i.executeToBuffer(node, r)
	}

	var sb strings.Builder
	if node.Type != parser.NodeBackend {
		sb.WriteString("<" + node.Name)
		for k, v := range node.Attributes {
			sb.WriteString(fmt.Sprintf(" %s=\"%s\"", i.resolveVars(k), i.resolveVars(v)))
		}
		sb.WriteString(">")
	}
	for _, child := range node.Children {
		switch child.Type {
		case parser.NodeText:
			sb.WriteString(i.resolveVars(child.Content))
		case parser.NodePrint:
			sb.WriteString(i.resolveVars(child.Content))
		case parser.NodeGet:
			// Handle <get> in HTML context
			sb.WriteString(i.executeToBuffer(child, r))
		case parser.NodeIf:
			sb.WriteString(i.executeToBuffer(child, r))
		case parser.NodeCall:
			sb.WriteString(i.executeToBuffer(child, r))
		default:
			sb.WriteString(i.renderHTML(child, r))
		}
	}
	if node.Type != parser.NodeBackend {
		sb.WriteString("</" + node.Name + ">")
	}
	return sb.String()
}

// Helper for capturing output
type bufferedResponseWriter struct {
	sb *strings.Builder
}

func (b *bufferedResponseWriter) Header() http.Header { return make(http.Header) }
func (b *bufferedResponseWriter) Write(p []byte) (int, error) {
	return b.sb.Write(p)
}
func (b *bufferedResponseWriter) WriteHeader(statusCode int) {}

func (i *Interpreter) handleOpen(node *parser.Node) {
	file := i.resolveVars(node.Attributes["file"])
	if file == "" {
		return
	}

	if strings.HasPrefix(file, "./") {
		cwd, _ := os.Getwd()
		file = filepath.Join(cwd, file)
	}

	f, err := os.Open(file)
	if err != nil {
		fmt.Printf("Open Error: %v\n", err)
		return
	}
	defer f.Close()

	p := parser.NewParser(f, file)
	ast, err := p.Parse()
	if err != nil {
		fmt.Printf("Parse failed for open %s: %v\n", file, err)
		return
	}

	go func() {
		newInterp := NewInterpreter()
		newInterp.Run(ast)
	}()
}

func (i *Interpreter) handleInclude(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	file := i.resolveVars(node.Attributes["file"])
	if file == "" {
		return
	}

	if strings.HasPrefix(file, "./") {
		cwd, _ := os.Getwd()
		file = filepath.Join(cwd, file)
	}

	f, err := os.Open(file)
	if err != nil {
		fmt.Printf("Include Error: %v\n", err)
		return
	}
	defer f.Close()

	p := parser.NewParser(f, file)
	ast, err := p.Parse()
	if err != nil {
		fmt.Printf("Parse failed for include %s: %v\n", file, err)
		return
	}

	// Push to stack for error tracing
	i.includeStack = append(i.includeStack, node)

	// Phase 1: Definitions
	for _, child := range ast.Children {
		switch child.Type {
		case parser.NodeVariable:
			i.handleVariable(child)
		case parser.NodeFunction:
			name := child.Attributes["name"]
			if name != "" {
				i.namedFunctions[name] = child
			}
		case parser.NodeDatabase:
			i.handleDatabase(child)
		}
	}

	// Phase 2: Execution (similar to executeHandler)
	if w != nil {
		i.executeHandler(ast, w, r)
	} else {
		// If at top level, just traverse
		i.traverseAndRun(ast, w, r)
	}

	// Pop from stack
	if len(i.includeStack) > 0 {
		i.includeStack = i.includeStack[:len(i.includeStack)-1]
	}
}

func (i *Interpreter) reportError(node *parser.Node, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if len(i.includeStack) > 0 {
		caller := i.includeStack[len(i.includeStack)-1]
		fmt.Printf("includeError: %s %d.%d at file %s %d.%d %s\n",
			filepath.Base(caller.Filename), caller.Line, caller.Col,
			filepath.Base(node.Filename), node.Line, node.Col,
			msg)
	} else {
		fmt.Printf("Error: %s %d.%d %s\n", filepath.Base(node.Filename), node.Line, node.Col, msg)
	}
}

func (i *Interpreter) handleGet(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	data := node.Attributes["data"]
	switch data {
	case "ip":
		ip := r.RemoteAddr
		if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
			ip = prior
		}
		fmt.Fprint(w, ip)
	case "url":
		fmt.Fprint(w, r.URL.String())
	default:
		// Unknown data type
	}
}

func (i *Interpreter) handleIf(node *parser.Node, w http.ResponseWriter, r *http.Request) {
	condition := i.resolveVars(node.Attributes["condition"])

	val, err := i.jsEngine.Eval(condition)
	if err != nil {
		i.reportError(node, "Condition Error: %v", err)
		return
	}

	isTrue, _ := val.ToBoolean()
	if isTrue {
		i.executeHandler(node, w, r)
		return
	}

	// Check for elseif and else
	for _, child := range node.Children {
		if child.Type == parser.NodeElseIf {
			cond := i.resolveVars(child.Attributes["condition"])
			v, err := i.jsEngine.Eval(cond)
			if err == nil {
				if t, _ := v.ToBoolean(); t {
					i.executeHandler(child, w, r)
					return
				}
			}
		} else if child.Type == parser.NodeElse {
			i.executeHandler(child, w, r)
			return
		}
	}
}

func (i *Interpreter) containsSecureTag(node *parser.Node) bool {
	if node.Type == parser.NodeGet {
		return true
	}

	// Check content for variable patterns that might be from <get>
	// Actually, the user says <get> tag cannot be in database
	// So we check children recursively
	for _, child := range node.Children {
		if i.containsSecureTag(child) {
			return true
		}
	}
	return false
}

func (i *Interpreter) renderInnerContent(node *parser.Node, r *http.Request) string {
	var sb strings.Builder
	for _, child := range node.Children {
		if child.Type == parser.NodeText {
			sb.WriteString(child.Content)
		} else {
			sb.WriteString(i.renderHTML(child, r))
		}
	}
	return sb.String()
}

func (i *Interpreter) isSpecialTag(nodeType parser.NodeType) bool {
	switch nodeType {
	case parser.NodePrint, parser.NodeGet, parser.NodeIf, parser.NodeCall, parser.NodeVariable, parser.NodeHttp, parser.NodeInput, parser.NodeFunction, parser.NodeQuery, parser.NodeOpen, parser.NodeInclude, parser.NodeBackend, parser.NodeElseIf, parser.NodeElse:
		return true
	default:
		return false
	}
}

func (i *Interpreter) executeToBuffer(node *parser.Node, r *http.Request) string {
	var sb strings.Builder
	recorder := &bufferedResponseWriter{sb: &sb}
	// Pass nil for w if it's not a direct HTTP response, but we need r for <get>
	i.executeNode(node, recorder, r)
	return sb.String()
}
