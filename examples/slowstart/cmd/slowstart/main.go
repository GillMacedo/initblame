// Command slowstart is a made-up CLI whose dependencies do heavy work at
// init, so even --version is slow.
package main

import (
	"fmt"
	"os"

	"example.com/slowstart/internal/report"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("slowstart 1.0.0")
		return
	}
	fmt.Println(report.Line("order-7-ab-12.csv", 42))
}
