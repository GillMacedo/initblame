// Command quickstart is a small HTTP health checker that does no work at
// init beyond the standard library's own. It's the healthy case.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080/healthz", "URL to check")
	version := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *version {
		fmt.Println("quickstart 1.0.0")
		return
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(*url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	resp.Body.Close()
	fmt.Println(resp.Status)
}
