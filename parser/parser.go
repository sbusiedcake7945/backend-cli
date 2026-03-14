// parser/parser.go
package parser

import (
	"fmt"
	"io"
	"strings"
)

type Parser struct {
	lexer        *Lexer
	tokens       []Token
	pos          int
	currentToken Token
	filename     string
}

func NewParser(r io.Reader, filename string) *Parser {
	lexer := NewLexer(r)

	// Tüm token'ları önceden oku
	var tokens []Token
	for {
		token := lexer.NextToken()
		tokens = append(tokens, token)
		if token.Type == TokenEOF || token.Type == TokenError {
			break
		}
	}

	parser := &Parser{
		lexer:    lexer,
		tokens:   tokens,
		pos:      0,
		filename: filename,
	}

	if len(tokens) > 0 {
		parser.currentToken = tokens[0]
	}

	return parser
}

func (p *Parser) advance() {
	p.pos++
	if p.pos < len(p.tokens) {
		p.currentToken = p.tokens[p.pos]
	} else {
		p.currentToken = Token{Type: TokenEOF}
	}
}

func (p *Parser) expect(tokenType TokenType) error {
	if p.currentToken.Type != tokenType {
		return fmt.Errorf("expected %v, got %v at line %d, col %d",
			tokenType, p.currentToken.Type, p.currentToken.Line, p.currentToken.Col)
	}
	return nil
}

func (p *Parser) Parse() (*Node, error) {
	// Root backend node'u oluştur
	root := &Node{
		Type:       NodeBackend,
		Name:       "backend",
		Attributes: make(map[string]string),
		Children:   []*Node{},
		Filename:   p.filename,
	}

	for p.currentToken.Type != TokenEOF {
		switch p.currentToken.Type {
		case TokenOpenTag:
			element, err := p.parseElement()
			if err != nil {
				return nil, err
			}
			root.Children = append(root.Children, element)
		case TokenText:
			// Text content'i ignore et (şimdilik)
			p.advance()
		default:
			p.advance()
		}
	}

	return root, nil
}

func (p *Parser) parseElement() (*Node, error) {
	// < açılış tag'ini kontrol et
	if err := p.expect(TokenOpenTag); err != nil {
		return nil, err
	}
	p.advance()

	// Element ismini al
	if err := p.expect(TokenIdentifier); err != nil {
		return nil, err
	}
	elementName := p.currentToken.Value
	p.advance()

	// Node oluştur
	node := &Node{
		Name:       elementName,
		Attributes: make(map[string]string),
		Children:   []*Node{},
		Line:       p.currentToken.Line,
		Col:        p.currentToken.Col,
		Filename:   p.filename,
	}

	// Attribute'ları parse et
	for p.currentToken.Type != TokenCloseTag &&
		p.currentToken.Type != TokenSlashClose &&
		p.currentToken.Type != TokenEOF {

		if p.currentToken.Type == TokenIdentifier {
			attrName := p.currentToken.Value
			p.advance()

			if err := p.expect(TokenEquals); err != nil {
				return nil, err
			}
			p.advance()

			if err := p.expect(TokenString); err != nil {
				return nil, err
			}
			attrValue := p.currentToken.Value
			p.advance()

			node.Attributes[attrName] = attrValue
		} else {
			return nil, p.error("expected attribute name")
		}
	}

	// Self-closing tag kontrolü
	if p.currentToken.Type == TokenSlashClose {
		p.advance()
		p.setNodeType(node)
		return node, nil
	}

	// > kapanış tag'ini kontrol et
	if err := p.expect(TokenCloseTag); err != nil {
		return nil, err
	}
	p.advance()

	// Content ve child element'leri parse et
	var contentBuilder strings.Builder

	for p.currentToken.Type != TokenEOF {
		if p.currentToken.Type == TokenOpenTag {
			// Closing tag kontrolü </name>
			if p.pos+1 < len(p.tokens) &&
				p.tokens[p.pos+1].Type == TokenIdentifier &&
				strings.HasPrefix(p.tokens[p.pos+1].Value, "/") {

				closingName := strings.TrimPrefix(p.tokens[p.pos+1].Value, "/")
				if closingName == elementName {
					p.advance() // < token'ını atla
					p.advance() // /element token'ını atla

					// > kapanışını kontrol et
					if p.currentToken.Type == TokenCloseTag {
						p.advance()
						break
					} else {
						return nil, p.error("expected closing tag '>'")
					}
				}
			}

			// Child element
			child, err := p.parseElement()
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)

		} else if p.currentToken.Type == TokenText {
			textNode := &Node{
				Type:     NodeText,
				Content:  p.currentToken.Value,
				Line:     p.currentToken.Line,
				Col:      p.currentToken.Col,
				Filename: p.filename,
			}
			node.Children = append(node.Children, textNode)
			contentBuilder.WriteString(p.currentToken.Value)
			p.advance()
		} else {
			p.advance()
		}
	}

	node.Content = strings.TrimSpace(contentBuilder.String())
	p.setNodeType(node)

	return node, nil
}

func (p *Parser) setNodeType(node *Node) {
	switch node.Name {
	case "backend":
		node.Type = NodeBackend
	case "config":
		node.Type = NodeConfig
	case "port":
		node.Type = NodePort
	case "route":
		node.Type = NodeRoute
	case "handler":
		node.Type = NodeHandler
	case "middleware":
		node.Type = NodeMiddleware
	case "import":
		node.Type = NodeImport
	case "function":
		node.Type = NodeFunction
	case "database":
		node.Type = NodeDatabase
	case "cron":
		node.Type = NodeCron
	case "if":
		node.Type = NodeIf
	case "elseif":
		node.Type = NodeElseIf
	case "else":
		node.Type = NodeElse
	case "get":
		node.Type = NodeGet
	case "print":
		node.Type = NodePrint
	case "input":
		node.Type = NodeInput
	case "html":
		node.Type = NodeHtml
	case "var", "variable":
		node.Type = NodeVariable
	case "http":
		node.Type = NodeHttp
	case "start_message":
		node.Type = NodeStartMessage
	case "query":
		node.Type = NodeQuery
	case "call":
		node.Type = NodeCall
	case "json_db":
		node.Type = NodeJsonDatabase
	case "open":
		node.Type = NodeOpen
	case "include":
		node.Type = NodeInclude
	default:
		node.Type = NodeElement
	}
}

func (p *Parser) error(msg string) error {
	return fmt.Errorf("line %d, col %d: %s",
		p.currentToken.Line, p.currentToken.Col, msg)
}

// AST Debug için yardımcı fonksiyon
func (p *Parser) PrintAST(node *Node, indent int) {
	prefix := strings.Repeat("  ", indent)
	fmt.Printf("%s%s", prefix, node.Name)

	if len(node.Attributes) > 0 {
		fmt.Printf(" [attrs: %v]", node.Attributes)
	}
	if node.Content != "" {
		fmt.Printf(" content: '%s'", node.Content)
	}
	fmt.Println()

	for _, child := range node.Children {
		p.PrintAST(child, indent+1)
	}
}
