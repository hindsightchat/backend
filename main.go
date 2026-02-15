package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	gomiddlewares "github.com/go-chi/chi/v5/middleware"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	valkeydb "github.com/hindsightchat/backend/src/lib/dbs/valkey"
	"github.com/hindsightchat/backend/src/middleware"
	authroutes "github.com/hindsightchat/backend/src/routes/auth"
	conversationroutes "github.com/hindsightchat/backend/src/routes/conversations"
	friendroutes "github.com/hindsightchat/backend/src/routes/friends"
	usersroutes "github.com/hindsightchat/backend/src/routes/users"
	websocketroutes "github.com/hindsightchat/backend/src/routes/websocket"
	"github.com/joho/godotenv"
)

func main() {

	if os.Getenv("IS_PROD") == "true" {
		fmt.Println("loading .env.prod")
		godotenv.Load(".env.prod")
	} else {
		fmt.Println("loading .env")
		godotenv.Load()
	}

	// initialize database
	database.InitDatabase()

	// wait til valkey is ready
	valkeydb.WaitUntilReady()

	// start gochi server

	r := chi.NewRouter()

	r.Use(middleware.CaseSensitiveMiddleware)
	r.Use(middleware.SaveAuthTokenMiddleware)
	r.Use(gomiddlewares.Logger)

	authroutes.RegisterRoutes(r)
	friendroutes.RegisterRoutes(r)
	usersroutes.RegisterRoutes(r)
	websocketroutes.RegisterRoutes(r)
	conversationroutes.RegisterRoutes(r)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	fmt.Println("backend running on http://localhost:" + "3000")

	fmt.Println("\nRoutes:\n")

	chi.Walk(r, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		fmt.Printf("[%s]: '%s' has %d middlewares\n", method, route, len(middlewares))
		return nil
	})

	// serve without showing it to the world (only locally)
	http.ListenAndServe(":3000", r)

	// http.ListenAndServe(":3000", r)

}
