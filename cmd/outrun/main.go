// Command outrun runs the game server: headless core, local API and the
// embedded web viewer.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"

	"myjev/internal/game"
	"myjev/internal/server"
	"myjev/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "loopback address to listen on")
	seed := flag.Uint64("seed", 0, "seed of the first run (0 = random)")
	flag.Parse()

	// The API has no authentication, so it must not be reachable from
	// other machines.
	if host, _, err := net.SplitHostPort(*addr); err != nil {
		log.Fatalf("outrun: bad -addr %q: %v", *addr, err)
	} else if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Fatalf("outrun: -addr must be a loopback address such as 127.0.0.1:8765, got %q", *addr)
	}

	if *seed == 0 {
		*seed = uint64(rand.Uint32())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := server.New(game.DefaultConfig(), *seed, web.FS)
	go srv.Run(ctx)

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler()}
	go func() {
		<-ctx.Done()
		httpSrv.Close()
	}()

	log.Printf("outrun: open http://%s  (K = left, L = right)", *addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
