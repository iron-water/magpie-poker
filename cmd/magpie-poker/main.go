package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"magpie-poker/internal/local"
)

func main() {
	var (
		port      = flag.Int("port", 3481, "local UI port")
		tablePort = flag.Int("table-port", 3482, "host table port")
		addr      = flag.String("addr", "127.0.0.1", "local bind address")
		webRoot   = flag.String("web-root", "", "plugin root containing web/ and assets/")
		dataDir   = flag.String("data-dir", "", "data directory")
		version   = flag.Bool("version", false, "print version")
	)
	flag.Parse()
	if *version {
		fmt.Println(local.Version)
		return
	}
	root := *webRoot
	if root == "" {
		if exe, err := os.Executable(); err == nil {
			root = filepath.Dir(exe)
			if _, err := os.Stat(filepath.Join(root, "web", "index.html")); err != nil {
				root = "."
			}
		} else {
			root = "."
		}
	}
	app, err := local.New(local.Options{
		Port: *port, TablePort: *tablePort, Addr: *addr, WebRoot: root, DataDir: *dataDir,
	})
	if err != nil {
		log.Fatal(err)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		_ = app.Close()
	}()
	fmt.Printf("[poker] go service on http://%s:%d\n", *addr, app.Port())
	if err := app.ListenAndServe(); err != nil {
		log.Print(err)
	}
}
