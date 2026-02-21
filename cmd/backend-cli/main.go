// cmd/backend-cli/main.go
package main

import (
	"backend-dsl/interpreter"
	"backend-dsl/parser"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: backend-cli run --input <file.backend>")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		inputPath := "app.backend"
		if len(os.Args) > 2 {
			inputPath = os.Args[2]
		}

		file, err := os.Open(inputPath)
		if err != nil {
			fmt.Printf("Error opening file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()

		p := parser.NewParser(file)
		ast, err := p.Parse()
		if err != nil {
			fmt.Printf("Parse failed: %v\n", err)
			os.Exit(1)
		}

		interp := interpreter.NewInterpreter()
		if err := interp.Run(ast); err != nil {
			fmt.Printf("Execution failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Println("Usage: backend-cli run <file.backend>")
		os.Exit(1)
	}
}
