package main

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	database "github.com/rmcord/backend/src/lib/dbs/tidb"
	valkeydb "github.com/rmcord/backend/src/lib/dbs/valkey"
	"github.com/rmcord/backend/src/middleware"
	authroutes "github.com/rmcord/backend/src/routes/auth"
)

func main() {
	godotenv.Load()

	// initialize database
	database.InitDatabase()

	// wait til valkey is ready
	valkeydb.WaitUntilReady()

	// start gochi server

	r := chi.NewRouter()

	r.Use(middleware.CaseSensitiveMiddleware)
	r.Use(middleware.SaveAuthTokenMiddleware)

	authroutes.RegisterRoutes(r)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	fmt.Println("backend running on http://localhost:" + "3000")

	fmt.Println("\nRoutes:\n")

	chi.Walk(r, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		fmt.Printf("[%s]: '%s' has %d middlewares\n", method, route, len(middlewares))
		return nil
	})

	http.ListenAndServe(":3000", r)

}
