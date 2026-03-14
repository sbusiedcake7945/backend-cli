// parser/ast.go (güncellenmiş)
package parser

type NodeType int

const (
	NodeBackend NodeType = iota
	NodeConfig
	NodeRoute
	NodeHandler
	NodeMiddleware
	NodeImport
	NodeFunction
	NodeDatabase
	NodeCron
	NodeIf
	NodeElseIf
	NodeElse
	NodeGet
	NodePrint
	NodeInput
	NodeHtml
	NodeStartMessage
	NodePort
	NodeElement
	NodeVariable
	NodeHttp
	NodeText
	NodeQuery
	NodeCall
	NodeOpen
	NodeInclude
	NodeJsonDatabase
)

type Node struct {
	Type       NodeType
	Name       string
	Attributes map[string]string
	Children   []*Node
	Content    string
	Line       int
	Col        int
	Filename   string
}

// AST Visitor pattern için interface
type Visitor interface {
	VisitBackend(node *Node) error
	VisitConfig(node *Node) error
	VisitRoute(node *Node) error
	VisitHandler(node *Node) error
	VisitMiddleware(node *Node) error
	VisitImport(node *Node) error
	VisitFunction(node *Node) error
	VisitDatabase(node *Node) error
	VisitCron(node *Node) error
	VisitIf(node *Node) error
	VisitElseIf(node *Node) error
	VisitElse(node *Node) error
	VisitGet(node *Node) error
	VisitPrint(node *Node) error
	VisitInput(node *Node) error
	VisitHtml(node *Node) error
	VisitStartMessage(node *Node) error
	VisitPort(node *Node) error
	VisitElement(node *Node) error
	VisitVariable(node *Node) error
	VisitHttp(node *Node) error
}

// AST traversal
func (n *Node) Accept(visitor Visitor) error {
	switch n.Type {
	case NodeBackend:
		return visitor.VisitBackend(n)
	case NodeConfig:
		return visitor.VisitConfig(n)
	case NodeRoute:
		return visitor.VisitRoute(n)
	case NodeHandler:
		return visitor.VisitHandler(n)
	case NodeMiddleware:
		return visitor.VisitMiddleware(n)
	case NodeImport:
		return visitor.VisitImport(n)
	case NodeFunction:
		return visitor.VisitFunction(n)
	case NodeDatabase:
		return visitor.VisitDatabase(n)
	case NodeCron:
		return visitor.VisitCron(n)
	case NodeIf:
		return visitor.VisitIf(n)
	case NodeElseIf:
		return visitor.VisitElseIf(n)
	case NodeElse:
		return visitor.VisitElse(n)
	case NodeGet:
		return visitor.VisitGet(n)
	case NodePrint:
		return visitor.VisitPrint(n)
	case NodeInput:
		return visitor.VisitInput(n)
	case NodeHtml:
		return visitor.VisitHtml(n)
	case NodeStartMessage:
		return visitor.VisitStartMessage(n)
	case NodePort:
		return visitor.VisitPort(n)
	case NodeElement:
		return visitor.VisitElement(n)
	case NodeVariable:
		return visitor.VisitVariable(n)
	case NodeHttp:
		return visitor.VisitHttp(n)
	default:
		return nil
	}
}

// Yardımcı fonksiyonlar
func (n *Node) GetChildByName(name string) *Node {
	for _, child := range n.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func (n *Node) GetChildrenByName(name string) []*Node {
	var children []*Node
	for _, child := range n.Children {
		if child.Name == name {
			children = append(children, child)
		}
	}
	return children
}

func (n *Node) GetAttribute(key string) string {
	if val, exists := n.Attributes[key]; exists {
		return val
	}
	return ""
}
