// Montly MCP Server
//
// Exposes the Montly recurring-task tracker as MCP tools so any
// MCP-compatible AI client (Claude Desktop, Cursor, ChatGPT, …)
// can query and interact with a self-hosted Montly instance.
//
// Required env vars:
//
//	MONTLY_URL   – base URL of the Montly instance  (default: http://localhost:8080)
//	MONTLY_TOKEN – a Montly API token (mt_…)         (required)
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newServer(client *montlyClient) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "montly",
			Version: "0.1.0",
		},
		nil,
	)
	registerTools(server, client)
	return server
}

func main() {
	client := newMontlyClient()

	// MCP_PORT selects the transport:
	//   set   → Streamable HTTP on that port (for Docker / remote)
	//   unset → stdio (for local / Claude Desktop subprocess)
	port := os.Getenv("MCP_PORT")

	if port == "" {
		server := newServer(client)
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
		return
	}

	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return newServer(client) },
		nil,
	)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("montly-mcp listening on :%s (http)", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("mcp http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("montly-mcp shutting down…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
