package main

import (
	"os"

	"github.com/taylormonacelli/justbe"
)

func main() {
	code := justbe.Execute()
	os.Exit(code)
}
