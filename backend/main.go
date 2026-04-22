package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"io/fs"
	"log"
	"net/http"
	"os"
	pathpkg "path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"
)

//go:embed dist
var static embed.FS

func securityHeaders(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:")
			if secure {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func main() {
	// ── Database ──────────────────────────────────────────────────────────────
	dbType := os.Getenv("DB_TYPE")
	if dbType == "" {
		dbType = "sqlite"
	}

	var dbDSN string
	switch dbType {
	case "postgres":
		dbDSN = os.Getenv("DATABASE_URL")
		if dbDSN == "" {
			log.Fatal("DATABASE_URL is required when DB_TYPE=postgres")
		}
	case "sqlite":
		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = "./data"
		}
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			log.Fatalf("create data dir: %v", err)
		}
		dbDSN = dataDir + "/montly.db"
	default:
		log.Fatalf("unsupported DB_TYPE %q (must be sqlite or postgres)", dbType)
	}

	db, err := initDB(dbDSN, dbType)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	// ── First-run admin bootstrap ─────────────────────────────────────────────
	var settingsMigrationAdminID int64

	n, err := db.CountUsers()
	if err != nil {
		log.Fatalf("count users: %v", err)
	}
	if n == 0 {
		adminUser := os.Getenv("ADMIN_USERNAME")
		adminPass := os.Getenv("ADMIN_PASSWORD")
		if adminUser == "" || adminPass == "" {
			log.Fatal("No users exist. Set ADMIN_USERNAME and ADMIN_PASSWORD to create the initial admin account.")
		}
		if len(adminPass) < 8 {
			log.Fatal("ADMIN_PASSWORD must be at least 8 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("hash admin password: %v", err)
		}
		admin, err := db.CreateUser(adminUser, string(hash), true)
		if err != nil {
			log.Fatalf("create admin user: %v", err)
		}
		if err := db.AssignOrphanedTasks(admin.ID); err != nil {
			log.Fatalf("assign orphaned tasks: %v", err)
		}
		settingsMigrationAdminID = admin.ID
		log.Printf("created admin user %q (id=%d)", adminUser, admin.ID)
	} else {
		// Find the first admin for the settings schema migration (one-time).
		if fa, err := db.GetFirstAdmin(); err == nil {
			settingsMigrationAdminID = fa.ID
		}
	}

	// ── Settings schema migration (global → per-user) ─────────────────────────
	if settingsMigrationAdminID != 0 {
		if err := db.MigrateSettingsToUserScoped(settingsMigrationAdminID); err != nil {
			log.Fatalf("migrate settings: %v", err)
		}
	}

	// ── Files & receipts ──────────────────────────────────────────────────────
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	receiptsDir := dataDir + "/receipts"
	if err := os.MkdirAll(receiptsDir, 0o700); err != nil {
		log.Fatalf("create receipts dir: %v", err)
	}

	// ── Session secret ────────────────────────────────────────────────────────
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("rand.Read: %v", err)
		}
		sessionSecret = base64.StdEncoding.EncodeToString(b)
		log.Println("WARNING: SESSION_SECRET not set — sessions will not survive restarts. Set SESSION_SECRET for persistent sessions.")
	}
	secret := []byte(sessionSecret)

	secureCookies := os.Getenv("SECURE_COOKIES") == "true"
	trustProxy := os.Getenv("TRUST_PROXY_HEADERS") == "true"

	// ── Background context (cancels goroutines on shutdown) ───────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Router ────────────────────────────────────────────────────────────────
	h := &Handler{db: db, receiptsDir: receiptsDir}
	rl := newRateLimiter(ctx)
	ah := &AuthHandler{db: db, secret: secret, secure: secureCookies, trustProxy: trustProxy, rl: rl}
	uh := &UserHandler{db: db}
	th := &TokenHandler{db: db}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(securityHeaders(secureCookies))
	r.Use(recoverer)

	mountRoutes := func(r chi.Router) {
		// Add API version header to all API responses.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-API-Version", "1")
				next.ServeHTTP(w, r)
			})
		})

		// Public: login / logout
		r.Post("/auth/login", ah.Login)
		r.Post("/auth/logout", ah.Logout)

		// Protected
		r.Group(func(r chi.Router) {
			r.Use(requireAuth(secret, db))
			r.Get("/auth/me", ah.Me)
			r.Patch("/auth/password", ah.ChangePassword)
			r.Get("/auth/tokens", th.ListTokens)
			r.Post("/auth/tokens", th.CreateToken)
			r.Delete("/auth/tokens/{id}", th.RevokeToken)
			mountAPI(r, h)

			// Admin-only user management
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/users", uh.ListUsers)
				r.Post("/users", uh.CreateUser)
				r.Delete("/users/{id}", uh.DeleteUser)
			})
		})
	}

	r.Route("/api",    func(r chi.Router) { mountRoutes(r) })
	r.Route("/api/v1", func(r chi.Router) { mountRoutes(r) })

	dist, err := fs.Sub(static, "dist")
	if err != nil {
		log.Fatal(err)
	}
	r.Handle("/*", spaHandler(dist))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("montly listening on :%s (db=%s, secure=%v)", port, dbType, secureCookies)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func mountAPI(r chi.Router, h *Handler) {
	r.Get("/settings", h.GetSettings)
	r.Put("/settings", h.UpdateSettings)
	r.Get("/tasks", h.ListTasks)
	r.Post("/tasks", h.CreateTask)
	r.Get("/tasks/{id}", h.GetTask)
	r.Put("/tasks/{id}", h.UpdateTask)
	r.Delete("/tasks/{id}", h.DeleteTask)
	r.Get("/receipts/{filename}", h.ServeReceipt)
	r.Get("/completions", h.ListCompletions)
	r.Post("/completions/toggle", h.ToggleCompletion)
	r.Patch("/completions/{task_id}/{month}", h.PatchCompletion)
	r.Post("/completions/{task_id}/{month}/receipt", h.UploadCompletionReceipt)
	r.Delete("/completions/{task_id}/{month}/receipt", h.DeleteCompletionReceipt)
}

func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := pathpkg.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "." {
			clean = "index.html"
		}
		if _, err := fsys.Open(clean); err != nil {
			http.ServeFileFS(w, r, fsys, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
